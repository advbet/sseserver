package sseserver

import (
	"net/http"
	"sync"
)

type stream struct {
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
func NewGeneric(resync ResyncFn, lastID interface{}, cfg Config) Stream {
	s := &stream{
		broker:       newBroker(),
		resync:       resync,
		cfg:          cfg,
		responseStop: make(chan struct{}),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.broker.run(map[string]interface{}{"": lastID})
	}()
	return s
}

func (s *stream) Publish(event *Event) {
	s.PublishTopic("", event)
}

func (s *stream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, nil)
}

func (s *stream) Subscribe(w http.ResponseWriter, lastEventID interface{}) error {
	return s.SubscribeTopic(w, "", lastEventID)
}

func (s *stream) SubscribeTopic(w http.ResponseWriter, topic string, lastEventID interface{}) error {
	source := make(chan *Event, s.cfg.QueueLength)
	toID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	// lastEventID will be nil if client connects for the first time
	// serverID will be nil if server did not send any events yet
	events, ok := s.resync(lastEventID, toID)
	if !ok {
		return Respond(w, prependStream(events, nil), &s.cfg, s.responseStop)
	}
	return Respond(w, prependStream(events, source), &s.cfg, s.responseStop)
}

func (s *stream) DropSubscribers() {
	close(s.responseStop)
}

func (s *stream) Stop() {
	close(s.broker)
	s.wg.Wait()
}

// prependStream takes slice and channel of events and and produces new channel
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
