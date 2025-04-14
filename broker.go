package sseserver

type operation int

// command is a message data type for controlling broker process.
type command struct {
	op    operation
	topic string

	sink     chan<- *Event // used for subscribe, unsubscribe
	response chan<- string // used for subscribe

	event      *Event       // used for publish
	prePublish func(string) // used for publish
}

// brokerChan is an implementation of single pub-sub communications channel.
type brokerChan chan command

const (
	subscribe operation = iota
	unsubscribe
	publish
	broadcast
)

// newBroker creates a new instance of broker. It needs to be started with run
// method before publishing or subscribing.
func newBroker() brokerChan {
	return make(chan command)
}

// publish broadcasts given event via broker to all of the subscribers.
//
// prePublish will be called by broker before publishing event with last event
// ID as an argument.
func (b brokerChan) publish(topic string, event *Event, prePublish func(string)) {
	b <- command{
		op:         publish,
		topic:      topic,
		event:      event,
		prePublish: prePublish,
	}
}

// broadcast will send given event to all active subscribers. Last event ID will
// only be updated if event ID field is not set to nil.
func (b brokerChan) broadcast(event *Event) {
	b <- command{
		op:    broadcast,
		event: event,
	}
}

// subscribe adds subscribes given channel to receive all events published to
// this broker.
func (b brokerChan) subscribe(topic string, events chan<- *Event) string {
	response := make(chan string, 1)
	b <- command{
		op:       subscribe,
		topic:    topic,
		sink:     events,
		response: response,
	}
	// brokerRun goroutine will write current last seen event ID to this
	// channel exactly once and close it
	return <-response
}

// unsubscribe is a helper function for removing a subscription, safe for
// concurrent access.
func (b brokerChan) unsubscribe(ch chan<- *Event) {
	b <- command{
		op:   unsubscribe,
		sink: ch,
	}
}

// run handles event broadcasting and manages subscription lists. Each started
// stream have this code running in a separate goroutine.
func (b brokerChan) run(lastIDs map[string]string) {
	sinks := make(map[chan<- *Event]string)

	if lastIDs == nil {
		lastIDs = make(map[string]string)
	}

	emit := func(ch chan<- *Event, event *Event) {
		select {
		case ch <- event:
			// Success
		default:
			// Client is too slow, close stream and
			// wait for client reconnect
			close(ch)
			delete(sinks, ch)
		}
	}

	for cmd := range b {
		switch cmd.op {
		case subscribe:
			sinks[cmd.sink] = cmd.topic

			// return last seen event ID to the subscriber
			cmd.response <- lastIDs[cmd.topic]
			close(cmd.response)
		case unsubscribe:
			if _, ok := sinks[cmd.sink]; ok {
				close(cmd.sink)
				delete(sinks, cmd.sink)
			}
		case broadcast:
			for ch, topic := range sinks {
				if cmd.event.ID != "" {
					lastIDs[topic] = cmd.event.ID
				}

				emit(ch, cmd.event)
			}
		case publish:
			if cmd.prePublish != nil {
				cmd.prePublish(lastIDs[cmd.topic])
			}

			if cmd.event.ID != "" {
				lastIDs[cmd.topic] = cmd.event.ID
			}

			for ch, topic := range sinks {
				if topic != cmd.topic {
					continue
				}

				emit(ch, cmd.event)
			}
		}
	}

	for ch := range sinks {
		close(ch)
	}
}
