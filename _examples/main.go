package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/advbet/sseserver/v2"
)

var (
	cachedStream      *sseserver.CachedStream
	cachedCountStream *sseserver.CachedCountStream
	genericStream     *sseserver.GenericStream
	lastOnlyStream    *sseserver.LastOnlyStream
)

const (
	cachedTopic      = "cached"
	cachedCountTopic = "cached-count"
	genericTopic     = "generic"
	lastOnlyTopic    = "last-only"
)

func main() {
	if err := run(); err != nil {
		slog.Error("running application", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cachedStream = sseserver.NewCached(sseserver.DefaultConfig, "", 5*time.Minute, time.Minute)
	defer cachedStream.Stop()

	cachedCountStream = sseserver.NewCachedCount(sseserver.DefaultConfig, "", 5)
	defer cachedCountStream.Stop()

	genericStream = sseserver.NewGeneric(sseserver.DefaultConfig, lookupEvents, "0")
	defer genericStream.Stop()

	lastOnlyStream = sseserver.NewLastOnly(sseserver.DefaultConfig)
	defer lastOnlyStream.Stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/sse/cached", cachedHandler)
	mux.HandleFunc("/sse/cached-count", cachedCountHandler)
	mux.HandleFunc("/sse/generic", genericHandler)
	mux.HandleFunc("/sse/last-only", lastOnlyHandler)

	srv := &http.Server{
		Addr:              ":8000",
		Handler:           mux,
		ReadHeaderTimeout: time.Second * 10,
	}
	srv.RegisterOnShutdown(func() {
		cachedStream.DropSubscribers()
		cachedCountStream.DropSubscribers()
		genericStream.DropSubscribers()
		lastOnlyStream.DropSubscribers()
	})

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	go func() { //nolint:wsl
		defer wg.Done()
		eventGenerator(ctx, cachedTopic, time.Second, cachedStream)
	}()

	wg.Add(1)
	go func() { //nolint:wsl
		defer wg.Done()
		eventGenerator(ctx, cachedCountTopic, 2*time.Second, cachedCountStream)
	}()

	wg.Add(1)
	go func() { //nolint:wsl
		defer wg.Done()
		eventGenerator(ctx, genericTopic, 3*time.Second, genericStream)
	}()

	wg.Add(1)
	go func() { //nolint:wsl
		defer wg.Done()
		eventGenerator(ctx, lastOnlyTopic, 4*time.Second, lastOnlyStream)
	}()

	wg.Add(1)
	go func() { //nolint:wsl
		defer wg.Done()
		slog.Info("starting server on :8000")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", slog.Any("error", err))
		}
	}()

	<-ctx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown failed", slog.Any("error", err))
	}

	return nil
}

func cachedHandler(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("Last-Event-ID")
	if err := cachedStream.SubscribeTopic(r.Context(), w, cachedTopic, id); err != nil {
		slog.Error("subscribing to cached stream", slog.Any("error", err))
	}
}

func cachedCountHandler(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("Last-Event-ID")
	if err := cachedCountStream.SubscribeTopic(r.Context(), w, cachedCountTopic, id); err != nil {
		slog.Error("subscribing to cached count stream", slog.Any("error", err))
	}
}

func genericHandler(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("Last-Event-ID")
	if err := genericStream.SubscribeTopic(r.Context(), w, genericTopic, id); err != nil {
		slog.Error("subscribing to generic stream", slog.Any("error", err))
	}
}

func lastOnlyHandler(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("Last-Event-ID")
	if err := lastOnlyStream.SubscribeTopic(r.Context(), w, lastOnlyTopic, id); err != nil {
		slog.Error("subscribing to last only stream", slog.Any("error", err))
	}
}

func lookupEvents(ctx context.Context, topic string, fromStr string, toStr string) ([]sseserver.Event, error) {
	if ctx.Err() != nil {
		// Client disconnected
		return nil, nil
	}

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

func eventGenerator(ctx context.Context, topic string, inc time.Duration, stream sseserver.MultiStream) {
	i := 0

	ticker := time.NewTicker(inc)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, stop generating events
			return
		case <-ticker.C:
			i++
			stream.PublishTopic(topic, newEvent(topic, strconv.Itoa(i)))
		}
	}
}
