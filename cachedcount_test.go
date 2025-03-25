package sseserver

import (
	"errors"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

var _ MultiStream = &CachedCountStream{}
var _ Stream = &CachedCountStream{}

func TestCachedCountResync(t *testing.T) {
	stream := NewCachedCount("first", Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	}, 2)
	defer stream.Stop()

	// Publish two events
	event1 := Event{ID: "16"}
	stream.Publish(&event1)
	event2 := Event{ID: "32"}
	stream.Publish(&event2)

	w := httptest.NewRecorder()
	// connect with initial last event ID to receive both cached events
	_ = stream.Subscribe(w, "first")

	// Assert both events were received
	assertReceivedEvents(t, w, event1, event2)

	event3 := Event{ID: "64"}
	stream.Publish(&event3)

	w = httptest.NewRecorder()
	// cache size limit passed
	err := stream.Subscribe(w, "first")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("Expected error: %v, got: %v", ErrCacheMiss, err)
	}
}

func TestCachedCountResyncWithBroadcast(t *testing.T) {
	stream := NewCachedCount("first", Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	}, 5)
	defer stream.Stop()

	// Publish two events, with broadcast in between
	event1 := Event{ID: "16"}
	stream.Publish(&event1)
	stream.PublishBroadcast(&Event{ID: "999"})
	event2 := Event{ID: "32"}
	stream.Publish(&event2)

	w := httptest.NewRecorder()
	// connect with initial last event ID to receive both cached events,
	// broadcasted event should be excluded
	_ = stream.Subscribe(w, "first")

	// Assert both events were received
	assertReceivedEvents(t, w, event1, event2)
}

func TestCachedCountError(t *testing.T) {
	stream := NewCachedCount("8", Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	}, 5)
	defer stream.Stop()

	w := httptest.NewRecorder()
	// resyncing from non existant event ID should return error
	err := stream.Subscribe(w, "non exitant")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("Expected error: %v, got: %v", ErrCacheMiss, err)
	}
}

func TestCachedCountResyncTopics(t *testing.T) {
	stream := NewCachedCountMultiStream(map[string]string{
		"topic1": "first1",
		"topic2": "first2",
	}, Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	}, 10)
	defer stream.Stop()

	// Generate two sub-streams of events
	var events1, events2 []Event
	for i := 0; i < 5; i++ {
		event1 := Event{ID: strconv.Itoa(i * 10)}
		events1 = append(events1, event1)
		stream.PublishTopic("topic1", &event1)

		event2 := Event{ID: strconv.Itoa(i * 20)}
		events2 = append(events2, event2)
		stream.PublishTopic("topic2", &event2)
	}

	t.Run("with topic1", func(t *testing.T) {
		w := httptest.NewRecorder()
		_ = stream.SubscribeTopic(w, "topic1", "first1")
		assertReceivedEvents(t, w, events1...)
	})
	t.Run("with topic2", func(t *testing.T) {
		w := httptest.NewRecorder()
		_ = stream.SubscribeTopic(w, "topic2", "first2")
		assertReceivedEvents(t, w, events2...)
	})
}
