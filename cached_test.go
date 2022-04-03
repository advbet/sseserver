package sseserver

import (
	"bytes"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var _ MultiStream = &CachedStream{}
var _ Stream = &CachedStream{}

func assertReceivedEvents(t *testing.T, resp *httptest.ResponseRecorder, events ...Event) {
	var buf bytes.Buffer

	for _, event := range events {
		write(&buf, &event)
	}

	assert.Equal(t, buf.String(), resp.Body.String())
}

func TestCachedResync(t *testing.T) {
	stream := NewCached("first", Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 2,
	}, time.Minute, time.Minute)
	defer stream.Stop()

	// Publish two events
	event1 := Event{ID: "16"}
	stream.Publish(&event1)
	event2 := Event{ID: "32"}
	stream.Publish(&event2)

	w := httptest.NewRecorder()
	// connect with initial last event ID to receive both cached events
	stream.Subscribe(w, "first")

	// Assert both events were received
	assertReceivedEvents(t, w, event1, event2)
}

func TestCachedResyncWithBroadcast(t *testing.T) {
	stream := NewCached("first", Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 2,
	}, time.Minute, time.Minute)
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
	stream.Subscribe(w, "first")

	// Assert both events were received
	assertReceivedEvents(t, w, event1, event2)
}

func TestCachedError(t *testing.T) {
	stream := NewCached("8", Config{
		Reconnect:   0,
		KeepAlive:   0,
		Lifetime:    10 * time.Millisecond,
		QueueLength: 32,
	}, time.Minute, time.Minute)
	defer stream.Stop()

	w := httptest.NewRecorder()
	// resyncing from non existant event ID should return error
	err := stream.Subscribe(w, "non exitant")
	assert.Equal(t, ErrCacheMiss, err)
}

func TestCachedResyncTopics(t *testing.T) {
	stream := NewCachedMultiStream(map[string]string{
		"topic1": "first1",
		"topic2": "first2",
	}, Config{
		Reconnect:             0,
		KeepAlive:             0,
		Lifetime:              10 * time.Millisecond,
		QueueLength:           32,
		ResyncEventsThreshold: 5,
	}, time.Minute, time.Minute)
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
		stream.SubscribeTopic(w, "topic1", "first1")
		assertReceivedEvents(t, w, events1...)
	})
	t.Run("with topic2", func(t *testing.T) {
		w := httptest.NewRecorder()
		stream.SubscribeTopic(w, "topic2", "first2")
		assertReceivedEvents(t, w, events2...)
	})
}
