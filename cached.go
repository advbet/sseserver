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
	maxEvents int,
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

		if len(res) == maxEvents || event.ID == maxID {
			return res, false
		}

		currentID = event.ID
	}
}

// CachedStream implements a server-sent events (SSE) stream with caching capabilities.
// It provides functionality for publishing events to topics, subscribing clients to topics,
// and automatically resynchronizing clients who reconnect after disconnection.
// Events are cached for a configurable duration to support client resynchronization.
// The stream supports multiple topics, filtering of events, and graceful shutdown.
type CachedStream struct {
	broker       brokerChan
	cfg          Config
	responseStop chan struct{}
	wg           sync.WaitGroup
	cache        *cache
}

// ErrCacheMiss is returned from cachedStream.Subscribe if resyncing client is
// not possible because events are not found in a cache. This situation will
// usually occur if client was disconnected for too long and the oldest events
// were evicted from the cache.
//
// This error is returned before writing anything to the response writer.
// It is responsibility of the caller of cachedStream.Subscribe to generate a
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
func NewCached(cfg Config, lastEventID string, expiration, cleanup time.Duration) *CachedStream {
	return NewCachedMultiStream(cfg, map[string]string{"": lastEventID}, expiration, cleanup)
}

// NewCachedMultiStream is similar to NewCached but allows setting initial last
// event ID values for multiple topics.
func NewCachedMultiStream(cfg Config, lastEventsIDs map[string]string, expiration, cleanup time.Duration) *CachedStream {
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
		s.broker.run(lastEventsIDs)
		cancel()
	}()

	s.wg.Add(1)

	go func() {
		defer s.wg.Done()
		s.cache.cleanUp(ctx)
	}()

	return s
}

// Publish sends an event to the default topic ("").
func (s *CachedStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

// PublishTopic sends an event to the specified topic.
func (s *CachedStream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, func(lastID string) {
		s.cache.add(topic, lastID, event)
	})
}

// PublishBroadcast sends an event to all connected clients across all topics.
func (s *CachedStream) PublishBroadcast(event *Event) {
	// Cached SSE stream does not support tracking broadcasted events. This
	// removes ID value from all broadcasted events.
	event.ID = ""
	s.broker.broadcast(event)
}

// Subscribe adds a subscriber to the default topic ("") and starts sending
// events to the provided response writer. This function handles automatic
// client reconnection by checking the lastEventID. If the client is connecting
// for the first time or has seen the most recent event, it will receive new events
// as they are published. If the client missed some events, it will attempt to
// resynchronize from the cache.
// If the requested events are no longer available in the cache, it returns ErrCacheMiss,
// allowing the caller to handle the situation appropriately.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *CachedStream) Subscribe(ctx context.Context, w http.ResponseWriter, lastEventID string) error {
	return s.SubscribeTopicFiltered(ctx, w, "", lastEventID, nil)
}

// SubscribeFiltered adds a subscriber to the default topic ("") with event filtering
// and starts sending events to the provided response writer. The filter function allows
// selective event delivery or event transformation before sending to the client.
// Events are processed through the filter before delivery, and nil results are omitted.
// Like Subscribe, this method supports automatic resynchronization for reconnecting clients.
// If the requested events are no longer available in the cache, it returns ErrCacheMiss,
// allowing the caller to handle the situation appropriately.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *CachedStream) SubscribeFiltered(ctx context.Context, w http.ResponseWriter, lastEventID string, f FilterFn) error {
	return s.SubscribeTopicFiltered(ctx, w, "", lastEventID, f)
}

// SubscribeTopic adds a subscriber to the specified topic and starts sending
// events to the provided response writer. This is similar to Subscribe but allows
// specifying which topic to receive events from. Each topic maintains its own
// event history and last event ID tracking, enabling multiple independent event
// streams within the same CachedStream instance.
// If the requested events are no longer available in the cache, it returns ErrCacheMiss,
// allowing the caller to handle the situation appropriately.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *CachedStream) SubscribeTopic(ctx context.Context, w http.ResponseWriter, topic string, lastEventID string) error {
	return s.SubscribeTopicFiltered(ctx, w, topic, lastEventID, nil)
}

// SubscribeTopicFiltered adds a subscriber to the specified topic with event filtering
// and starts sending events to the provided response writer. This is the most flexible
// subscription method, combining topic-specific event streams with event filtering.
// If the client needs to resynchronize, this function will attempt to retrieve missed
// events from the cache.
// If the requested events are no longer available in the cache, it returns ErrCacheMiss,
// allowing the caller to handle the situation appropriately.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *CachedStream) SubscribeTopicFiltered(ctx context.Context, w http.ResponseWriter, topic string, lastEventID string, f FilterFn) error {
	source := make(chan *Event, s.cfg.QueueLength)
	lastServerID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	if lastEventID == "" || lastEventID == lastServerID {
		// no resync needed
		return Respond(ctx, w, applyChanFilter(source, f), &s.cfg, s.responseStop)
	}

	events, miss := s.cache.get(
		topic,
		lastEventID,
		lastServerID,
		s.cfg.ResyncEventsThreshold,
		f,
	)

	if miss {
		// this can be deceiving as sometimes the client might be
		// ahead of the server and in fact be too early.
		err := Respond(ctx, w, prependStream([]Event{ResyncRequiredErrorEvent()}, nil), &s.cfg, s.responseStop)
		if err != nil {
			return err
		}

		return ErrCacheMiss
	}

	if len(events) == 0 || lastServerID == events[len(events)-1].ID {
		return Respond(ctx, w, prependStream(events, applyChanFilter(source, f)), &s.cfg, s.responseStop)
	}

	return Respond(ctx, w, prependStream(events, nil), &s.cfg, s.responseStop)
}

// DropSubscribers removes all currently active stream subscribers and close all active HTTP responses.
func (s *CachedStream) DropSubscribers() {
	close(s.responseStop)
}

// Stop gracefully shuts down the SSE stream by closing the underlying broker
// and waiting for all related goroutines to finish.
func (s *CachedStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}

func topicIDKey(topic string, id string) string {
	return fmt.Sprintf("%d:%s%d:%s", len(topic), topic, len(id), id)
}
