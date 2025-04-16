# sseserver

`sseserver` is a Go library for SSE streams, offering multiple caching strategies and flexible resync logic.

Stream Types:
* `GenericStream`: Custom resync logic for maximum flexibility
* `CachedStream`: Time-based caching of events
* `CachedCountStream`: Fixed-size event caching
* `LastOnlyStream`: Only resends the most recent event

[![Godoc](https://godoc.org/github.com/advbet/sseserver?status.svg)](https://godoc.org/bitbucket.org/advbet/sseserver)

## Installation

```sh
go get -u github.com/advbet/sseserver/v2
```

## Notice

Make sure your HTTP server `WriteTimeout` is bigger than Stream `Lifetime`, otherwise connections will be
closed from HTTP server side and front will not be able to receive events.

## Examples

See `_examples/` directory.
