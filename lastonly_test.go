package sseserver

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLastOnlyResync(t *testing.T) {
	stream := NewLastOnly(Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	})
	defer stream.Stop()

	// Publish two events
	event1 := Event{ID: 16}
	stream.Publish(&event1)
	event2 := Event{ID: 32}
	stream.Publish(&event2)

	// client should receive only last event
	w := httptest.NewRecorder()
	stream.Subscribe(w, 0)
	assertReceivedEvents(t, w, event2)

	// connect with up to date last event ID, should not generate duplicate
	// events
	w = httptest.NewRecorder()
	stream.Subscribe(w, 32)
	assertReceivedEvents(t, w)
}

func TestLastOnlyTopics(t *testing.T) {
	stream := NewLastOnly(Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	})
	defer stream.Stop()

	// Publish two events
	event1 := Event{ID: 1}
	stream.PublishTopic("topic1", &event1)
	event2 := Event{ID: 2}
	stream.PublishTopic("topic2", &event2)

	t.Run("with topic1", func(t *testing.T) {
		w := httptest.NewRecorder()
		stream.SubscribeTopic(w, "topic1", 0)
		assertReceivedEvents(t, w, event1)
	})

	t.Run("with topic2", func(t *testing.T) {
		w := httptest.NewRecorder()
		stream.SubscribeTopic(w, "topic2", 0)
		assertReceivedEvents(t, w, event2)
	})
}
