package sseserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBorkerLastID checks if subscribing to broker correctly returns last
// published event ID.
func TestBrokerLastID(t *testing.T) {
	broker := newBroker()
	defer close(broker)
	go broker.run(99)

	// Before any publishings last ID should be the same as given when
	// starting broker goroutine
	assert.Equal(t, 99, broker.subscribe(make(chan *Event)))

	broker.publish(&Event{ID: 2})
	assert.Equal(t, 2, broker.subscribe(make(chan *Event)))

	broker.publish(&Event{ID: 4})
	assert.Equal(t, 4, broker.subscribe(make(chan *Event)))
}

// TestBrokerStop cheks if closing broker closes all subscribers.
func TestBrokerStop(t *testing.T) {
	broker := newBroker()
	go broker.run(nil)

	client := make(chan *Event, 10)

	broker.subscribe(client)
	close(broker)

	// reading from closed channel should fail
	_, ok := <-client
	assert.False(t, ok)
}

// TestBrokerPublish cheks if published event is broadcasted to all of the
// subscribers.
func TestBrokerPublish(t *testing.T) {
	broker := newBroker()
	defer close(broker)
	go broker.run(nil)

	// attach three clients to the broker
	clients := []chan *Event{
		make(chan *Event, 10),
		make(chan *Event, 10),
		make(chan *Event, 10),
	}
	for _, client := range clients {
		broker.subscribe(client)
	}

	// emit single event
	event := &Event{ID: 15, Event: "test", Data: "ok"}
	broker.publish(event)

	// check if all clients received it
	for _, client := range clients {
		e, ok := <-client
		assert.True(t, ok)
		assert.Equal(t, event, e)
	}
}

// TestBrokerPublishFull cheks if subscribers with full channels are
// does not block publishing and are automatically disconnected.
func TestBrokerPublishFull(t *testing.T) {
	broker := newBroker()
	defer close(broker)
	go broker.run(nil)

	// Create new client with 3 events buffer size
	client := make(chan *Event, 3)
	broker.subscribe(client)

	// Emit 6 events to overflow the client, naive broker implementation
	// would block when sending fourth event
	for i := 0; i < cap(client)*2; i++ {
		broker.publish(&Event{ID: i})
	}

	// Drain event from the client buffer, check if events are received in
	// oder
	for i := 0; i < cap(client); i++ {
		e, ok := <-client
		assert.True(t, ok)
		assert.Equal(t, &Event{ID: i}, e)
	}

	// Assert that broker have not buffered any overflowed events and closed
	// client channel
	_, ok := <-client
	assert.False(t, ok)
}

// TestBrokerUnsubscribe checks if unsubscribing from broker does not receive
// further events and closes client channel.
func TestBrokerUnsubscribe(t *testing.T) {
	broker := newBroker()
	defer close(broker)
	go broker.run(nil)

	client := make(chan *Event, 10)
	broker.subscribe(client)
	broker.publish(&Event{ID: 10})
	broker.unsubscribe(client)
	<-client

	// client should not receive events published after client is
	// disconnected
	broker.publish(&Event{ID: 20})

	// assert client channel is closed and have not received the second
	// event
	_, ok := <-client
	assert.False(t, ok)
}
