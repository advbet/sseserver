package sseserver

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	cache "github.com/patrickmn/go-cache"
)

type CachedStream struct {
	broker       brokerChan
	cfg          Config
	responseStop chan struct{}
	wg           sync.WaitGroup
	cache        *cache.Cache
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
		cache:        cache.New(expiration, cleanup),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.broker.run(lastIDs)
	}()

	return s
}

func (s *CachedStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

func (s *CachedStream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, func(lastID string) {
		s.cache.Set(topicIDKey(topic, lastID), event, cache.DefaultExpiration)
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

func (s *CachedStream) SubscribeTopicFiltered(w http.ResponseWriter, topic string, lastClientID string, f FilterFn) error {
	source := make(chan *Event, s.cfg.QueueLength)
	lastServerID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	if lastClientID == "" || lastClientID == lastServerID {
		// no resync needed
		return Respond(w, applyChanFilter(source, f), &s.cfg, s.responseStop)
	}

	var events []Event
	for len(events) <= s.cfg.ResyncEventsThreshold {
		eventIface, ok := s.cache.Get(topicIDKey(topic, lastClientID))
		if !ok {
			return ErrCacheMiss
		}

		event := eventIface.(*Event)
		switch f {
		case nil:
			events = append(events, *event)
		default:
			if e := f(event); e != nil {
				events = append(events, *e)
			}
		}

		lastClientID = event.ID
		if lastServerID == lastClientID {
			return Respond(w, prependStream(events, applyChanFilter(source, f)), &s.cfg, s.responseStop)
		}
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
