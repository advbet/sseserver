package sseserver

import (
	"context"
	"net/http"
	"sync"
)

// CachedCountStream caches SSE events up to a limit.
// Returns ErrCacheMiss if a requested event is not found.
type CachedCountStream struct {
	broker       brokerChan
	cfg          Config
	responseStop chan struct{}
	wg           sync.WaitGroup
	maxKeysCount int
	lastKeys     []string
	ctr          int
	events       map[string]*Event
	mu           sync.RWMutex
}

// NewCachedCount creates a new SSE stream. All published events are cached.
// The number of cached events can be set by passing the desired size argument.
// Clients are automatically resynced on reconnect.
//
// Passing empty string as last event ID for Subscribe() would connect client
// without resync.
//
// Call to Subscribe() might return ErrCacheMiss if client requests to resync
// from an event not found in the cache. If ErrCacheMiss is returned user of
// this library is responsible for generating HTTP response to the client. It is
// recommended to return 204 no content response to stop client from
// reconnecting until he syncs event state manually.
func NewCachedCount(lastID string, cfg Config, size int) *CachedCountStream {
	return NewCachedCountMultiStream(map[string]string{"": lastID}, cfg, size)
}

// NewCachedCountMultiStream is similar to NewCachedCount but allows setting initial last
// event ID values for multiple topics.
func NewCachedCountMultiStream(lastIDs map[string]string, cfg Config, size int) *CachedCountStream {
	s := &CachedCountStream{
		broker:       newBroker(),
		cfg:          cfg,
		responseStop: make(chan struct{}),
		maxKeysCount: size,
		lastKeys:     make([]string, size),
		ctr:          0,
		events:       make(map[string]*Event),
	}

	s.wg.Add(1)

	go func() {
		defer s.wg.Done()
		s.broker.run(lastIDs)
	}()

	return s
}

// Publish sends an event to the default topic ("").
func (s *CachedCountStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

// PublishTopic sends an event to the specified topic.
func (s *CachedCountStream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, func(lastID string) {
		s.mu.Lock()
		s.ctr = (s.ctr + 1) % s.maxKeysCount
		delete(s.events, s.lastKeys[s.ctr])

		key := topicIDKey(topic, lastID)

		s.events[key] = event
		s.lastKeys[s.ctr] = key
		s.mu.Unlock()
	})
}

// PublishBroadcast sends an event to all connected clients across all topics.
func (s *CachedCountStream) PublishBroadcast(event *Event) {
	// Cached SSE stream does not support tracking broadcasted events. This
	// removes ID value from all broadcasted events.
	event.ID = ""
	s.broker.broadcast(event)
}

// Subscribe adds a subscriber to the default topic ("") and starts sending
// events to the provided response writer.
func (s *CachedCountStream) Subscribe(ctx context.Context, w http.ResponseWriter, lastClientID string) error {
	return s.SubscribeTopicFiltered(ctx, w, "", lastClientID, nil)
}

// SubscribeFiltered adds a subscriber to the default topic ("") with event filtering
// and starts sending events to the provided response writer.
func (s *CachedCountStream) SubscribeFiltered(ctx context.Context, w http.ResponseWriter, lastClientID string, f FilterFn) error {
	return s.SubscribeTopicFiltered(ctx, w, "", lastClientID, f)
}

// SubscribeTopic adds a subscriber to the specified topic and starts sending
// events to the provided response writer.
func (s *CachedCountStream) SubscribeTopic(ctx context.Context, w http.ResponseWriter, topic string, lastClientID string) error {
	return s.SubscribeTopicFiltered(ctx, w, topic, lastClientID, nil)
}

// SubscribeTopicFiltered adds a subscriber to the specified topic with event filtering
// and starts sending events to the provided response writer.
func (s *CachedCountStream) SubscribeTopicFiltered(ctx context.Context, w http.ResponseWriter, topic string, lastClientID string, f FilterFn) error {
	source := make(chan *Event, s.cfg.QueueLength)
	lastServerID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	if lastClientID == "" || lastClientID == lastServerID {
		// no resync needed
		return Respond(ctx, w, applyChanFilter(source, f), &s.cfg, s.responseStop)
	}

	var events []Event

	s.mu.RLock()
	for {
		event, ok := s.events[topicIDKey(topic, lastClientID)]
		if !ok {
			s.mu.RUnlock()

			return ErrCacheMiss
		}

		events = append(events, *event)
		lastClientID = event.ID

		if lastServerID == lastClientID {
			break
		}
	}
	s.mu.RUnlock()

	return Respond(ctx, w, applyChanFilter(prependStream(events, source), f), &s.cfg, s.responseStop)
}

// DropSubscribers removes all currently active stream subscribers and close all active HTTP responses.
func (s *CachedCountStream) DropSubscribers() {
	close(s.responseStop)
}

// Stop gracefully shuts down the SSE stream by closing the underlying broker
// and waiting for all related goroutines to finish.
func (s *CachedCountStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}
