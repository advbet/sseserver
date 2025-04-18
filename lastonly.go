package sseserver

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
)

// LastOnlyStream sends only the last SSE event per topic to new clients.
type LastOnlyStream struct {
	broker       brokerChan
	cfg          Config
	responseStop chan struct{}

	wg sync.WaitGroup

	sync.RWMutex
	lastEventID string
	last        map[string]map[string]*Event
}

var errFiltersNotSupported = errors.New("filters are not supported")

// NewLastOnly creates a new sse stream that resends only last seen event to all
// newly connected clients. If client already have seen the latest event is not repeated.
//
// Event filtering is not supported.
func NewLastOnly(cfg Config) *LastOnlyStream {
	s := &LastOnlyStream{
		broker:       newBroker(),
		cfg:          cfg,
		responseStop: make(chan struct{}),
		last:         make(map[string]map[string]*Event),
	}

	s.wg.Add(1)

	go func() {
		defer s.wg.Done()
		s.broker.run(nil)
	}()

	return s
}

// Publish sends an event to the default topic ("").
func (s *LastOnlyStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

// PublishTopic sends an event to the specified topic.
func (s *LastOnlyStream) PublishTopic(topic string, event *Event) {
	//nolint:revive
	s.broker.publish(topic, event, func(lastID string) {
		s.Lock()
		defer s.Unlock()

		if _, ok := s.last[topic]; !ok {
			s.last[topic] = make(map[string]*Event)
		}

		s.last[topic][event.Event] = event
		s.lastEventID = event.ID
	})
}

// PublishBroadcast sends an event to all connected clients across all topics.
func (s *LastOnlyStream) PublishBroadcast(event *Event) {
	// LastOnly SSE stream does not support tracking broadcasted events. This
	// removes ID value from all broadcasted events.
	event.ID = ""
	s.broker.broadcast(event)
}

// Subscribe adds a subscriber to the default topic ("") and starts sending
// events to the provided response writer. This function sends the last event in the default topic,
// then streams new events as they are published. Unlike other implementations,
// LastOnlyStream does not maintain a historical event log - it only remembers the
// most recent event of each event type per topic.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *LastOnlyStream) Subscribe(ctx context.Context, w http.ResponseWriter, lastEventID string) error {
	return s.SubscribeTopicFiltered(ctx, w, "", lastEventID, nil)
}

// SubscribeFiltered adds a subscriber to the default topic ("") with event filtering
// and starts sending events to the provided response writer. This function is provided
// for interface compatibility, but LastOnlyStream does not support filtering and will
// return an error if a filter function is provided. This limitation exists because
// filters would complicate the "last event only" semantics of this implementation.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *LastOnlyStream) SubscribeFiltered(ctx context.Context, w http.ResponseWriter, lastEventID string, f FilterFn) error {
	return s.SubscribeTopicFiltered(ctx, w, "", lastEventID, f)
}

// SubscribeTopic adds a subscriber to the specified topic and starts sending
// events to the provided response writer. Each topic maintains its own set of "last events".
// The client will immediately receive the most recent event for each event in the topic,
// then receive new events as they are published.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *LastOnlyStream) SubscribeTopic(ctx context.Context, w http.ResponseWriter, topic string, lastEventID string) error {
	return s.SubscribeTopicFiltered(ctx, w, topic, lastEventID, nil)
}

// SubscribeTopicFiltered adds a subscriber to the specified topic with event filtering
// and starts sending events to the provided response writer. Same as SubscribeTopic.
// Note that filtering is not supported and will result in an error if attempted.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *LastOnlyStream) SubscribeTopicFiltered(ctx context.Context, w http.ResponseWriter, topic string, lastEventID string, f FilterFn) error {
	if f != nil {
		return errFiltersNotSupported
	}

	source := make(chan *Event, s.cfg.QueueLength)
	s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	s.RLock()
	last := s.last[topic]
	events := make([]Event, 0)

	if len(last) > 0 && (lastEventID != s.lastEventID || lastEventID == "") {
		s := make([]string, 0)
		for key := range last {
			s = append(s, key)
		}

		sort.Strings(s)

		for _, k := range s {
			events = append(events, *last[k])
		}
	}
	s.RUnlock()

	if len(events) > 0 {
		return Respond(ctx, w, applyChanFilter(prependStream(events, source), f), &s.cfg, s.responseStop)
	}

	return Respond(ctx, w, applyChanFilter(source, f), &s.cfg, s.responseStop)
}

// DropSubscribers removes all currently active stream subscribers and close all active HTTP responses.
func (s *LastOnlyStream) DropSubscribers() {
	close(s.responseStop)
}

// Stop gracefully shuts down the SSE stream by closing the underlying broker
// and waiting for all related goroutines to finish.
func (s *LastOnlyStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}
