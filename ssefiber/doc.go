// Package ssefiber provides Fiber-first SSE route helpers backed by the
// transport-neutral eventstream package. The package owns SSE framing, resume
// handling, retry directives, heartbeat control frames, stream-gap signaling,
// and route mounting concerns; it does not change the generic stream contract
// itself.
//
// v1 is explicit about Fiber-backed go-router usage. Generic net/http streaming
// support can be added separately without moving transport control frames into
// eventstream.
package ssefiber
