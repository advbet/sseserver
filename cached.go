package sseserver

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	cache "github.com/patrickmn/go-cache"
)

type cachedStream struct {
	broker       brokerChan
	lastID       interface{}
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
// Passing nil as last event ID for Subscribe would connect client without
// resync. Call to subscribe might return ErrCacheMiss, in this case user of
// this library is responsible for generating HTTP response to the client. It is
// recommended to return 204 no content response to stop client from
// reconnecting until he syncs event state manually.
func NewCached(lastID interface{}, cfg Config, expiration, cleanup time.Duration) Stream {
	s := &cachedStream{
		broker:       newBroker(),
		lastID:       lastID,
		cfg:          cfg,
		responseStop: make(chan struct{}),
		cache:        cache.New(expiration, cleanup),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.broker.run(nil)
	}()

	return s
}

func (s *cachedStream) Publish(event *Event) {
	s.cache.Set(fmt.Sprintf("%v", s.lastID), event, cache.DefaultExpiration)
	s.lastID = event.ID
	s.broker.publish(event)
}

func (s *cachedStream) Subscribe(w http.ResponseWriter, lastClientID interface{}) error {
	source := make(chan *Event, s.cfg.QueueLength)
	lastServerID := s.broker.subscribe(source)
	defer s.broker.unsubscribe(source)

	// if lastClientID is nil attach client to the stream without resync
	if lastClientID == nil {
		return Respond(w, source, &s.cfg, s.responseStop)
	}

	events := make([]Event, 0)
	serverID := fmt.Sprintf("%v", lastServerID)
	for {
		clientID := fmt.Sprintf("%v", lastClientID)
		if serverID == clientID {
			break
		}

		eventIface, ok := s.cache.Get(clientID)
		if !ok {
			return ErrCacheMiss
		}

		event := eventIface.(*Event)
		events = append(events, *event)
		lastClientID = event.ID
	}
	return Respond(w, prependStream(events, source), &s.cfg, s.responseStop)
}

func (s *cachedStream) DropSubscribers() {
	close(s.responseStop)
}

func (s *cachedStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}
