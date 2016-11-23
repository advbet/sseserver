package sseserver

// command is a message data type for controlling broker process.
type command struct {
	op       operation
	sink     chan<- *Event      // used for subscribe, unsubscribe
	response chan<- interface{} // used for subscribe
	event    *Event             // used for publish
}

type operation int

const (
	subscribe operation = iota
	unsubscribe
	publish
)

// brokerRun handles event broadcasting and manages subscription lists. Each
// started stream have this code running in a separate goroutine.
func brokerRun(cmds <-chan command, lastID interface{}) {
	sinks := make(map[chan<- *Event]struct{})

	for cmd := range cmds {
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
