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
func NewCachedCount(cfg Config, lastEventID string, size int) *CachedCountStream {
	return NewCachedCountMultiStream(cfg, map[string]string{"": lastEventID}, size)
}

// NewCachedCountMultiStream is similar to NewCachedCount but allows setting initial last
// event ID values for multiple topics.
func NewCachedCountMultiStream(cfg Config, lastEventsIDs map[string]string, size int) *CachedCountStream {
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
		s.broker.run(lastEventsIDs)
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
// events to the provided response writer. When a client reconnects with a lastEventID,
// the stream attempts to resynchronize by retrieving missed events from the fixed-size
// circular cache. Unlike the time-based CachedStream, this implementation limits the
// cache by count, making it suitable for applications with steady event rates.
// If the requested events are no longer available in the cache, it returns ErrCacheMiss,
// allowing the caller to handle the situation appropriately.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *CachedCountStream) Subscribe(ctx context.Context, w http.ResponseWriter, lastEventID string) error {
	return s.SubscribeTopicFiltered(ctx, w, "", lastEventID, nil)
}

// SubscribeFiltered adds a subscriber to the default topic ("") with event filtering
// and starts sending events to the provided response writer. The filter function allows
// selective event delivery or event transformation before sending to the client.
// Events are processed through the filter before delivery, and nil results are omitted.
// Like Subscribe, this method supports reconnection with automatic resynchronization
// from the count-based cache.
// If the requested events are no longer available in the cache, it returns ErrCacheMiss,
// allowing the caller to handle the situation appropriately.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *CachedCountStream) SubscribeFiltered(ctx context.Context, w http.ResponseWriter, lastEventID string, f FilterFn) error {
	return s.SubscribeTopicFiltered(ctx, w, "", lastEventID, f)
}

// SubscribeTopic adds a subscriber to the specified topic and starts sending
// events to the provided response writer. This is similar to Subscribe but allows
// specifying which topic to receive events from. Each topic maintains its own
// event history and last event ID tracking, enabling multiple independent event
// streams within the same CachedCountStream instance. The count-based cache is
// shared across all topics, with positions tracked by topic and event ID.
// If the requested events are no longer available in the cache, it returns ErrCacheMiss,
// allowing the caller to handle the situation appropriately.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *CachedCountStream) SubscribeTopic(ctx context.Context, w http.ResponseWriter, topic string, lastEventID string) error {
	return s.SubscribeTopicFiltered(ctx, w, topic, lastEventID, nil)
}

// SubscribeTopicFiltered adds a subscriber to the specified topic with event filtering
// and starts sending events to the provided response writer. This is the most flexible
// subscription method, combining topic-specific event streams with event filtering.
// If the client needs to resynchronize, this function will attempt to retrieve missed
// events from the count-based circular cache.
// If the requested events are no longer available in the cache, it returns ErrCacheMiss,
// allowing the caller to handle the situation appropriately.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *CachedCountStream) SubscribeTopicFiltered(ctx context.Context, w http.ResponseWriter, topic string, lastEventID string, f FilterFn) error {
	source := make(chan *Event, s.cfg.QueueLength)
	lastServerID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	if lastEventID == "" || lastEventID == lastServerID {
		// no resync needed
		return Respond(ctx, w, applyChanFilter(source, f), &s.cfg, s.responseStop)
	}

	var events []Event

	s.mu.RLock()
	for {
		event, ok := s.events[topicIDKey(topic, lastEventID)]
		if !ok {
			s.mu.RUnlock()

			err := Respond(ctx, w, prependStream([]Event{ResyncRequiredErrorEvent()}, nil), &s.cfg, s.responseStop)
			if err != nil {
				return err
			}

			return ErrCacheMiss
		}

		events = append(events, *event)
		lastEventID = event.ID

		if lastServerID == lastEventID {
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
