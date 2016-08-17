package ssestream

import "sync"

type lastOnlyStream struct {
	stream
	sync.RWMutex
	last *Event
}

func NewLastOnly(cfg Config) Stream {
	s := &lastOnlyStream{
		stream: stream{
			cmd:          make(chan command),
			resync:       nil, // self-referencing structs are not supported
			cfg:          cfg,
			responseStop: make(chan struct{}),
		},
		last: nil,
	}
	s.stream.resync = s.resync
	s.wg.Add(1)
	go s.run(nil)

	return s
}

func (s *lastOnlyStream) resync(fromID, toID interface{}) (events []Event, ok bool) {
	s.RLock()
	defer s.RUnlock()

	if s.last == nil {
		return nil, true
	}
	return []Event{*s.last}, true
}

func (s *lastOnlyStream) Publish(event *Event) {
	s.Lock()
	s.last = event
	s.Unlock()

	s.stream.Publish(event)
}
