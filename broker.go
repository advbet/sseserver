package sseserver

type operation int

// command is a message data type for controlling broker process.
type command struct {
	op       operation
	sink     chan<- *Event      // used for subscribe, unsubscribe
	response chan<- interface{} // used for subscribe
	event    *Event             // used for publish
}

// brokerChan is an implementation of single pub-sub communications channel.
type brokerChan chan command

const (
	subscribe operation = iota
	unsubscribe
	publish
)

// newBroker creates a new instance of broker. It needs to be started with run
// method before publishing or subscribing.
func newBroker() brokerChan {
	return make(chan command)
}

// brokerPublish broadcasts given event via broker to all of the subscribers.
func (b brokerChan) publish(event *Event) {
	b <- command{
		op:    publish,
		event: event,
	}
}

// brokerSubscribe adds subscribes given channel to receive all events published
// to this broker.
func (b brokerChan) subscribe(events chan<- *Event) interface{} {
	response := make(chan interface{}, 1)
	b <- command{
		op:       subscribe,
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

// brokerRun handles event broadcasting and manages subscription lists. Each
// started stream have this code running in a separate goroutine.
func (b brokerChan) run(lastID interface{}) {
	sinks := make(map[chan<- *Event]struct{})

	for cmd := range b {
		switch cmd.op {
		case subscribe:
			sinks[cmd.sink] = struct{}{}

			// return last seen event ID to the subscriber
			cmd.response <- lastID
			close(cmd.response)
		case unsubscribe:
			if _, ok := sinks[cmd.sink]; ok {
				close(cmd.sink)
				delete(sinks, cmd.sink)
			}
		case publish:
			lastID = cmd.event.ID
			for ch := range sinks {
				select {
				case ch <- cmd.event:
					// Success
				default:
					// Client is too slow, close stream and
					// wait for client reconnect
					close(ch)
					delete(sinks, ch)
				}
			}
		}
	}

	for ch := range sinks {
		close(ch)
	}
}
