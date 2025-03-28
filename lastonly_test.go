package sseserver

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

var (
	_ StreamWithContext      = &LastOnlyStream{}
	_ MultiStreamWithContext = &LastOnlyStream{}
)

func TestLastOnlyResync(t *testing.T) {
	t.Parallel()

	stream := NewLastOnly(Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	})
	defer stream.Stop()

	// Publish two events
	event1 := Event{ID: "16"}
	stream.Publish(&event1)
	event2 := Event{ID: "32"}
	stream.Publish(&event2)

	t.Run("no id", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		_ = stream.Subscribe(w, r, "")
		// client should receive only last event
		assertReceivedEvents(t, w, event2)
	})

	t.Run("id old", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		_ = stream.Subscribe(w, r, "16")
		// client should receive only last event
		assertReceivedEvents(t, w, event2)
	})

	t.Run("id up to date", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		_ = stream.Subscribe(w, r, "32")
		// connect with up to date last event ID,
		// should not generate duplicate events.
		assertReceivedEvents(t, w)
	})
}

func TestLastOnlyTopics(t *testing.T) {
	t.Parallel()

	stream := NewLastOnly(Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	})
	defer stream.Stop()

	// Publish two events
	event1 := Event{ID: "1"}
	stream.PublishTopic("topic1", &event1)
	event2 := Event{ID: "2"}
	stream.PublishTopic("topic2", &event2)

	t.Run("with topic1", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		_ = stream.SubscribeTopic(w, r, "topic1", "")
		assertReceivedEvents(t, w, event1)
	})

	t.Run("with topic2", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		_ = stream.SubscribeTopic(w, r, "topic2", "")
		assertReceivedEvents(t, w, event2)
	})
}

func TestLastPerTopic(t *testing.T) {
	t.Parallel()

	stream := NewLastOnly(Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	})
	defer stream.Stop()

	// Publish three events to the same topic with different `event` values
	// Client should only receive the last for each topic and event combination.
	event1 := Event{ID: "1", Event: "name1"}
	event2 := Event{ID: "2", Event: "name2"}
	event2v2 := Event{ID: "3", Event: "name2"}
	stream.PublishTopic("topic1", &event1)
	stream.PublishTopic("topic1", &event2)
	stream.PublishTopic("topic1", &event2v2)

	t.Run("receive ids 1 and 3", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		_ = stream.SubscribeTopic(w, r, "topic1", "")
		assertReceivedEvents(t, w, event1, event2v2)
	})

	t.Run("receive none", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		_ = stream.SubscribeTopic(w, r, "topic1", "3")
		assertReceivedEvents(t, w)
	})
}

func TestFilterSupport(t *testing.T) {
	t.Parallel()

	stream := NewLastOnly(Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	})
	defer stream.Stop()

	f := FilterFn(func(e *Event) *Event { return nil })
	t.Run("with filter should error", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		err := stream.SubscribeTopicFiltered(w, r, "topic1", "", f)
		if !errors.Is(err, errFiltersNotSupported) {
			t.Errorf("Expected error %v, got %v", errFiltersNotSupported, err)
		}
	})

	t.Run("without filter should not error", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
		err := stream.SubscribeTopicFiltered(w, r, "topic1", "", nil)
		if err != nil {
			t.Errorf("Expected nil error, got %v", err)
		}
	})
}
