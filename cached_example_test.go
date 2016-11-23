package sseserver_test

import (
	"fmt"
	"net/http"
	"time"

	"bitbucket.org/advbet/sseserver"
)

func eventSource(stream sseserver.Stream) {
	for i := 0; true; i++ {
		stream.Publish(&sseserver.Event{
			ID:    i,
			Event: "counter",
			Data: map[string]interface{}{
				"msg": "ticks since start",
				"val": i,
			},
		})
		time.Sleep(time.Second)
	}
}

func Example_cached() {
	stream := sseserver.NewCached(nil, sseserver.DefaultConfig, 5*time.Minute, time.Minute)
	go eventSource(stream)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var id interface{}
		lastID := r.Header.Get("Last-Event-ID")
		if lastID != "" {
			id = lastID
		}
		if err := stream.Subscribe(w, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	fmt.Println(http.ListenAndServe(":8000", nil))

	// Test with:
	//   curl http://localhost:8000/
	//   curl -H "Last-Event-ID: 5" http://localhost:8000/
}
