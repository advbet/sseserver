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
		ID: id,
		Data: map[string]interface{}{
			"msg":   "ticks since start",
			"topic": topic,
			"val":   id,
		},
	}
}

func eventGenerator(stream sseserver.Stream) {
	i := 0
	c := time.Tick(10 * time.Second)

	for range c {
		i++
		stream.Publish(newEvent("", strconv.Itoa(i)))
	}
}

func main() {
	stream := sseserver.NewLastOnly(sseserver.DefaultConfig)
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
