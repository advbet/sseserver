// Package sseserver is a library for creating SSE server HTTP handlers.
//
// This library provides a publish/subscribe interface for generating SSE
// streams. It handles keep-alive messages, allows setting client reconnect
// timeout, automatically disconnect long-lived connections. If provided
// event message data is a slice of bytes it will be sent as is, however if it
// is not a slice of bytes, the data will be marshaled using encoding/json.
//
// This library is targeted for advanced SSE server implementations that
// supports client state resyncing after disconnect. Different client resync
// strategies are provided by multiple Stream interface implementations.
//
// Typical usage of this package is:
//   - Create new stream object that satisfies Stream interface with one of
//     the New... constructors.
//   - Start a goroutine that generates events and publishes them via
//     Publish() method.
//   - Create HTTP handlers that parses Last-Event-ID header, everything else
//     is handled by the Subscribe() method.
//   - If graceful web server shutdown is required use DropSubscribers() to
//     gracefully disconnect all active streams.
//   - If dynamic stream creation is required use Stop() to free resources
//     allocated for sse stream.
package sseserver
