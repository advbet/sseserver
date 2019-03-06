package sseserver

import (
	"net/http"
	"sync"
)

// GenericStream is the most generic SSE stream implementation where resync
// logic is supplied by the user of this package.
type GenericStream struct {
	broker       brokerChan
	resync       ResyncFn
	cfg          Config
	responseStop chan struct{}

	wg sync.WaitGroup
}

// NewGeneric creates a new instance of SSE stream. Creating new stream requires
// to provide a resync function with ResyncFn signature. It is used to
// generate a list of events that client might have missed during a reconnect.
// Argument lastID is used set last event ID that was published before
// application was started, this value is passed to the resync function and
// later replaced by the events published with stream.Publish method.
func NewGeneric(resync ResyncFn, lastID interface{}, cfg Config) *GenericStream {
	s := &GenericStream{
		broker:       newBroker(),
		resync:       resync,
		cfg:          cfg,
		responseStop: make(chan struct{}),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.broker.run(map[string]interface{}{"": lastID})
	}()
	return s
}

func (s *GenericStream) Publish(event *Event) {
	s.PublishTopic("", event)
}

func (s *GenericStream) PublishTopic(topic string, event *Event) {
	s.broker.publish(topic, event, nil)
}

func (s *GenericStream) PublishBroadcast(event *Event) {
	event.ID = nil
	s.broker.broadcast(event)
}

func (s *GenericStream) Subscribe(w http.ResponseWriter, lastEventID interface{}) error {
	return s.SubscribeTopicFiltered(w, "", lastEventID, nil)
}

func (s *GenericStream) SubscribeFiltered(w http.ResponseWriter, lastEventID interface{}, f FilterFn) error {
	return s.SubscribeTopicFiltered(w, "", lastEventID, f)
}

func (s *GenericStream) SubscribeTopic(w http.ResponseWriter, topic string, lastEventID interface{}) error {
	return s.SubscribeTopicFiltered(w, topic, lastEventID, nil)
}

func (s *GenericStream) SubscribeTopicFiltered(w http.ResponseWriter, topic string, lastEventID interface{}, f FilterFn) error {
	source := make(chan *Event, s.cfg.QueueLength)
	toID := s.broker.subscribe(topic, source)
	defer s.broker.unsubscribe(source)

	events := make([]Event, 0)
	// lastEventID will be nil if client connects for the first time
	// serverID will be nil if server did not send any events yet
	for len(events) <= s.cfg.ResyncEventsThreshold {
		list, err := s.resync(topic, lastEventID, toID)
		if err != nil {
			if len(events) > 0 {
				return Respond(w, prependStream(events, nil), &s.cfg, s.responseStop)
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return nil
		}
		if len(list) == 0 {
			return Respond(w, prependStream(events, source), &s.cfg, s.responseStop)
		}
		switch f {
		case nil:
			events = append(events, list...)
		default:
			events = append(events, applySliceFilter(list, f)...)
		}
		lastEventID = list[len(list)-1].ID
	}
	return Respond(w, prependStream(events, nil), &s.cfg, s.responseStop)
}

func (s *GenericStream) DropSubscribers() {
	close(s.responseStop)
}

func (s *GenericStream) Stop() {
	close(s.broker)
	s.wg.Wait()
}

// prependStream takes slice and channel of events and and produces new channel
// that will contain all events in the slice followed by the events in source
// channel. If source channel is nil it will be ignored an only events in the
// slice will be used.
func prependStream(events []Event, source <-chan *Event) <-chan *Event {
	sink := make(chan *Event)
	go func() {
		defer close(sink)
		// Stream static events
		for i := range events {
			sink <- &events[i]
		}
		// Exit if source stream is missing, this allows to reuse this
		// function for generating stream from slice only
		if source == nil {
			return
		}
		// Restream source channel
		for event := range source {
			sink <- event
		}
	}()
	return sink
}
