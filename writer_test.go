package sseserver

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

type writerNotFlusher struct{}

func (w writerNotFlusher) Header() http.Header       { return make(http.Header) }
func (w writerNotFlusher) Write([]byte) (int, error) { return 0, errors.New("not implemented") }
func (w writerNotFlusher) WriteHeader(int)           {}

func recordResponse(t *testing.T, source <-chan *Event, c *Config, stop <-chan struct{}) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	err := Respond(w, source, c, stop)
	if err != nil {
		t.Errorf("Expected nil error, got %v", err)
	}
	return w
}

func TestRespondWithoutFlusher(t *testing.T) {
	t.Parallel()

	var w writerNotFlusher

	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when using non-flusher writer, but no panic occurred")
		}
	}()

	_ = Respond(w, make(<-chan *Event), nil, nil)
}

func TestRespondReconnect(t *testing.T) {
	t.Parallel()

	source := make(chan *Event)
	// Make sure request ends because source is drained
	close(source)

	// Check if retry: is added to stream
	w := recordResponse(t, source, &Config{
		Reconnect: 99 * time.Millisecond,
	}, nil)
	if !bytes.Contains(w.Body.Bytes(), []byte("retry: 99\n")) {
		t.Error("Expected response to contain 'retry: 99\\n', but it doesn't")
	}

	// Check if retry: is omitted
	w = recordResponse(t, source, &Config{
		Reconnect: 0 * time.Millisecond,
	}, nil)
	if bytes.Contains(w.Body.Bytes(), []byte("retry:")) {
		t.Error("Expected response not to contain 'retry:', but it does")
	}
}

func TestRespondContentType(t *testing.T) {
	t.Parallel()

	source := make(chan *Event)
	// Make sure request ends because source is drained.
	close(source)

	w := recordResponse(t, source, &Config{}, nil)
	contentType := w.Header().Get("Content-Type")
	expected := "text/event-stream; charset=utf-8"
	if contentType != expected {
		t.Errorf("Content-Type header is missing or invalid, expected %q, got %q", expected, contentType)
	}
}

func TestRespondTimeout(t *testing.T) {
	t.Parallel()

	source := make(chan *Event)
	// Close source after 1 second if timeout does not work.
	time.AfterFunc(1*time.Second, func() { close(source) })

	start := time.Now()
	lifetime := 50 * time.Millisecond
	recordResponse(t, source, &Config{
		Lifetime: lifetime,
	}, nil)
	end := time.Now()

	if duration := end.Sub(start); duration > lifetime*2 {
		t.Errorf("Response took too long, expected under %v, got %v", lifetime*2, duration)
	}
}

func TestRespondKeepAlive(t *testing.T) {
	t.Parallel()

	source := make(chan *Event)
	// Close source after 50 milliseconds
	time.AfterFunc(50*time.Millisecond, func() { close(source) })

	w := recordResponse(t, source, &Config{
		KeepAlive: 30 * time.Millisecond,
	}, nil)
	if !bytes.Contains(w.Body.Bytes(), []byte(":keep-alive\n")) {
		t.Error("Expected response to contain ':keep-alive\\n', but it doesn't")
	}
}

func TestRespondStop(t *testing.T) {
	t.Parallel()

	source := make(chan *Event)
	// Close source after 1 second if close does not work
	time.AfterFunc(1*time.Second, func() { close(source) })

	stop := make(chan struct{})
	stopTimeout := 50 * time.Millisecond
	time.AfterFunc(stopTimeout, func() { close(stop) })

	start := time.Now()
	recordResponse(t, source, &Config{}, stop)
	end := time.Now()

	if duration := end.Sub(start); duration > stopTimeout*2 {
		t.Errorf("Response took too long, expected under %v, got %v", stopTimeout*2, duration)
	}
}

func TestRespondWrite(t *testing.T) {
	t.Parallel()

	source := make(chan *Event, 1)
	expected := []byte("id: 42\nevent: single\ndata: \"body\"\n\n")

	source <- &Event{
		ID:    "42",
		Event: "single",
		Data:  "body",
	}
	close(source)

	w := recordResponse(t, source, &Config{}, nil)
	if !bytes.Equal(expected, w.Body.Bytes()) {
		t.Errorf("Unexpected response body\nexpected: %q\ngot: %q", expected, w.Body.Bytes())
	}
}

type customResponseRecorder struct {
	closeChan chan bool
	*httptest.ResponseRecorder
}

func (rw *customResponseRecorder) CloseNotify() <-chan bool { return rw.closeChan }

func TestRespondCloseNotify(t *testing.T) {
	t.Parallel()

	source := make(chan *Event)
	// Close source after 1 second if client close does not work
	time.AfterFunc(1*time.Second, func() { close(source) })

	w := &customResponseRecorder{make(chan bool, 1), httptest.NewRecorder()}
	closeTimeout := 50 * time.Millisecond
	time.AfterFunc(closeTimeout, func() { w.closeChan <- true })

	start := time.Now()
	_ = Respond(w, source, &Config{}, nil)
	end := time.Now()

	if duration := end.Sub(start); duration > closeTimeout*2 {
		t.Errorf("Response took too long, expected under %v, got %v", closeTimeout*2, duration)
	}
}

func TestApplyFilter(t *testing.T) {
	t.Parallel()

	evenFilterGen := func() FilterFn {
		var n int
		return func(e *Event) *Event {
			n++
			if n%2 == 1 {
				return e
			}

			return nil
		}
	}

	tests := []struct {
		msg    string
		filter FilterFn
		input  []*Event
		output []*Event
	}{
		{
			msg:    "nil filter",
			input:  []*Event{{ID: "1"}, {ID: "2"}},
			output: []*Event{{ID: "1"}, {ID: "2"}},
		},
		{
			msg:    "identity filter",
			filter: func(e *Event) *Event { return e },
			input:  []*Event{{ID: "1"}, {ID: "2"}},
			output: []*Event{{ID: "1"}, {ID: "2"}},
		},
		{
			msg:    "even only filter",
			filter: evenFilterGen(),
			input:  []*Event{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
			output: []*Event{{ID: "a"}, {ID: "c"}},
		},
	}

	for _, test := range tests {
		t.Run(test.msg, func(t *testing.T) {
			t.Parallel()

			in := make(chan *Event, len(test.input))
			for _, e := range test.input {
				in <- e
			}

			close(in)

			output := make([]*Event, 0)
			for e := range applyChanFilter(in, test.filter) {
				output = append(output, e)
			}

			if !reflect.DeepEqual(test.output, output) {
				t.Errorf("Expected %+v, got %+v", test.output, output)
			}
		})
	}
}
