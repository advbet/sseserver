// Package ssestream is a library for creating SSE server HTTP handlers.
//
// This library provides a publish/subscribe interface for generating SSE
// streams. It handles keep-alive messages, allows setting client reconnect
// timeout, automatically disconnect long-lived connections. Message data are
// always marshaled to JSON.
//
// This library is targeted for advanced SSE server implementations that
// supports client state resyncing after disconnect. Different client resync
// strategies are provided by multiple Stream interface implementations.
//
// Typical usage of this package is:
// 	* Create new stream object that satisfies Stream interface with one of
//	  the New... constructors.
//	* Start a goroutine that generates events and publishes them via
//	  Publish() method.
//	* Create HTTP handlers that parses Last-Event-ID header, everything else
//	  is handled by the Subscribe() method.
//	* Stitch everything together with a resync function that allows looking
//	  up old events.
//	* If graceful shutdown or dynamic stream creation is required use Stop()
//	  method to deallocate resources and disconnect existing clients.
package ssestream
