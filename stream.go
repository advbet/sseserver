package sseserver

import "net/http"

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
