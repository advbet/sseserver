package sseserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func voidLookup(fromID, toID interface{}) (events []Event, ok bool) {
	return nil, true
}

// TestStreamLastID checks if subscribing to stream correctly returns last
// published event ID
func TestStreamLastID(t *testing.T) {
	s := NewGeneric(voidLookup, nil, DefaultConfig).(*stream)
	defer s.Stop()

	// Before any publishings last ID will be nil
	lastID := s.subscribe(make(chan *Event))
	assert.Equal(t, nil, lastID)

	s.Publish(&Event{ID: 2})
	lastID = s.subscribe(make(chan *Event))
	assert.Equal(t, 2, lastID)

	s.Publish(&Event{ID: 4})
	lastID = s.subscribe(make(chan *Event))
	assert.Equal(t, 4, lastID)
}

// TestStreamStop cheks if stopping stream closes all subscribers.
func TestStreamStop(t *testing.T) {
	s := NewGeneric(voidLookup, nil, DefaultConfig).(*stream)
	sub := make(chan *Event, 10)
	s.subscribe(sub)

	s.Stop()

	// reading from closed channel should fail
	_, ok := <-sub
	assert.False(t, ok)
}

// TestStreamPublish cheks if published event is broadcasted to all of the
// subscribers.
func TestStreamPublish(t *testing.T) {
	s := NewGeneric(voidLookup, nil, DefaultConfig).(*stream)
	defer s.Stop()
	event := &Event{ID: 15, Event: "test", Data: "ok"}
	subs := []chan *Event{
		make(chan *Event, 10),
		make(chan *Event, 10),
		make(chan *Event, 10),
	}

	for _, sub := range subs {
		s.subscribe(sub)
	}

	s.Publish(event)
	for _, sub := range subs {
		e, ok := <-sub
		assert.True(t, ok)
		assert.Equal(t, event, e)
	}
}

// TestStreamPublishFull cheks if subscribers with full channels are
// unsubscribed and closed.
func TestStreamPublishFull(t *testing.T) {
	s := NewGeneric(voidLookup, nil, DefaultConfig).(*stream)
	defer s.Stop()

	sub := make(chan *Event, 3)
	s.subscribe(sub)

	// Overflow subscriber channel
	for i := 0; i < cap(sub)*2; i++ {
		s.Publish(&Event{ID: i})
	}

	// Drain subscriber channel
	for i := 0; i < cap(sub); i++ {
		e, ok := <-sub
		assert.True(t, ok)
		assert.Equal(t, &Event{ID: i}, e)
	}

	// Check if subscriber is closed
	_, ok := <-sub
	assert.False(t, ok)
}

// TestStreamUnsubscribe checks if unsubscribing closes events channel
func TestStreamUnsubscribe(t *testing.T) {
	s := NewGeneric(voidLookup, nil, DefaultConfig).(*stream)
	defer s.Stop()

	sub := make(chan *Event, 3)
	s.subscribe(sub)

	s.Publish(&Event{ID: 10})
	s.unsubscribe(sub)
	s.Publish(&Event{ID: 20})

	// subscriber should receive only first event and have its channel
	// closed
	e, ok := <-sub
	assert.True(t, ok)
	assert.Equal(t, &Event{ID: 10}, e)

	_, ok = <-sub
	assert.False(t, ok)

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
