package sseserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// cache is a custom cache implementation for events caching.
type cache struct {
	mu        sync.RWMutex
	items     map[string]map[string]*Event
	expirator struct {
		ttl  time.Duration
		intv time.Duration
		list []ttlRef
	}
}

// ttlRef contains information about a specific event and its expiration
// information.
type ttlRef struct {
	topic      string
	id         string
	expiration time.Time
}

// newCache creates a new instance of a cache.
func newCache(ttl, intv time.Duration) *cache {
	c := &cache{
		items: make(map[string]map[string]*Event),
	}

	c.expirator.ttl = ttl
	c.expirator.intv = intv

	return c
}

// cleanUp starts a blocking process of a cache clean up. Periodically it
// checks expirator's item's ttl references and deletes expired items from the
// topic's. A context can be used to stop the process.
func (c *cache) cleanUp(ctx context.Context) {
	tm := time.NewTimer(c.expirator.intv)
	defer tm.Stop()

	var tstamp time.Time
	for {
		select {
		case tstamp = <-tm.C:
		case <-ctx.Done():
			return
		}

		c.mu.Lock()
		var total int
		for _, item := range c.expirator.list {
			if item.expiration.After(tstamp) {
				break
			}

			delete(c.items[item.topic], item.id)
			if len(c.items[item.topic]) == 0 {
				delete(c.items, item.topic)
			}

			total++
		}

		c.expirator.list = c.expirator.list[total:]
		c.mu.Unlock()

		tm.Reset(c.expirator.intv)
	}
}

// add adds the event to the topic, mapping it to the specified id.
func (c *cache) add(topic, id string, event *Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	events, ok := c.items[topic]
	if !ok {
		events = make(map[string]*Event)
	}

	if _, ok := events[id]; ok {
		slog.Warn("overwritten existing cache item", slog.String("topic", topic), slog.String("id", id))
	}

	events[id] = event
	c.items[topic] = events
	c.expirator.list = append(c.expirator.list, ttlRef{
		topic:      topic,
		id:         id,
		expiration: time.Now().Add(c.expirator.ttl),
	})
}

// get retrieves events from a specified topic starting with the startID. The
// max attribute determines how many events can be retrieved at once, if less
// events are available, all of them are returned. If the event with the maxID
// is reached, the method returns collected events. If no events were found,
// an empty slice is returned.
func (c *cache) get(
	topic, currentID, maxID string,
	max int,
	filter FilterFn,
) ([]Event, bool) {

	c.mu.RLock()
	defer c.mu.RUnlock()

	events, ok := c.items[topic]
	if !ok {
		return nil, true
	}

	var res []Event
	for {
		event, ok := events[currentID]
		if !ok {
			return res, true
		}

		switch filter {
		case nil:
			res = append(res, *event)
		default:
			filtered := filter(event)
			if filtered != nil {
				res = append(res, *filtered)
			}
		}

		if len(res) == max || event.ID == maxID {
			return res, false
		}

		currentID = event.ID
	}
}

type CachedStream struct {
	broker       brokerChan
	cfg          Config
	responseStop chan struct{}
	wg           sync.WaitGroup
	cache        *cache
}

// ErrCacheMiss is returned from cachedStream.Subscribe if resyncinc client is
// not possible because events are not found in a cache. This situation will
// usualy occur if client was disconnected for too long and the oldes events
// were evicted from the cache.
//
// This error is returned before writing anything to the response writer. It
// isresponsibiity of the caller of cachedStream.Subscribe to generate a
// response if this error is returned.
var ErrCacheMiss = errors.New("missing events in cache")

// NewCached creates a new SSE stream. All published events are cached for up to
// expiration time in local cache and clients are automatically resynced on
// reconnect.
//
// Passing empty string as last event ID for Subscribe() would connect client
// without resync.
//
// Call to Subscribe() might return ErrCacheMiss if client requests to resync
// from an event not found in the cache. If ErrCacheMiss is returned user of
// this library is responsible for generating HTTP response to the client. It is
// recommended to return 204 no content response to stop client from
// reconnecting until he syncs event state manually.
func NewCached(lastID string, cfg Config, expiration, cleanup time.Duration) *CachedStream {
	return NewCachedMultiStream(map[string]string{"": lastID}, cfg, expiration, cleanup)
}

// NewCachedMultiStream is similar to NewCached but allows setting initial last
// event ID values for multiple topics.
func NewCachedMultiStream(lastIDs map[string]string, cfg Config, expiration, cleanup time.Duration) *CachedStream {
	s := &CachedStream{
		broker:       newBroker(),
		cfg:          cfg,
		responseStop: make(chan struct{}),
		cache:        newCache(expiration, cleanup),
	}

	ctx, cancel := context.WithCancel(context.Background())

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.broker.run(lastIDs)
		cancel()
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.cache.cleanUp(ctx)
	}()

	return s
}

func (s *CachedStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

func (s *CachedStream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, func(lastID string) {
		s.cache.add(topic, lastID, event)
	})
}

func (s *CachedStream) PublishBroadcast(event *Event) {
	// Cached SSE stream does not support tracking broadcasted events. This
	// removes ID value from all broadcasted events.
	event.ID = ""
	s.broker.broadcast(event)
}

func (s *CachedStream) Subscribe(w http.ResponseWriter, lastClientID string) error {
	return s.SubscribeTopicFiltered(w, "", lastClientID, nil)
}

func (s *CachedStream) SubscribeFiltered(w http.ResponseWriter, lastClientID string, f FilterFn) error {
	return s.SubscribeTopicFiltered(w, "", lastClientID, f)
}

func (s *CachedStream) SubscribeTopic(w http.ResponseWriter, topic string, lastClientID string) error {
	return s.SubscribeTopicFiltered(w, topic, lastClientID, nil)
}

func (s *CachedStream) SubscribeTopicFiltered(w http.ResponseWriter, topic string, lastClientID string, filter FilterFn) error {
	source := make(chan *Event, s.cfg.QueueLength)
	lastServerID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	if lastClientID == "" || lastClientID == lastServerID {
		// no resync needed
		return Respond(w, applyChanFilter(source, filter), &s.cfg, s.responseStop)
	}

	events, miss := s.cache.get(
		topic,
		lastClientID,
		lastServerID,
		s.cfg.ResyncEventsThreshold,
		filter,
	)
	if miss {
		// this can be deceiving as sometimes the client might be
		// ahead of the server and in fact be too early.
		return ErrCacheMiss
	}

	if len(events) == 0 || lastServerID == events[len(events)-1].ID {
		return Respond(w, prependStream(events, applyChanFilter(source, filter)), &s.cfg, s.responseStop)
	}

	return Respond(w, prependStream(events, nil), &s.cfg, s.responseStop)
}

func (s *CachedStream) DropSubscribers() {
	close(s.responseStop)
}

func (s *CachedStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}

func topicIDKey(topic string, id string) string {
	return fmt.Sprintf("%d:%s%d:%s", len(topic), topic, len(id), id)
}
