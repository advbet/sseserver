package sseserver

import (
	"net/http"
	"sync"
)

type CachedCountStream struct {
	broker       brokerChan
	cfg          Config
	responseStop chan struct{}
	wg           sync.WaitGroup
	maxKeysCount int
	lastKeys     []string
	ctr          int
	events       map[string]*Event
	mu           sync.RWMutex
}

// NewCachedCount creates a new SSE stream. All published events are cached.
// The number of cached events can be set by passing the desired size argument.
// Clients are automatically resynced on reconnect.
//
// Passing empty string as last event ID for Subscribe() would connect client
// without resync.
//
// Call to Subscribe() might return ErrCacheMiss if client requests to resync
// from an event not found in the cache. If ErrCacheMiss is returned user of
// this library is responsible for generating HTTP response to the client. It is
// recommended to return 204 no content response to stop client from
// reconnecting until he syncs event state manually.
func NewCachedCount(lastID string, cfg Config, size int) *CachedCountStream {
	return NewCachedCountMultiStream(map[string]string{"": lastID}, cfg, size)
}

// NewCachedCountMultiStream is similar to NewCachedCount but allows setting initial last
// event ID values for multiple topics.
func NewCachedCountMultiStream(lastIDs map[string]string, cfg Config, size int) *CachedCountStream {
	s := &CachedCountStream{
		broker:       newBroker(),
		cfg:          cfg,
		responseStop: make(chan struct{}),
		maxKeysCount: size,
		lastKeys:     make([]string, size),
		ctr:          0,
		events:       make(map[string]*Event),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.broker.run(lastIDs)
	}()

	return s
}

func (s *CachedCountStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

func (s *CachedCountStream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, func(lastID string) {
		s.mu.Lock()
		s.ctr = (s.ctr + 1) % s.maxKeysCount
		delete(s.events, s.lastKeys[s.ctr])
		key := topicIDKey(topic, lastID)
		s.events[key] = event
		s.lastKeys[s.ctr] = key
		s.mu.Unlock()
	})
}

func (s *CachedCountStream) PublishBroadcast(event *Event) {
	// Cached SSE stream does not support tracking broadcasted events. This
	// removes ID value from all broadcasted events.
	event.ID = ""
	s.broker.broadcast(event)
}

func (s *CachedCountStream) Subscribe(w http.ResponseWriter, lastClientID string) error {
	return s.SubscribeTopicFiltered(w, "", lastClientID, nil)
}

func (s *CachedCountStream) SubscribeFiltered(w http.ResponseWriter, lastClientID string, f FilterFn) error {
	return s.SubscribeTopicFiltered(w, "", lastClientID, f)
}

func (s *CachedCountStream) SubscribeTopic(w http.ResponseWriter, topic string, lastClientID string) error {
	return s.SubscribeTopicFiltered(w, topic, lastClientID, nil)
}

func (s *CachedCountStream) SubscribeTopicFiltered(w http.ResponseWriter, topic string, lastClientID string, f FilterFn) error {
	source := make(chan *Event, s.cfg.QueueLength)
	lastServerID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	if lastClientID == "" || lastClientID == lastServerID {
		// no resync needed
		return Respond(w, applyChanFilter(source, f), &s.cfg, s.responseStop)
	}

	var events []Event
	s.mu.RLock()
	for {
		event, ok := s.events[topicIDKey(topic, lastClientID)]
		if !ok {
			s.mu.RUnlock()
			return ErrCacheMiss
		}

		events = append(events, *event)
		lastClientID = event.ID
		if lastServerID == lastClientID {
			break
		}
	}
	s.mu.RUnlock()
	return Respond(w, applyChanFilter(prependStream(events, source), f), &s.cfg, s.responseStop)
}

func (s *CachedCountStream) DropSubscribers() {
	close(s.responseStop)
}

func (s *CachedCountStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}
