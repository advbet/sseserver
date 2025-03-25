package sseserver

import (
	"strconv"
	"testing"
)

// TestBorkerLastID checks if subscribing to broker correctly returns last
// published event ID.
func TestBrokerLastID(t *testing.T) {
	broker := newBroker()
	defer close(broker)
	go broker.run(map[string]string{"": "123"})

	// Before any publishing's last ID should be the same as given when
	// starting broker goroutine
	lastID := broker.subscribe("", make(chan *Event))
	if lastID != "123" {
		t.Errorf("expected lastID to be '123', got '%s'", lastID)
	}

	broker.publish("", &Event{ID: "1"}, nil)
	lastID = broker.subscribe("", make(chan *Event))
	if lastID != "1" {
		t.Errorf("expected lastID to be '1', got '%s'", lastID)
	}

	broker.publish("", &Event{ID: "2"}, nil)
	lastID = broker.subscribe("", make(chan *Event))
	if lastID != "2" {
		t.Errorf("expected lastID to be '2', got '%s'", lastID)
	}
}

// TestBorkerLastIDTopics checks if using multiple topics does track event IDs
// independently.
func TestBrokerLastIDTopics(t *testing.T) {
	broker := newBroker()
	defer close(broker)
	go broker.run(map[string]string{
		"topic1": "123",
		"topic2": "456",
	})

	// Before any publishing's last ID should be the same as given when
	// starting broker goroutine
	lastID := broker.subscribe("topic1", make(chan *Event))
	if lastID != "123" {
		t.Errorf("expected lastID for topic1 to be '123', got '%s'", lastID)
	}

	lastID = broker.subscribe("topic2", make(chan *Event))
	if lastID != "456" {
		t.Errorf("expected lastID for topic2 to be '456', got '%s'", lastID)
	}

	// Publish on topic1
	broker.publish("topic1", &Event{ID: "1"}, nil)
	lastID = broker.subscribe("topic1", make(chan *Event))
	if lastID != "1" {
		t.Errorf("expected lastID for topic1 to be '1', got '%s'", lastID)
	}

	lastID = broker.subscribe("topic2", make(chan *Event))
	if lastID != "456" {
		t.Errorf("expected lastID for topic2 to be '456', got '%s'", lastID)
	}

	// Publish on topic2
	broker.publish("topic2", &Event{ID: "2"}, nil)
	lastID = broker.subscribe("topic1", make(chan *Event))
	if lastID != "1" {
		t.Errorf("expected lastID for topic1 to be '1', got '%s'", lastID)
	}

	lastID = broker.subscribe("topic2", make(chan *Event))
	if lastID != "2" {
		t.Errorf("expected lastID for topic2 to be '2', got '%s'", lastID)
	}
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
	if ok {
		t.Error("expected client channel to be closed, but it was still open")
	}
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
	event := &Event{ID: "15", Event: "test", Data: "ok"}
	broker.publish("", event, nil)

	// check if all clients received it
	for i, client := range clients {
		e, ok := <-client
		if !ok {
			t.Errorf("client %d: channel unexpectedly closed", i)
			continue
		}
		if e.ID != event.ID || e.Event != event.Event || e.Data != event.Data {
			t.Errorf("client %d: expected event %+v, got %+v", i, event, e)
		}
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
	event1 := &Event{ID: "15", Event: "test1", Data: "ok1"}
	broker.publish("topic1", event1, nil)

	event2 := &Event{ID: "17", Event: "test2", Data: "ok2"}
	broker.publish("topic2", event2, nil)

	// check if all clients received it
	for i, client := range clients1 {
		e, ok := <-client
		if !ok {
			t.Errorf("topic1 client %d: channel unexpectedly closed", i)
			continue
		}
		if e.ID != event1.ID || e.Event != event1.Event || e.Data != event1.Data {
			t.Errorf("topic1 client %d: expected event %+v, got %+v", i, event1, e)
		}
	}

	for i, client := range clients2 {
		e, ok := <-client
		if !ok {
			t.Errorf("topic2 client %d: channel unexpectedly closed", i)
			continue
		}
		if e.ID != event2.ID || e.Event != event2.Event || e.Data != event2.Data {
			t.Errorf("topic2 client %d: expected event %+v, got %+v", i, event2, e)
		}
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
		broker.publish("", &Event{ID: strconv.Itoa(i)}, nil)
	}

	// Drain event from the client buffer, check if events are received in
	// order
	for i := 0; i < cap(client); i++ {
		e, ok := <-client
		if !ok {
			t.Fatalf("client channel closed prematurely at iteration %d", i)
		}
		expectedID := strconv.Itoa(i)
		if e.ID != expectedID {
			t.Errorf("expected event ID %s, got %s", expectedID, e.ID)
		}
	}

	// Assert that broker have not buffered any overflowed events and closed
	// client channel
	_, ok := <-client
	if ok {
		t.Error("expected client channel to be closed, but it was still open")
	}
}

// TestBrokerUnsubscribe checks if unsubscribing from broker does not receive
// further events and closes client channel.
func TestBrokerUnsubscribe(t *testing.T) {
	broker := newBroker()
	defer close(broker)
	go broker.run(nil)

	client := make(chan *Event, 10)
	broker.subscribe("", client)
	broker.publish("", &Event{ID: "10"}, nil)
	broker.unsubscribe(client)
	<-client

	// client should not receive events published after client is
	// disconnected
	broker.publish("", &Event{ID: "20"}, nil)

	// assert client channel is closed and have not received the second
	// event
	_, ok := <-client
	if ok {
		t.Error("expected client channel to be closed, but it was still open")
	}
}

func TestBrokerBroadcast(t *testing.T) {
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
	event := &Event{ID: "15", Event: "test1", Data: "ok1"}
	broker.broadcast(event)

	// check if all clients received it
	for i, client := range clients1 {
		e, ok := <-client
		if !ok {
			t.Errorf("topic1 client %d: channel unexpectedly closed", i)
			continue
		}
		if e.ID != event.ID || e.Event != event.Event || e.Data != event.Data {
			t.Errorf("topic1 client %d: expected event %+v, got %+v", i, event, e)
		}
	}

	for i, client := range clients2 {
		e, ok := <-client
		if !ok {
			t.Errorf("topic2 client %d: channel unexpectedly closed", i)
			continue
		}
		if e.ID != event.ID || e.Event != event.Event || e.Data != event.Data {
			t.Errorf("topic2 client %d: expected event %+v, got %+v", i, event, e)
		}
	}
}
