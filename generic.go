package sseserver

import (
	"context"
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
func NewGeneric(cfg Config, resync ResyncFn, lastID string) *GenericStream {
	return NewGenericMultiStream(cfg, resync, map[string]string{"": lastID})
}

// NewGenericMultiStream is similar to NewGeneric but allows setting initial last
// event ID values for multiple topics.
func NewGenericMultiStream(cfg Config, resync ResyncFn, lastIDs map[string]string) *GenericStream {
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
func (s *GenericStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

// PublishTopic sends an event to the specified topic.
func (s *GenericStream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, nil)
}

// PublishBroadcast sends an event to all connected clients across all topics.
func (s *GenericStream) PublishBroadcast(event *Event) {
	event.ID = ""
	s.broker.broadcast(event)
}

// Subscribe adds a subscriber to the default topic ("") and starts sending
// events to the provided response writer. Unlike cached implementations,
// the GenericStream relies on the user-provided resync function to retrieve
// missed events when a client reconnects.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *GenericStream) Subscribe(ctx context.Context, w http.ResponseWriter, lastEventID string) error {
	return s.SubscribeTopicFiltered(ctx, w, "", lastEventID, nil)
}

// SubscribeFiltered adds a subscriber to the default topic ("") with event filtering
// and starts sending events to the provided response writer. The filter function allows
// selective event delivery or event transformation before sending to the client.
// Events are processed through the filter before delivery, and nil results are omitted.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *GenericStream) SubscribeFiltered(ctx context.Context, w http.ResponseWriter, lastEventID string, f FilterFn) error {
	return s.SubscribeTopicFiltered(ctx, w, "", lastEventID, f)
}

// SubscribeTopic adds a subscriber to the specified topic and starts sending
// events to the provided response writer. This is similar to Subscribe but allows
// specifying which topic to receive events from. Each topic maintains its own
// event history and last event ID tracking. The user-provided resync function
// receives the topic name and is responsible for retrieving historical events
// specific to that topic, enabling topic-specific resynchronization logic.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *GenericStream) SubscribeTopic(ctx context.Context, w http.ResponseWriter, topic string, lastEventID string) error {
	return s.SubscribeTopicFiltered(ctx, w, topic, lastEventID, nil)
}

// SubscribeTopicFiltered adds a subscriber to the specified topic with event filtering
// and starts sending events to the provided response writer. This is the most flexible
// subscription method, combining topic-specific event streams with event filtering.
// When a client reconnects with a lastEventID, the user-provided resync function is called
// to retrieve missed events (up to ResyncEventsThreshold). If the resync function returns
// an error, the connection will be terminated or, if some events were already retrieved,
// those will be sent before closing.
// The connection remains open until closed by the client, server shutdown, or context cancellation.
func (s *GenericStream) SubscribeTopicFiltered(ctx context.Context, w http.ResponseWriter, topic string, lastEventID string, f FilterFn) error {
	source := make(chan *Event, s.cfg.QueueLength)
	toID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	events := make([]Event, 0)
	// lastEventID will be nil if client connects for the first time
	// serverID will be nil if server did not send any events yet
	for len(events) <= s.cfg.ResyncEventsThreshold {
		list, err := s.resync(ctx, topic, lastEventID, toID)
		if err != nil {
			if len(events) > 0 {
				return Respond(ctx, w, prependStream(events, nil), &s.cfg, s.responseStop)
			}

			return err
		}

		if len(list) == 0 {
			return Respond(ctx, w, prependStream(events, applyChanFilter(source, f)), &s.cfg, s.responseStop)
		}

		switch f {
		case nil:
			events = append(events, list...)
		default:
			events = append(events, applySliceFilter(list, f)...)
		}

		lastEventID = list[len(list)-1].ID
	}

	return Respond(ctx, w, prependStream(events, nil), &s.cfg, s.responseStop)
}

// DropSubscribers removes all currently active stream subscribers and close all active HTTP responses.
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
