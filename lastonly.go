package sseserver

import (
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
// The event is cached to support client resynchronization.
func (s *LastOnlyStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

// PublishTopic sends an event to the specified topic.
// The event is cached to support client resynchronization.
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

// PublishBroadcast for LastOnlyStream does not cache a broadcasted event
// and thus does not permit sending an event with ID value.
func (s *LastOnlyStream) PublishBroadcast(event *Event) {
	// LastOnly SSE stream does not support tracking broadcasted events. This
	// removes ID value from all broadcasted events.
	event.ID = ""
	s.broker.broadcast(event)
}

// Subscribe adds a subscriber to the default topic ("") and starts sending
// events to the provided response writer. If lastEventID is provided and
// differs from the server's last event ID, it attempts to resynchronize
// missing events from the cache.
// Returns ErrCacheMiss if resynchronization is needed but events are not found in cache.
func (s *LastOnlyStream) Subscribe(w http.ResponseWriter, lastEventID string) error {
	return s.SubscribeTopicFiltered(w, "", lastEventID, nil)
}

// SubscribeFiltered adds a subscriber to the default topic ("") with event filtering
// and starts sending events to the provided response writer. The filter function
// can be used to modify or exclude events before sending them to the client.
// Returns ErrCacheMiss if resynchronization is needed but events are not found in cache.
func (s *LastOnlyStream) SubscribeFiltered(w http.ResponseWriter, lastEventID string, f FilterFn) error {
	return s.SubscribeTopicFiltered(w, "", lastEventID, f)
}

// SubscribeTopic adds a subscriber to the specified topic and starts sending
// events to the provided response writer. If lastEventID is provided and
// differs from the server's last event ID, it attempts to resynchronize
// missing events from the cache.
// Returns ErrCacheMiss if resynchronization is needed but events are not found in cache.
func (s *LastOnlyStream) SubscribeTopic(w http.ResponseWriter, topic string, lastEventID string) error {
	return s.SubscribeTopicFiltered(w, topic, lastEventID, nil)
}

// SubscribeTopicFiltered adds a subscriber to the specified topic with event filtering
// and starts sending events to the provided response writer. If lastEventID is provided and
// differs from the server's last event ID, it attempts to resynchronize missing events from the cache.
// The filter function can be used to modify or exclude events before sending them to the client.
// Returns ErrCacheMiss if resynchronization is needed but events are not found in cache.
func (s *LastOnlyStream) SubscribeTopicFiltered(w http.ResponseWriter, topic string, lastEventID string, f FilterFn) error {
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
		return Respond(w, applyChanFilter(prependStream(events, source), f), &s.cfg, s.responseStop)
	}

	return Respond(w, applyChanFilter(source, f), &s.cfg, s.responseStop)
}

// DropSubscribers closes all active connections to subscribers.
// This forces clients to reconnect, which can be useful when server state changes.
func (s *LastOnlyStream) DropSubscribers() {
	close(s.responseStop)
}

// Stop gracefully shuts down the SSE stream by closing the underlying broker
// and waiting for all related goroutines to finish.
func (s *LastOnlyStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}
