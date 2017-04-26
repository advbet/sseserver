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
	go broker.run(map[string]interface{}{"": 123})

	// Before any publishings last ID should be the same as given when
	// starting broker goroutine
	assert.Equal(t, 123, broker.subscribe("", make(chan *Event)))

	broker.publish("", &Event{ID: 1}, nil)
	assert.Equal(t, 1, broker.subscribe("", make(chan *Event)))

	broker.publish("", &Event{ID: 2}, nil)
	assert.Equal(t, 2, broker.subscribe("", make(chan *Event)))
}

// TestBorkerLastIDTopics checks if using multiple topics does track event IDs
// independently.
func TestBrokerLastIDTopics(t *testing.T) {
	broker := newBroker()
	defer close(broker)
	go broker.run(map[string]interface{}{
		"topic1": 123,
		"topic2": 456,
	})

	// Before any publishings last ID should be the same as given when
	// starting broker goroutine
	assert.Equal(t, 123, broker.subscribe("topic1", make(chan *Event)))
	assert.Equal(t, 456, broker.subscribe("topic2", make(chan *Event)))

	// Publish on topic1
	broker.publish("topic1", &Event{ID: 1}, nil)
	assert.Equal(t, 1, broker.subscribe("topic1", make(chan *Event)))
	assert.Equal(t, 456, broker.subscribe("topic2", make(chan *Event)))

	// Publish on topic2
	broker.publish("topic2", &Event{ID: 2}, nil)
	assert.Equal(t, 1, broker.subscribe("topic1", make(chan *Event)))
	assert.Equal(t, 2, broker.subscribe("topic2", make(chan *Event)))
}

// TestBrokerStop cheks if closing broker closes all subscribers.
func TestBrokerStop(t *testing.T) {
	broker := newBroker()
	go broker.run(nil)

	client := make(chan *Event, 10)

	broker.subscribe("", client)
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
		broker.subscribe("", client)
	}

	// emit single event
	event := &Event{ID: 15, Event: "test", Data: "ok"}
	broker.publish("", event, nil)

	// check if all clients received it
	for _, client := range clients {
		e, ok := <-client
		assert.True(t, ok)
		assert.Equal(t, event, e)
	}
}

// TestBrokerPublishTopics cheks if published event is broadcasted to all of the
// subscribers and broadcasts does not mix between topics.
func TestBrokerPublishTopics(t *testing.T) {
	broker := newBroker()
	defer close(broker)
	go broker.run(nil)

	// attach three clients to the broker
	clients1 := make([]chan *Event, 3)
	clients2 := make([]chan *Event, 3)
	for i := 0; i < 3; i++ {
		clients1[i] = make(chan *Event, 10)
		broker.subscribe("topic1", clients1[i])

		clients2[i] = make(chan *Event, 10)
		broker.subscribe("topic2", clients2[i])
	}

	// emit single event
	event1 := &Event{ID: 15, Event: "test1", Data: "ok1"}
	broker.publish("topic1", event1, nil)

	event2 := &Event{ID: 17, Event: "test2", Data: "ok2"}
	broker.publish("topic2", event2, nil)

	// check if all clients received it
	for _, client := range clients1 {
		e, ok := <-client
		assert.True(t, ok)
		assert.Equal(t, event1, e)
	}
	for _, client := range clients2 {
		e, ok := <-client
		assert.True(t, ok)
		assert.Equal(t, event2, e)
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
	broker.subscribe("", client)

	// Emit 6 events to overflow the client, naive broker implementation
	// would block when sending fourth event
	for i := 0; i < cap(client)*2; i++ {
		broker.publish("", &Event{ID: i}, nil)
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
	broker.subscribe("", client)
	broker.publish("", &Event{ID: 10}, nil)
	broker.unsubscribe(client)
	<-client

	// client should not receive events published after client is
	// disconnected
	broker.publish("", &Event{ID: 20}, nil)

	// assert client channel is closed and have not received the second
	// event
	_, ok := <-client
	assert.False(t, ok)
}

func TestBorkerBroadcast(t *testing.T) {
	broker := newBroker()
	defer close(broker)
	go broker.run(nil)

	// attach three clients to the broker
	clients1 := make([]chan *Event, 3)
	clients2 := make([]chan *Event, 3)
	for i := 0; i < 3; i++ {
		clients1[i] = make(chan *Event, 10)
		broker.subscribe("topic1", clients1[i])

		clients2[i] = make(chan *Event, 10)
		broker.subscribe("topic2", clients2[i])
	}

	// emit single event
	event := &Event{ID: 15, Event: "test1", Data: "ok1"}
	broker.broadcast(event)

	// check if all clients received it
	for _, client := range clients1 {
		e, ok := <-client
		assert.True(t, ok)
		assert.Equal(t, event, e)
	}
	for _, client := range clients2 {
		e, ok := <-client
		assert.True(t, ok)
		assert.Equal(t, event, e)
	}
}
