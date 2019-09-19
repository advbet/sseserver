package sseserver

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var _ Stream = &GenericStream{}
var _ MultiStream = &GenericStream{}

func resyncGenerator(events []Event, err error) ResyncFn {
	return func(topic string, fromID, toID string) ([]Event, error) {
		return events, err
	}
}

func TestGenericDisconnect(t *testing.T) {
	resyncErr := errors.New("error")
	stream := NewGeneric(resyncGenerator(nil, resyncErr), "first", Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 10000,
	})
	defer stream.Stop()

	w := httptest.NewRecorder()
	assert.Equal(t, resyncErr, stream.Subscribe(w, "first"))
}

func TestGenericResyncThreshold(t *testing.T) {
	expected := []Event{{ID: "1"}, {ID: "2"}}
	stream := NewGeneric(resyncGenerator(expected, nil), "first", Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 1,
	})
	defer stream.Stop()

	w := httptest.NewRecorder()
	stream.Subscribe(w, "")
	assertReceivedEvents(t, w, expected...)
}

func TestGenericResyncBeforeDisconnect(t *testing.T) {
	expected := []Event{{ID: "1"}, {ID: "2"}}
	var synced bool
	errSynced := errors.New("synced")
	resync := func(topic string, fromID, toID string) ([]Event, error) {
		if !synced {
			synced = true
			return expected, nil
		}
		return nil, errSynced
	}
	stream := NewGeneric(resync, "first", Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 5,
	})
	defer stream.Stop()

	// Get resynced events
	w1 := httptest.NewRecorder()
	assert.Equal(t, nil, stream.Subscribe(w1, ""))
	assertReceivedEvents(t, w1, expected...)

	// Client reconnects after resync
	w2 := httptest.NewRecorder()
	assert.Equal(t, errSynced, stream.Subscribe(w2, "2"))
}

func TestGenericInitialLastEventID(t *testing.T) {
	initialID := "15"
	var actualID string
	resync := func(topcic string, fromID, toID string) ([]Event, error) {
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
	stream.Subscribe(w, "")
	assertReceivedEvents(t, w)
	assert.Equal(t, initialID, actualID)
}

func TestGenericResyncTopic(t *testing.T) {
	const topic = "some-topic"
	var receivedTopic string
	resync := func(topic string, fromID, toID string) ([]Event, error) {
		receivedTopic = topic
		return nil, nil
	}
	stream := NewGeneric(resync, "first", Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 10000,
	})
	defer stream.Stop()

	w := httptest.NewRecorder()
	stream.SubscribeTopic(w, topic, "0")
	assertReceivedEvents(t, w)
	assert.Equal(t, topic, receivedTopic, "resync function received another topic")
}

func TestPrependStream(t *testing.T) {
	events := []Event{
		{ID: "1"},
		{ID: "2"},
	}

	stream := make(chan *Event, 2)
	stream <- &Event{ID: "3"}
	stream <- &Event{ID: "4"}
	close(stream)

	expected := []Event{
		{ID: "1"},
		{ID: "2"},
		{ID: "3"},
		{ID: "4"},
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
		{ID: "1"},
		{ID: "2"},
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
