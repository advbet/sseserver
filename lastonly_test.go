package sseserver

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var _ Stream = &LastOnlyStream{}
var _ MultiStream = &LastOnlyStream{}

func TestLastOnlyResync(t *testing.T) {
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
		stream.Subscribe(w, "")
		// client should receive only last event
		assertReceivedEvents(t, w, event2)
	})

	t.Run("id old", func(t *testing.T) {
		w := httptest.NewRecorder()
		stream.Subscribe(w, "16")
		// client should receive only last event
		assertReceivedEvents(t, w, event2)
	})

	t.Run("id up to date", func(t *testing.T) {
		w := httptest.NewRecorder()
		stream.Subscribe(w, "32")
		// connect with up to date last event ID,
		// should not generate duplicate events.
		assertReceivedEvents(t, w)
	})
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
	event1 := Event{ID: "1"}
	stream.PublishTopic("topic1", &event1)
	event2 := Event{ID: "2"}
	stream.PublishTopic("topic2", &event2)

	t.Run("with topic1", func(t *testing.T) {
		w := httptest.NewRecorder()
		stream.SubscribeTopic(w, "topic1", "")
		assertReceivedEvents(t, w, event1)
	})

	t.Run("with topic2", func(t *testing.T) {
		w := httptest.NewRecorder()
		stream.SubscribeTopic(w, "topic2", "")
		assertReceivedEvents(t, w, event2)
	})
}

func TestLastPerTopic(t *testing.T) {
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
		stream.SubscribeTopic(w, "topic1", "")
		assertReceivedEvents(t, w, event1, event2v2)
	})

	t.Run("receive none", func(t *testing.T) {
		w := httptest.NewRecorder()
		stream.SubscribeTopic(w, "topic1", "3")
		assertReceivedEvents(t, w)
	})
}

func TestFilterSupport(t *testing.T) {
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
		assert.Equal(t, stream.SubscribeTopicFiltered(w, "topic1", "", f), errFiltersNotSupported)
	})

	t.Run("without filter should not error", func(t *testing.T) {
		w := httptest.NewRecorder()
		assert.Equal(t, stream.SubscribeTopicFiltered(w, "topic1", "", nil), nil)
	})
}
