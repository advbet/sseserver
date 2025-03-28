package sseserver

import "net/http"

// Stream is an abstraction of SSE stream. Single instance of stream should be
// created for each SSE stream available in the application. Application can
// broadcast streams using stream.Publish method. HTTP handlers for SSE client
// endpoints should use stream.Subscribe to tap into the event stream.
type Stream interface {
	// Publish broadcast given event to all currently connected clients
	// (subscribers) on a default topic.
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

	// Subscribe handles HTTP request to receive SSE stream for a default
	// topic. Caller is responsible for extracting Last event ID value from
	// the request.
	//
	// Subscribe on a stopped stream will cause panic.
	Subscribe(w http.ResponseWriter, lastEventID string) error

	// SubscribeFiltered is similar to Subscribe but each event before being
	// sent to client will be passed to given filtering function. Events
	// returned by the filtering function will be used instead.
	SubscribeFiltered(w http.ResponseWriter, lastEventID string, f FilterFn) error
}

type StreamWithContext interface {
	// Publish broadcast given event to all currently connected clients
	// (subscribers) on a default topic.
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

	// Subscribe handles HTTP request to receive SSE stream for a default
	// topic. Caller is responsible for extracting Last event ID value from
	// the request.
	//
	// Subscribe on a stopped stream will cause panic.
	Subscribe(w http.ResponseWriter, r *http.Request, lastEventID string) error

	// SubscribeFiltered is similar to Subscribe but each event before being
	// sent to client will be passed to given filtering function. Events
	// returned by the filtering function will be used instead.
	SubscribeFiltered(w http.ResponseWriter, r *http.Request, lastEventID string, f FilterFn) error
}

// MultiStream is an abstraction of multiple SSE streams. Single instance of
// object could be used to transmit multiple independent SSE stream. Each stream
// is identified by a unique topic name. Application can broadcast events using
// stream.PublishTopic method. HTTP handlers for SSE client endpoints should use
// stream.SubscribeTopic to tap into the event stream.
type MultiStream interface {
	// PublishTopic broadcast given event to all currently connected clients
	// (subscribers) on a given topic.
	//
	// Publish on a stopped stream will cause panic.
	PublishTopic(topic string, event *Event)

	// PublishBroadcast emits given event to all connected subscribers (for
	// all topics).
	PublishBroadcast(event *Event)

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

	// SubscribeTopic handles HTTP request to receive SSE stream for a given
	// topic. Caller is responsible for extracting Last event ID value from
	// the request.
	//
	// Subscribe on a stopped stream will cause panic.
	SubscribeTopic(w http.ResponseWriter, topic string, lastEventID string) error

	// SubscribeTopicFiltered is similar to Subscribe but each event before being
	// sent to client will be passed to given filtering function. Events
	// returned by the filtering function will be used instead.
	SubscribeTopicFiltered(w http.ResponseWriter, topic string, lastEventID string, f FilterFn) error
}

type MultiStreamWithContext interface {
	// PublishTopic broadcast given event to all currently connected clients
	// (subscribers) on a given topic.
	//
	// Publish on a stopped stream will cause panic.
	PublishTopic(topic string, event *Event)

	// PublishBroadcast emits given event to all connected subscribers (for
	// all topics).
	PublishBroadcast(event *Event)

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

	// SubscribeTopic handles HTTP request to receive SSE stream for a given
	// topic. Caller is responsible for extracting Last event ID value from
	// the request.
	//
	// Subscribe on a stopped stream will cause panic.
	SubscribeTopic(w http.ResponseWriter, r *http.Request, topic string, lastEventID string) error

	// SubscribeTopicFiltered is similar to Subscribe but each event before being
	// sent to client will be passed to given filtering function. Events
	// returned by the filtering function will be used instead.
	SubscribeTopicFiltered(w http.ResponseWriter, r *http.Request, topic string, lastEventID string, f FilterFn) error
}

// ResyncFn is a definition of function used to lookup events missed by
// client reconnects. Users of this package must provide an implementation of
// this function when creating new streams.
//
// For multi-streams topic argument will be set to sub-stream name, for
// single-streams it will be empty string.
//
// This function takes two event ID values as an argument and should return all
// events having IDs in interval (fromID, toID]. ResyncFn will be called
// repeatedly until an empty events slice is returned (no more missing events)
// or ResyncEventsThreshold is reached. Note that event with ID equal to fromID
// SHOULD NOT be included, but event with toID SHOULD be included. Argument
// fromID can be empty string if empty string was passed to stream.Subscribe(),
// it usually means client has connected to the SSE stream for the first time.
// Argument toID can also be empty string if empty string was passed as initial
// last event ID when stream was created and client have connected to the SSE
// stream before any events were published using stream.Publish().
//
// ResyncFn should return all events in a given range in a slice. If the second
// return variable err is not nil the subscription will disconnect with error.
//
// Correct implementation of this function is essential for proper client
// resync and vital to whole SSE functionality.
type ResyncFn func(topic string, fromID, toID string) (events []Event, err error)

// FilterFn is a callback function used to mutate event stream for individual
// subscriptions. This function will be invoked for each event before sending it
// to the client, result of this function will be sent instead of original
// event. If this function returns `nil` event will be omitted.
//
// Original event passed to this function should NOT be mutated. Filtering
// function with the same event data will be called in separate per-subscriber
// go-routines. Event mutation will cause guaranteed data race condition. If
// event needs to be altered fresh copy needs to be returned.
type FilterFn func(e *Event) *Event
