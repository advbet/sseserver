package sseserver

import (
	"net/http"
	"sync"
)

// Stream is an abstraction of SSE stream. Single instance of stream should be
// created for each SSE stream available in the application. Application can
// broadcast streams using stream.Publish method. HTTP handlers for SSE client
// endpoints should use stream.Subscribe to tap into the event stream.
type Stream interface {
	// Publish broadcast given event to all currently connected clients
	// (subscribers).
	//
	// Publish on a stopped stream will cause panic.
	Publish(event *Event)

	// DropSubscribers removes all currently active stream subscribers and
	// close all active HTTP responses. After call to this method all new
	// subscribers would be closed immediately. Calling DropSubscribers more
	// than one time would panic.
	//
	// This function is useful in implementing graceful application
	// shutdown, this method should be called only when web server are not
	// accepting any new connections and all that is left is terminating
	// already connected ones.
	DropSubscribers()

	// Stop closes event stream. It will disconnect all connected
	// subscribers and deallocate all resources used for the stream. After
	// stream is stopped it can not started again and should not be used
	// anymore.
	//
	// Calls to Publish or Subscribe after stream was stopped will cause
	// panic.
	Stop()

	// Subscribe handled HTTP request to receive SSE stream. Caller of this
	// function should parse Last-Event-ID header and create appropriate
	// lastEventID object.
	//
	// Subscribe on a stopped stream will cause panic.
	Subscribe(w http.ResponseWriter, lastEventID interface{}) error
}

type stream struct {
	cmd          chan command
	resync       ResyncFn
	cfg          Config
	responseStop chan struct{}

	wg sync.WaitGroup
}

type operation int

const (
	subscribe operation = iota
	unsubscribe
	publish
)

type command struct {
	op       operation
	sink     chan<- *Event      // used for subscribe, unsubscribe
	response chan<- interface{} // used for subscribe
	event    *Event             // used for publish
}

// ResyncFn is a definition of function used to lookup events missed by
// client reconnects. Users of this package must provide an implementation of
// this function when creating new streams.
//
// This function takes two event ID values as an argument and must return all
// events having IDs in interval (fromID, toID]. Note that event with ID equal
// to fromID SHOULD NOT be included, but event with toID SHOULD be included.
// Argument fromID can be nil if nil was passed to stream.Subscribe as last
// event ID, it usually means client have connected to the SSE stream for the
// first time. Argument toID can also be nil if nil was passed as lastID to
// New() function and client have connected to the SSE stream before any events
// were published using stream.Publish.
//
// ResyncFn should return all events in a given range in a slice. Second
// return variable ok should be set to true if result contains all the requested
// events, false otherwise. With the help of second argument this function can
// limit number of returned events to save resources. Returning false from this
// function will make client request missing events again after given event
// batch is consumed.
//
// Correct implementation of this function is essential for proper client
// resync and vital to whole SSE functionality.
type ResyncFn func(fromID, toID interface{}) (events []Event, ok bool)

// NewGeneric creates a new instance of SSE stream. Creating new stream requires
// to provide a resync function with ResyncFn signature. It is used to
// generate a list of events that client might have missed during a reconnect.
// Argument lastID is used set last event ID that was published before
// application was started, this value is passed to the resync function and
// later replaced by the events published with stream.Publish method.
func NewGeneric(resync ResyncFn, lastID interface{}, cfg Config) Stream {
	s := &stream{
		cmd:          make(chan command),
		resync:       resync,
		cfg:          cfg,
		responseStop: make(chan struct{}),
	}
	s.wg.Add(1)
	go s.run(lastID)
	return s
}

// Publish broadcast given event to all currently connected clients
// (subscribers).
//
// Publish on a stopped stream will cause panic.
func (s *stream) Publish(event *Event) {
	s.cmd <- command{
		op:    publish,
		event: event,
	}
}

// Subscribe handled HTTP request to receive SSE stream. Caller of this function
// should parse Last-Event-ID header and create appropriate lastEventID object.
//
// Subscribe on a stopped stream will cause panic.
func (s *stream) Subscribe(w http.ResponseWriter, lastEventID interface{}) error {
	source := make(chan *Event, s.cfg.QueueLength)
	toID := s.subscribe(source)
	defer s.unsubscribe(source)

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
	close(s.cmd)
	s.wg.Wait()
}

// subscribe is a helper function for adding a subscription, safe for concurrent
// access.
func (s *stream) subscribe(ch chan<- *Event) interface{} {
	response := make(chan interface{}, 1)
	s.cmd <- command{
		op:       subscribe,
		sink:     ch,
		response: response,
	}
	// stream.run goroutine will write current last seen event ID to this
	// channel exactly once and close it
	return <-response
}

// unsubscribe is a helper function for removing a subscription, safe for
// concurrent access.
func (s *stream) unsubscribe(ch chan<- *Event) {
	s.cmd <- command{
		op:   unsubscribe,
		sink: ch,
	}
}

// run handles events broadcasting and manages subscription lists. Each active
// stream must have a single goroutine executing this code.
func (s *stream) run(lastID interface{}) {
	defer s.wg.Done()
	sinks := make(map[chan<- *Event]struct{})

	for cmd := range s.cmd {
		switch cmd.op {
		case subscribe:
			sinks[cmd.sink] = struct{}{}

			// return last seen event ID to the subscriber
			cmd.response <- lastID
			close(cmd.response)
		case unsubscribe:
			if _, ok := sinks[cmd.sink]; ok {
				close(cmd.sink)
				delete(sinks, cmd.sink)
			}
		case publish:
			lastID = cmd.event.ID
			for ch := range sinks {
				select {
				case ch <- cmd.event:
					// Success
				default:
					// Client is too slow, close stream and
					// wait for client reconnect
					close(ch)
					delete(sinks, ch)
				}
			}
		}
	}

	for ch := range sinks {
		close(ch)
	}
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
