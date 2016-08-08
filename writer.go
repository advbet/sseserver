package ssestream

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
	// if nginx proxy is used. By default nginx will timeout the request if
	// there is more than 60 seconds gap between two successive reads.
	KeepAlive time.Duration

	// Lifetime is a maximum amount of time connection is allowed to stay
	// open before a forced reconnect. Setting Lifetime to zero allows SSE
	// connections to be open indefinitely.
	Lifetime time.Duration
}

// Event holds data for single event in SSE stream.
type Event struct {
	ID    interface{} // ID value will be converted to string with fmt package
	Event string
	Data  interface{} // Data value will be marshaled to JSON
}

// DefaultConfig is a recommended SSE configuration.
var DefaultConfig = Config{
	Reconnect: 500 * time.Millisecond,
	KeepAlive: 30 * time.Second,
	Lifetime:  5 * time.Minute,
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

// Respond reads Events from a channel and writes SSE HTTP reponse. This
// function uses SSE configuration stored in cfg, if nill is passed default SSE
// configuration from DefaultConfig global variable will be used.
//
// Stop is an optional channel for stopping SSE stream, if this channel is
// closed or a value is received on it SSE stream will close. Please note that
// receiving a value on stop channel will close only one SSE stream if multiple
// stream shares single stop channel. If stream stopping functionality is not
// required Stop should be set to nil.
//
// This function returns nil if end of stream is reached, stream lifetime
// expired, client closes the connection or request to stop is received on the
// stop channel. Otherwise it returns an error.
//
// Note! After passing source channel to Stream it cannot be reused (for example
// passed to the Stream function again). This function will start a new
// goroutine to drain source channel on exit.
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

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

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
	if e.ID != nil {
		if _, err := fmt.Fprintf(w, "id: %v\n", e.ID); err != nil {
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
	if err := json.NewEncoder(w).Encode(e.Data); err != nil {
		return err
	}
	// Here we rely on the fact that json encoder will add a single "\n" at
	// the end of the JSON object. If json library is changed to include
	// other separator or omit separator all-together two new-lines should
	// be printed after data.
	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return err
	}
	return nil
}
