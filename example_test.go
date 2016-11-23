package sseserver_test

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"bitbucket.org/advbet/sseserver"
)

func newEvent(id int) *sseserver.Event {
	return &sseserver.Event{
		ID:    id,
		Event: "counter",
		Data: map[string]interface{}{
			"msg": "ticks since start",
			"val": id,
		},
	}
}

func lookupEvents(fromI interface{}, toI interface{}) ([]sseserver.Event, bool) {
	if fromI == nil {
		// New client
		// no resync, continue sending live events
		return nil, true
	}

	from := fromI.(int)
	to := toI.(int)

	if from >= to {
		// Client is up to date
		// no resync, continue sending live events
		return nil, true
	}

	events := []sseserver.Event{}
	if to-from > 10 {
		// do not resync more than 10 events at a time
		for i := from + 1; i <= from+10; i++ {
			events = append(events, *newEvent(i))
		}
		// send first 10 missing events, disconnect client to come back
		// for more
		return events, false
	} else {
		for i := from + 1; i <= to; i++ {
			events = append(events, *newEvent(i))
		}
		// send missing events, continue sending live events
		return events, true
	}
}

func eventGenerator(stream sseserver.Stream) {
	i := 0
	c := time.Tick(time.Second)

	for range c {
		i++
		stream.Publish(newEvent(i))
	}
}

func Example_generic() {
	stream := sseserver.NewGeneric(lookupEvents, 0, sseserver.DefaultConfig)
	go eventGenerator(stream)

	requestHandler := func(w http.ResponseWriter, r *http.Request) {
		var id interface{}
		var err error

		if id, err = strconv.Atoi(r.Header.Get("Last-Event-ID")); err != nil {
			fmt.Println(err)
			id = nil
		}
		err = stream.Subscribe(w, id)
		if err != nil {
			fmt.Println(err)
		}
	}

	http.HandleFunc("/", requestHandler)
	fmt.Println(http.ListenAndServe(":8000", nil))

	// Test with:
	//   curl http://localhost:8000/
	//   curl -H "Last-Event-ID: 5" http://localhost:8000/
}
