package sseserver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var _ Stream = &GenericStream{}
var _ MultiStream = &GenericStream{}

func resyncGenerator(events []Event, err error) ResyncFn {
	return func(topic string, fromID, toID interface{}) ([]Event, error) {
		return events, err
	}
}

func TestGenericDisconnect(t *testing.T) {
	stream := NewGeneric(resyncGenerator(nil, errors.New("error")), nil, Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 10000,
	})
	defer stream.Stop()

	w := httptest.NewRecorder()
	stream.Subscribe(w, nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGenericResyncThreshold(t *testing.T) {
	expected := []Event{{ID: 1}, {ID: 2}}
	stream := NewGeneric(resyncGenerator(expected, nil), nil, Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 1,
	})
	defer stream.Stop()

	w := httptest.NewRecorder()
	stream.Subscribe(w, nil)
	assertReceivedEvents(t, w, expected...)
}

func TestGenericResyncBeforeDisconnect(t *testing.T) {
	expected := []Event{{ID: 1}, {ID: 2}}
	var synced bool
	resync := func(topic string, fromID, toID interface{}) ([]Event, error) {
		if !synced {
			synced = true
			return expected, nil
		}
		return nil, errors.New("synced")
	}
	stream := NewGeneric(resync, nil, Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 5,
	})
	defer stream.Stop()

	// Get resynced events
	w1 := httptest.NewRecorder()
	stream.Subscribe(w1, nil)
	assertReceivedEvents(t, w1, expected...)

	// Client reconnects after resync
	w2 := httptest.NewRecorder()
	stream.Subscribe(w2, 2)
	assert.Equal(t, http.StatusInternalServerError, w2.Code)
}

func TestGenericInitialLastEventID(t *testing.T) {
	initialID := 15
	var actualID interface{}
	resync := func(topcic string, fromID, toID interface{}) ([]Event, error) {
		actualID = toID
		return nil, nil
	}
	stream := NewGeneric(resync, initialID, Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 10000,
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
	resync := func(topic string, fromID, toID interface{}) ([]Event, error) {
		receivedTopic = topic
		return nil, nil
	}
	stream := NewGeneric(resync, nil, Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 10000,
	})
	defer stream.Stop()

	w := httptest.NewRecorder()
	stream.SubscribeTopic(w, topic, 0)
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
