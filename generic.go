package sseserver

import (
	"net/http"
	"sync"
)

// GenericStream is the most generic SSE stream implementation where resync
// logic is supplied by the user of this package.
type GenericStream struct {
	broker       brokerChan
	resync       ResyncFn
	cfg          Config
	responseStop chan struct{}

	wg sync.WaitGroup
}

// NewGeneric creates a new instance of SSE stream. Creating new stream requires
// to provide a resync function with ResyncFn signature. It is used to
// generate a list of events that client might have missed during a reconnect.
// Argument lastID is used set last event ID that was published before
// application was started, this value is passed to the resync function and
// later replaced by the events published with stream.Publish method.
func NewGeneric(resync ResyncFn, lastID string, cfg Config) *GenericStream {
	return NewGenericMultiStream(resync, map[string]string{"": lastID}, cfg)
}

// NewGenericMultiStream is similar to NewGeneric but allows setting initial last
// event ID values for multiple topics.
func NewGenericMultiStream(resync ResyncFn, lastIDs map[string]string, cfg Config) *GenericStream {
	s := &GenericStream{
		broker:       newBroker(),
		resync:       resync,
		cfg:          cfg,
		responseStop: make(chan struct{}),
	}

	s.wg.Add(1)

	go func() {
		defer s.wg.Done()
		s.broker.run(lastIDs)
	}()

	return s
}

// Publish sends an event to the default topic ("").
// The event is cached to support client resynchronization.
func (s *GenericStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

// PublishTopic sends an event to the specified topic.
// The event is cached to support client resynchronization.
func (s *GenericStream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, nil)
}

// PublishBroadcast sends an event to all connected clients across all topics.
// Broadcasted events are not cached and their IDs are removed to prevent
// affecting the event sequence of any specific topic.
func (s *GenericStream) PublishBroadcast(event *Event) {
	event.ID = ""
	s.broker.broadcast(event)
}

// Subscribe adds a subscriber to the default topic ("") and starts sending
// events to the provided response writer. If lastEventID is provided and
// differs from the server's last event ID, it attempts to resynchronize
// missing events from the cache.
// Returns ErrCacheMiss if resynchronization is needed but events are not found in cache.
func (s *GenericStream) Subscribe(w http.ResponseWriter, r *http.Request, lastEventID string) error {
	return s.SubscribeTopicFiltered(w, r, "", lastEventID, nil)
}

// SubscribeFiltered adds a subscriber to the default topic ("") with event filtering
// and starts sending events to the provided response writer. The filter function
// can be used to modify or exclude events before sending them to the client.
// Returns ErrCacheMiss if resynchronization is needed but events are not found in cache.
func (s *GenericStream) SubscribeFiltered(w http.ResponseWriter, r *http.Request, lastEventID string, f FilterFn) error {
	return s.SubscribeTopicFiltered(w, r, "", lastEventID, f)
}

// SubscribeTopic adds a subscriber to the specified topic and starts sending
// events to the provided response writer. If lastEventID is provided and
// differs from the server's last event ID, it attempts to resynchronize
// missing events from the cache.
// Returns ErrCacheMiss if resynchronization is needed but events are not found in cache.
func (s *GenericStream) SubscribeTopic(w http.ResponseWriter, r *http.Request, topic string, lastEventID string) error {
	return s.SubscribeTopicFiltered(w, r, topic, lastEventID, nil)
}

// SubscribeTopicFiltered adds a subscriber to the specified topic with event filtering
// and starts sending events to the provided response writer. If lastEventID is provided and
// differs from the server's last event ID, it attempts to resynchronize missing events from the cache.
// The filter function can be used to modify or exclude events before sending them to the client.
// Returns ErrCacheMiss if resynchronization is needed but events are not found in cache.
func (s *GenericStream) SubscribeTopicFiltered(w http.ResponseWriter, r *http.Request, topic string, lastEventID string, f FilterFn) error {
	source := make(chan *Event, s.cfg.QueueLength)
	toID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	events := make([]Event, 0)
	// lastEventID will be nil if client connects for the first time
	// serverID will be nil if server did not send any events yet
	for len(events) <= s.cfg.ResyncEventsThreshold {
		list, err := s.resync(topic, lastEventID, toID)
		if err != nil {
			if len(events) > 0 {
				return Respond(w, prependStream(events, nil), &s.cfg, s.responseStop)
			}

			return err
		}

		if len(list) == 0 {
			return RespondWithContext(r.Context(), w, prependStream(events, applyChanFilter(source, f)), &s.cfg, s.responseStop)
		}

		switch f {
		case nil:
			events = append(events, list...)
		default:
			events = append(events, applySliceFilter(list, f)...)
		}

		lastEventID = list[len(list)-1].ID
	}

	return RespondWithContext(r.Context(), w, prependStream(events, nil), &s.cfg, s.responseStop)
}

// DropSubscribers closes all active connections to subscribers.
// This forces clients to reconnect, which can be useful when server state changes.
func (s *GenericStream) DropSubscribers() {
	close(s.responseStop)
}

// Stop gracefully shuts down the SSE stream by closing the underlying broker
// and waiting for all related goroutines to finish.
func (s *GenericStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}

// prependStream takes slice and channel of events and produces new channel
// that will contain all events in the slice followed by the events in source
// channel. If source channel is nil it will be ignored an only events in the
// slice will be used.
func prependStream(events []Event, source <-chan *Event) <-chan *Event {
	sink := make(chan *Event)
	go func() {
		defer close(sink)
		// Stream static events
		for i := range events {
			sink <- &events[i]
		}

		// Exit if source stream is missing, this allows to reuse this
		// function for generating stream from slice only
		if source == nil {
			return
		}

		// Restream source channel
		for event := range source {
			sink <- event
		}
	}()

	return sink
}
