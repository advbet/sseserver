package sseserver

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var _ Stream = &GenericStream{}
var _ MultiStream = &GenericStream{}

func resyncGenerator(events []Event, ok bool) ResyncFn {
	return func(topic string, fromID, toID interface{}) ([]Event, bool) {
		return events, ok
	}
}

func TestGenericDisconnect(t *testing.T) {
	stream := NewGeneric(resyncGenerator(nil, false), nil, Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	})
	defer stream.Stop()

	w := httptest.NewRecorder()
	stream.Subscribe(w, nil)
	assertReceivedEvents(t, w)
}

func TestGenericStaticEvents(t *testing.T) {
	expected := []Event{{ID: 1}, {ID: 2}}
	stream := NewGeneric(resyncGenerator(expected, false), nil, Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	})
	defer stream.Stop()

	w := httptest.NewRecorder()
	stream.Subscribe(w, nil)
	assertReceivedEvents(t, w, expected...)
}

func TestGenericInitialLastEventID(t *testing.T) {
	initialID := 15
	var actualID interface{}
	resync := func(topcic string, fromID, toID interface{}) ([]Event, bool) {
		actualID = toID
		return nil, false
	}
	stream := NewGeneric(resync, initialID, Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	})
	defer stream.Stop()

	w := httptest.NewRecorder()
	stream.Subscribe(w, nil)
	assertReceivedEvents(t, w)
	assert.Equal(t, initialID, actualID)
}

func TestGenericResyncTopic(t *testing.T) {
	const topic = "some-topic"
	var receivedTopic string
	resync := func(topcic string, fromID, toID interface{}) ([]Event, bool) {
		receivedTopic = topic
		return nil, false
	}
	stream := NewGeneric(resync, nil, Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	})
	defer stream.Stop()

	w := httptest.NewRecorder()
	stream.SubscribeTopic(w, topic, nil)
	assertReceivedEvents(t, w)
	assert.Equal(t, topic, receivedTopic, "resync function received another topic")
}

func TestPrependStream(t *testing.T) {
	events := []Event{
		{ID: 1},
		{ID: 2},
	}

	stream := make(chan *Event, 2)
	stream <- &Event{ID: 3}
	stream <- &Event{ID: 4}
	close(stream)

	expected := []Event{
		{ID: 1},
		{ID: 2},
		{ID: 3},
		{ID: 4},
	}

	combined := prependStream(events, stream)
	// Check if combined stream contains expected list of events
	for _, event := range expected {
		e, ok := <-combined
		assert.True(t, ok)
		assert.Equal(t, event, *e)
	}

	// Check if stream is closed afterwards
	_, ok := <-combined
	assert.False(t, ok)
}

// TestPrependStreamStatic checks if prependStream works correctly if nil is
// passed instead of source stream
func TestPrependStreamStatic(t *testing.T) {
	events := []Event{
		{ID: 1},
		{ID: 2},
	}

	combined := prependStream(events, nil)
	// Check if combined stream contains expected list of events
	for _, event := range events {
		e, ok := <-combined
		assert.True(t, ok)
		assert.Equal(t, event, *e)
	}

	// Check if stream is closed afterwards
	_, ok := <-combined
	assert.False(t, ok)
}
