package sseserver

import (
	"net/http"
	"sync"
)

type lastOnlyStream struct {
	broker       brokerChan
	cfg          Config
	responseStop chan struct{}

	wg sync.WaitGroup

	sync.RWMutex
	last *Event
}

// NewLastOnly creates a new sse stream that resends only last seen event to all
// newly connected clients. If client alredy have seen the lates event is is not
// repeated.
func NewLastOnly(cfg Config) Stream {
	s := &lastOnlyStream{
		broker:       newBroker(),
		cfg:          cfg,
		responseStop: make(chan struct{}),
		last:         nil,
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.broker.run(nil)
	}()

	return s
}

func (s *lastOnlyStream) Publish(event *Event) {
	s.Lock()
	s.last = event
	s.Unlock()

	s.broker.publish(event)
}

func (s *lastOnlyStream) Subscribe(w http.ResponseWriter, lastEventID interface{}) error {
	source := make(chan *Event, s.cfg.QueueLength)
	s.RLock()
	if s.last != nil && s.last.ID != lastEventID {
		source <- s.last
	}
	s.RUnlock()

	s.broker.subscribe(source)
	defer s.broker.unsubscribe(source)

	return Respond(w, source, &s.cfg, s.responseStop)
}

func (s *lastOnlyStream) DropSubscribers() {
	close(s.responseStop)
}

func (s *lastOnlyStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}
