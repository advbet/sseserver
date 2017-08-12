package sseserver

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	cache "github.com/patrickmn/go-cache"
)

type CachedStream struct {
	broker       brokerChan
	cfg          Config
	responseStop chan struct{}
	wg           sync.WaitGroup
	cache        *cache.Cache
}

// ErrCacheMiss is returned from cachedStream.Subscribe if resyncinc client is
// not possible because events are not found in a cache. This situation will
// usualy occur if client was disconnected for too long and the oldes events
// were evicted from the cache.
//
// This error is returned before writing anything to the response writer. It
// isresponsibiity of the caller of cachedStream.Subscribe to generate a
// response if this error is returned.
var ErrCacheMiss = errors.New("missing events in cache")

// NewCached creates a new SSE stream. All published events are cached for up to
// expiration time in local cache and clients are automatically resynced on
// reconnect.
//
// Passing nil or empty string as last event ID for Subscribe() would connect
// client without resync.
//
// Call to Subscribe() might return ErrCacheMiss if client requests to resync
// from an event not found in the cache. If ErrCacheMiss is returned user of
// this library is responsible for generating HTTP response to the client. It is
// recommended to return 204 no content response to stop client from
// reconnecting until he syncs event state manually.
func NewCached(lastID interface{}, cfg Config, expiration, cleanup time.Duration) *CachedStream {
	s := &CachedStream{
		broker:       newBroker(),
		cfg:          cfg,
		responseStop: make(chan struct{}),
		cache:        cache.New(expiration, cleanup),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.broker.run(map[string]interface{}{
			"": lastID,
		})
	}()

	return s
}

func (s *CachedStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

func (s *CachedStream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, func(lastID interface{}) {
		s.cache.Set(topicIDKey(topic, lastID), event, cache.DefaultExpiration)
	})
}

func (s *CachedStream) PublishBroadcast(event *Event) {
	// Cached SSE stream does not support tracking broadcasted events. This
	// removes ID value from all broadcasted events.
	event.ID = nil
	s.broker.broadcast(event)
}

func (s *CachedStream) Subscribe(w http.ResponseWriter, lastClientID interface{}) error {
	return s.SubscribeTopic(w, "", lastClientID)
}

func (s *CachedStream) SubscribeTopic(w http.ResponseWriter, topic string, lastClientID interface{}) error {
	source := make(chan *Event, s.cfg.QueueLength)
	lastServerID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	serverID := fmt.Sprintf("%v", lastServerID)
	clientID := fmt.Sprintf("%v", lastClientID)
	if lastClientID == nil || clientID == "" || clientID == serverID {
		// no resync needed
		return Respond(w, source, &s.cfg, s.responseStop)
	}

	var events []Event
	for {
		eventIface, ok := s.cache.Get(topicIDKey(topic, clientID))
		if !ok {
			return ErrCacheMiss
		}

		event := eventIface.(*Event)
		events = append(events, *event)

		clientID = fmt.Sprintf("%v", event.ID)
		if serverID == clientID {
			break
		}
	}
	return Respond(w, prependStream(events, source), &s.cfg, s.responseStop)
}

func (s *CachedStream) DropSubscribers() {
	close(s.responseStop)
}

func (s *CachedStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}

func topicIDKey(topic string, id interface{}) string {
	strID := fmt.Sprintf("%v", id)
	return fmt.Sprintf("%d:%s%d:%s", len(topic), topic, len(strID), strID)
}
