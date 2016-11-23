package sseserver

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type writerNotFlusher struct{}

func (w writerNotFlusher) Header() http.Header       { return make(http.Header) }
func (w writerNotFlusher) Write([]byte) (int, error) { return 0, errors.New("not implemented") }
func (w writerNotFlusher) WriteHeader(int)           {}

func recordResponse(t *testing.T, source <-chan *Event, c *Config, stop <-chan struct{}) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	err := Respond(w, source, c, stop)
	assert.Nil(t, err)
	return w
}

func TestRespondWithoutFlusher(t *testing.T) {
	var w writerNotFlusher

	assert.Panics(t, func() {
		_ = Respond(w, make(<-chan *Event), nil, nil)
	})
}

func TestRespondReconnect(t *testing.T) {
	source := make(chan *Event)
	// Make sure request ends because source is drained
	close(source)

	// Check if retry: is added to stream
	w := recordResponse(t, source, &Config{
		Reconnect: 99 * time.Millisecond,
	}, nil)
	assert.True(t, bytes.Contains(w.Body.Bytes(), []byte("retry: 99\n")))

	// Check if retry: is ommited
	w = recordResponse(t, source, &Config{
		Reconnect: 0 * time.Millisecond,
	}, nil)
	assert.False(t, bytes.Contains(w.Body.Bytes(), []byte("retry:")))
}

func TestRespondContentType(t *testing.T) {
	source := make(chan *Event)
	// Make sure request ends because source is drained
	close(source)

	w := recordResponse(t, source, &Config{}, nil)
	contentType := w.HeaderMap.Get("Content-Type")
	assert.Equal(t, "text/event-stream", contentType, "Content-Type header is missing or invalid")
}

func TestRespondTimeout(t *testing.T) {
	source := make(chan *Event)
	// Close source after 1 second if timeout does not work
	time.AfterFunc(1*time.Second, func() { close(source) })

	start := time.Now()
	lifetime := 50 * time.Millisecond
	recordResponse(t, source, &Config{
		Lifetime: lifetime,
	}, nil)
	end := time.Now()

	assert.WithinDuration(t, start, end, lifetime*2)
}

func TestRespondKeepAlive(t *testing.T) {
	source := make(chan *Event)
	// Close source after 50 miliseconds
	time.AfterFunc(50*time.Millisecond, func() { close(source) })

	w := recordResponse(t, source, &Config{
		KeepAlive: 30 * time.Millisecond,
	}, nil)
	assert.True(t, bytes.Contains(w.Body.Bytes(), []byte(":keep-alive\n")))
}

func TestRespondStop(t *testing.T) {
	source := make(chan *Event)
	// Close source after 1 second if close does not work
	time.AfterFunc(1*time.Second, func() { close(source) })

	stop := make(chan struct{})
	stopTimeout := 50 * time.Millisecond
	time.AfterFunc(stopTimeout, func() { close(stop) })

	start := time.Now()
	recordResponse(t, source, &Config{}, stop)
	end := time.Now()
	assert.WithinDuration(t, start, end, stopTimeout*2)
}

func TestRespondWrite(t *testing.T) {
	source := make(chan *Event, 1)
	expected := []byte("id: 42\nevent: single\ndata: \"body\"\n\n")

	source <- &Event{
		ID:    "42",
		Event: "single",
		Data:  "body",
	}
	close(source)

	w := recordResponse(t, source, &Config{}, nil)
	assert.Equal(t, expected, w.Body.Bytes())
}

type customResponseRecorder struct {
	closeChan chan bool
	*httptest.ResponseRecorder
}

func (rw *customResponseRecorder) CloseNotify() <-chan bool { return rw.closeChan }

func TestRespondCloseNotify(t *testing.T) {
	source := make(chan *Event)
	// Close source after 1 second if client close does not work
	time.AfterFunc(1*time.Second, func() { close(source) })

	w := &customResponseRecorder{make(chan bool, 1), httptest.NewRecorder()}
	closeTimeout := 50 * time.Millisecond
	time.AfterFunc(closeTimeout, func() { w.closeChan <- true })

	start := time.Now()
	_ = Respond(w, source, &Config{}, nil)
	end := time.Now()

	assert.WithinDuration(t, start, end, closeTimeout*2)
}
