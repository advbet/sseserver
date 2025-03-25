package main

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/advbet/sseserver"
)

func newEvent(topic string, id string) *sseserver.Event {
	return &sseserver.Event{
		ID:    id,
		Event: "counter",
		Data: map[string]interface{}{
			"msg":   "ticks since start",
			"topic": topic,
			"val":   id,
		},
	}
}

func lookupEvents(topic string, fromStr string, toStr string) ([]sseserver.Event, error) {
	if fromStr == "" {
		// New client
		// no resync, continue sending live events
		return nil, nil
	}

	from, err := strconv.Atoi(fromStr)
	if err != nil {
		return nil, err
	}

	to, err := strconv.Atoi(toStr)
	if err != nil {
		return nil, err
	}

	if from >= to {
		// Client is up to date
		// no resync, continue sending live events
		return nil, nil
	}

	var events []sseserver.Event

	switch {
	case to-from > 10:
		// do not resync more than 10 events at a time
		for i := from + 1; i <= from+10; i++ {
			events = append(events, *newEvent(topic, strconv.Itoa(i)))
		}

		// send first 10 missing events
		return events, nil
	default:
		for i := from + 1; i <= to; i++ {
			events = append(events, *newEvent(topic, strconv.Itoa(i)))
		}
		// send missing events, continue sending live events
		return events, nil
	}
}

func eventGenerator(stream sseserver.Stream) {
	i := 0
	c := time.Tick(time.Second)

	for range c {
		i++
		stream.Publish(newEvent("", strconv.Itoa(i)))
	}
}

func main() {
	stream := sseserver.NewGeneric(lookupEvents, "0", sseserver.DefaultConfig)
	go eventGenerator(stream)

	requestHandler := func(w http.ResponseWriter, r *http.Request) {
		var err error
		if _, err = strconv.Atoi(r.Header.Get("Last-Event-ID")); err != nil {
			fmt.Println(err)
		}

		if err = stream.Subscribe(w, r.Header.Get("Last-Event-ID")); err != nil {
			fmt.Println(err)
		}
	}

	http.HandleFunc("/", requestHandler)
	fmt.Println(http.ListenAndServe(":8000", nil))

	// Test with:
	//   curl http://localhost:8000/
	//   curl -H "Last-Event-ID: 5" http://localhost:8000/
}
