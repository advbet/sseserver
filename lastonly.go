package sseserver

import (
	"net/http"
	"sync"
)

type LastOnlyStream struct {
	broker       brokerChan
	cfg          Config
	responseStop chan struct{}

	wg sync.WaitGroup

	sync.RWMutex
	last map[string]*Event
}

// NewLastOnly creates a new sse stream that resends only last seen event to all
// newly connected clients. If client alredy have seen the lates event is is not
// repeated.
func NewLastOnly(cfg Config) *LastOnlyStream {
	s := &LastOnlyStream{
		broker:       newBroker(),
		cfg:          cfg,
		responseStop: make(chan struct{}),
		last:         make(map[string]*Event),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.broker.run(nil)
	}()

	return s
}

func (s *LastOnlyStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

func (s *LastOnlyStream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, func(lastID interface{}) {
		s.Lock()
		defer s.Unlock()
		s.last[topic] = event
	})
}

// PublishBroadcast for LastOnlyStream does not cache a broadcasted event
// and thus does not permit sending an event with ID value.
func (s *LastOnlyStream) PublishBroadcast(event *Event) {
	// LastOnly SSE stream does not support tracking broadcasted events. This
	// removes ID value from all broadcasted events.
	event.ID = nil
	s.broker.broadcast(event)
}

func (s *LastOnlyStream) Subscribe(w http.ResponseWriter, lastEventID interface{}) error {
	return s.SubscribeTopicFiltered(w, "", lastEventID, nil)
}

func (s *LastOnlyStream) SubscribeFiltered(w http.ResponseWriter, lastEventID interface{}, f FilterFn) error {
	return s.SubscribeTopicFiltered(w, "", lastEventID, f)
}

func (s *LastOnlyStream) SubscribeTopic(w http.ResponseWriter, topic string, lastEventID interface{}) error {
	return s.SubscribeTopicFiltered(w, topic, lastEventID, nil)
}

func (s *LastOnlyStream) SubscribeTopicFiltered(w http.ResponseWriter, topic string, lastEventID interface{}, f FilterFn) error {
	source := make(chan *Event, s.cfg.QueueLength)
	s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	s.RLock()
	last := s.last[topic]
	s.RUnlock()

	if last != nil && last.ID != lastEventID {
		return Respond(w, applyFilter(prependStream([]Event{*last}, source), f), &s.cfg, s.responseStop)
	}

	return Respond(w, applyFilter(source, f), &s.cfg, s.responseStop)
}

func (s *LastOnlyStream) DropSubscribers() {
	close(s.responseStop)
}

func (s *LastOnlyStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}
