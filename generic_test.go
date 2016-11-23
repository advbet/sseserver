package sseserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func voidLookup(fromID, toID interface{}) (events []Event, ok bool) {
	return nil, true
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
