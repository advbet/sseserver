// Package ssestream is a library for creating SSE server HTTP handlers.
//
// This library provides a publish/subscribe interface for generating and
// consuming SSE events. It handles SSE wire format, tracks active clients and
// provides interface to resync clients on reconnects.
//
// This package encodes event data using JSON encoding.
package ssestream
