package sseserver

import (
	"bytes"
	"errors"
	"fmt"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	gocache "github.com/patrickmn/go-cache"
)

var _ MultiStream = &CachedStream{}
var _ Stream = &CachedStream{}

var (
	topic          = "benchmark_topic"
	localCache     = newCache(time.Minute, time.Minute)
	patrickmnCache = gocache.New(time.Minute, time.Minute)
)

func init() {
	for n := 0; n < 10000; n++ {
		event := &Event{
			ID: strconv.Itoa(n + 1),
		}

		localCache.add(topic, strconv.Itoa(n), event)
		_ = patrickmnCache.Add(fmt.Sprintf("%s%d", topic, n), event, gocache.DefaultExpiration)
	}
}

func BenchmarkLocalCacheAdd(b *testing.B) {
	for n := 0; n < b.N; n++ {
		localCache.add(topic, strconv.Itoa(n-1), &Event{
			ID: strconv.Itoa(n),
		})
	}
}

func BenchmarkPatrickmnCacheAdd(b *testing.B) {
	for n := 0; n < b.N; n++ {
		_ = patrickmnCache.Add(fmt.Sprintf("%s%s", topic, strconv.Itoa(n-1)), &Event{
			ID: strconv.Itoa(n),
		}, gocache.DefaultExpiration)
	}
}

func BenchmarkLocalCacheGet(b *testing.B) {
	for n := 0; n < b.N; n++ {
		events, _ := localCache.get(topic, "0", "5000", 5000, nil)
		for range events {
		}
	}
}

func BenchmarkPatrickmnCacheGet(b *testing.B) {
	for n := 0; n < b.N; n++ {
		var events []*Event
		for i := 0; i < 5000; i++ {
			event, ok := patrickmnCache.Get(fmt.Sprintf("%s%d", topic, i))
			if ok {
				events = append(events, event.(*Event))
			}
		}

		for range events {
		}
	}
}

func assertReceivedEvents(t *testing.T, resp *httptest.ResponseRecorder, events ...Event) {
	var buf bytes.Buffer
	for _, event := range events {
		_ = write(&buf, &event)
	}

	if buf.String() != resp.Body.String() {
		t.Errorf("Expected response body: %q, got: %q", buf.String(), resp.Body.String())
	}
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
	_ = stream.Subscribe(w, "first")

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
	_ = stream.Subscribe(w, "first")

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
	// resyncing from non-existent event ID should return error
	err := stream.Subscribe(w, "non-existent")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("Expected error: %v, got: %v", ErrCacheMiss, err)
	}
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
		_ = stream.SubscribeTopic(w, "topic1", "first1")
		assertReceivedEvents(t, w, events1...)
	})
	t.Run("with topic2", func(t *testing.T) {
		w := httptest.NewRecorder()
		_ = stream.SubscribeTopic(w, "topic2", "first2")
		assertReceivedEvents(t, w, events2...)
	})
}
