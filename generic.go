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
		s.broker.run(lastID)
	}()
	return s
}

// Publish broadcast given event to all currently connected clients
// (subscribers).
//
// Publish on a stopped stream will cause panic.
func (s *stream) Publish(event *Event) {
	s.broker.publish(event)
}

// Subscribe handled HTTP request to receive SSE stream. Caller of this function
// should parse Last-Event-ID header and create appropriate lastEventID object.
//
// Subscribe on a stopped stream will cause panic.
func (s *stream) Subscribe(w http.ResponseWriter, lastEventID interface{}) error {
	source := make(chan *Event, s.cfg.QueueLength)
	toID := s.broker.subscribe(source)
	defer s.broker.unsubscribe(source)

	// lastEventID will be nil if client connects for the first time
	// serverID will be nil if server did not send any events yet
	events, ok := s.resync(lastEventID, toID)
	if !ok {
		return Respond(w, prependStream(events, nil), &s.cfg, s.responseStop)
	}
	return Respond(w, prependStream(events, source), &s.cfg, s.responseStop)
}

// DropSubscribers removes all currently active stream subscribers and close all
// active HTTP responses. After call to this method all new subscribers would be
// closed immediately. Calling DropSubscribers more than one time would panic.
//
// This function is useful in implementing graceful application shutdown, this
// method should be called only when web server are not accepting any new
// connections and all that is left is terminating already connected ones.
func (s *stream) DropSubscribers() {
	close(s.responseStop)
}

// Stop closes event stream. It will disconnect all connected subscribers and
// deallocate all resources used for the stream. After stream is stopped it can
// not started again and should not be used anymore.
//
// Calls to Publish or Subscribe after stream was stopped will cause panic.
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
