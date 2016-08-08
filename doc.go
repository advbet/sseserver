// Package ssestream is a library for creating SSE server HTTP handlers.
//
// This library provides a publish/subscribe interface for generating SSE events
// stream. It handles keep-alive messages, allows setting reconnect server and
// client reconnect timeouts. Message data are always marshaled to JSON.
//
// This library provides an interface that allows resyncing client state after
// reconnect.
//
// Typical usage of this package is - create new stream object with New(). Spawn goroutine
// that creates events and publishes them via stream.Publish(). Create HTTP
// handlers that unmarshal Last-Event-ID to native value and pass request
// handling to library via stream.Subscribe(). Stitch everything together with a
// resync function that allows looking up old events. If graceful shutdown or
// dynamic stream creation is required use stream.Stop() to remove unused
// streams.
package ssestream
