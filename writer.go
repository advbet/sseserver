package sseserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Config holds SSE stream configuration. Single Config instance can be safely
// used in multiple go routines (http request handlers) simultaneously without
// locking.
type Config struct {
	// Reconnect is a time duration before successive reconnects, it is
	// passed as a recommendation for SSE clients. Setting Reconnect to zero
	// disables sending a reconnect hint and client will use its default
	// value. Recommended value is 500 milliseconds.
	Reconnect time.Duration

	// KeepAlive sets how often SSE stream should include a dummy keep alive
	// message. Setting KeepAlive to zero disables sending keep alive
	// messages. It is recommended to keep this value lower than 60 seconds
	// if nginx proxy is used. By default, nginx will timeout the request if
	// there is more than 60 seconds gap between two successive reads.
	KeepAlive time.Duration

	// Lifetime is a maximum amount of time connection is allowed to stay
	// open before a forced reconnect. Setting Lifetime to zero allows SSE
	// connections to be open indefinitely.
	Lifetime time.Duration

	// QueueLength is the maximum number of events pending to be transmitted
	// to the client before connection is closed. Note queue length of 0
	// should be never used, recommended size is 32.
	QueueLength int

	// ResyncEventsThreshold is the threshold number of events that can be
	// returned to the client after resync with a last seen event id. After
	// the threshold is crossed no more attempts at a resync will be performed
	// and the client will be disconnected.
	ResyncEventsThreshold int
}

// Event holds data for single event in SSE stream.
type Event struct {
	ID    string      // ID value
	Event string      // Event type value
	Data  interface{} // Data value will be marshaled to JSON
}

// DefaultConfig is a recommended SSE configuration.
var DefaultConfig = Config{
	Reconnect:             500 * time.Millisecond,
	KeepAlive:             30 * time.Second,
	Lifetime:              5 * time.Minute,
	QueueLength:           32,
	ResyncEventsThreshold: 1000,
}

var errFlusherIface = errors.New("http.ResponseWriter does not implement http.Flusher interface")

// drain reads and discards all data from source channel in a separate go
// routine
func drain(source <-chan *Event) {
	go func() {
		for range source {
		}
	}()
}

// Respond reads Events from a channel and writes SSE HTTP response. function
// provides a lower level API that allows manually generating SSE stream. In
// most cases this function should not be used directly.
//
// Cfg is SSE stream configuration, if nil is passed configuration from
// DefaultConfiguration global will be used.
//
// Stop is an optional channel for stopping SSE stream, if this channel is
// closed SSE stream will stop and http connection closed. If stream stopping
// functionality is not required Stop should be set to nil.
//
// This function returns nil if end of stream is reached, stream lifetime
// expired, client closes the connection or request to stop is received on the
// stop channel. Otherwise, it returns an error.
//
// Note! After passing source channel to Stream it cannot be reused (for example
// passed to the Stream function again). This function will drain source channel
// on exit.
func Respond(w http.ResponseWriter, source <-chan *Event, cfg *Config, stop <-chan struct{}) error {
	// Draining the source stream can help protect against resource leaks if
	// too small source chan buffer size was used and producer is stuck on
	// trying to send more data. It has a downside that source channel will
	// become useless after call to Stream.
	defer drain(source)

	if cfg == nil {
		cfg = &DefaultConfig
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		panic(errFlusherIface)
	}

	var closeChan <-chan bool
	//nolint:staticcheck
	if notifier, ok := w.(http.CloseNotifier); ok {
		closeChan = notifier.CloseNotify()
	}

	var timeoutChan <-chan time.Time
	if cfg.Lifetime > 0 {
		timeoutChan = time.After(cfg.Lifetime)
	}

	var keepaliveChan <-chan time.Time
	if cfg.KeepAlive > 0 {
		ticker := time.NewTicker(cfg.KeepAlive)
		defer ticker.Stop()
		keepaliveChan = ticker.C
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	// Instruct nginx to disable buffering
	w.Header().Set("X-Accel-Buffering", "no")

	if cfg.Reconnect != 0 {
		if _, err := fmt.Fprintf(w, "retry: %d\n\n", cfg.Reconnect/time.Millisecond); err != nil {
			return err
		}
		flusher.Flush()
	}

loop:
	for {
		select {
		case <-timeoutChan:
			// Stream lifetime has ended, client should reconnect
			break loop
		case <-stop:
			// Caller requests to stop serving SSE stream
			break loop
		case <-closeChan:
			// Client closed the connection
			break loop
		case <-keepaliveChan:
			if _, err := io.WriteString(w, ":keep-alive\n\n"); err != nil {
				return err
			}
			flusher.Flush()
		case event, ok := <-source:
			if !ok {
				// Source is drained
				break loop
			}
			if err := write(w, event); err != nil {
				return err
			}
			flusher.Flush()
		}
	}
	return nil
}

// Write dumps single event in SSE wire format to a http.ResponseWriter.
// Flushing should be performed by the caller.
func write(w io.Writer, e *Event) error {
	if e == nil {
		return nil
	}
	if e.ID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", e.ID); err != nil {
			return err
		}
	}

	if e.Event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", e.Event); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprint(w, "data: "); err != nil {
		return err
	}

	// We must ensure that two empty lines are used after writing the
	// data.
	switch data := e.Data.(type) {
	case []byte:
		if _, err := w.Write(data); err != nil {
			return err
		}

		if _, err := fmt.Fprint(w, "\n"); err != nil {
			return err
		}
	default:
		// Here we rely on the fact that json encoder will add a single "\n" at
		// the end of the JSON object.
		if err := json.NewEncoder(w).Encode(e.Data); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return err
	}

	return nil
}

func applyChanFilter(input <-chan *Event, f FilterFn) <-chan *Event {
	if f == nil {
		return input
	}

	output := make(chan *Event)
	go func() {
		defer close(output)
		for e := range input {
			event := f(e)
			if event != nil {
				output <- event
			}
		}
	}()
	return output
}

func applySliceFilter(events []Event, f FilterFn) []Event {
	result := make([]Event, 0)
	for _, event := range events {
		if e := f(&event); e != nil {
			result = append(result, *e)
		}
	}

	return result
}
