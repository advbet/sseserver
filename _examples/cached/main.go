package main

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/advbet/sseserver/v2"
)

func eventSource(stream sseserver.Stream) {
	for i := 0; true; i++ {
		stream.Publish(&sseserver.Event{
			ID:    strconv.Itoa(i),
			Event: "counter",
			Data: map[string]interface{}{
				"msg": "ticks since start",
				"val": i,
			},
		})
		time.Sleep(time.Second)
	}
}

func main() {
	stream := sseserver.NewCached("", sseserver.DefaultConfig, 5*time.Minute, time.Minute)
	go eventSource(stream)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("Last-Event-ID")
		if err := stream.Subscribe(w, id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	fmt.Println(http.ListenAndServe(":8000", nil))

	// Test with:
	//   curl http://localhost:8000/
	//   curl -H "Last-Event-ID: 5" http://localhost:8000/
}

//func main() {
//	stream := sseserver.NewCachedCount("", sseserver.DefaultConfig, 5)
//	go eventSource(stream)
//
//	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
//		id := r.Header.Get("Last-Event-ID")
//		if err := stream.Subscribe(w, id); err != nil {
//			http.Error(w, err.Error(), http.StatusInternalServerError)
//		}
//	})
//
//	fmt.Println(http.ListenAndServe(":8000", nil))
//
//	// Test with:
//	//   curl http://localhost:8000/
//	//   curl -H "Last-Event-ID: 5" http://localhost:8000/
//}
