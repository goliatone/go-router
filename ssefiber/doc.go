// Package ssefiber provides adapter-neutral SSE route helpers backed by the
// transport-neutral eventstream package. The package owns SSE framing, resume
// handling, retry directives, heartbeat control frames, stream-gap signaling,
// and route mounting concerns; it does not change the generic stream contract
// itself.
//
// Mount supports streaming go-router backends. MountFiber retains the legacy
// Fiber v2 entry point. Hosts cancel owned stream contexts before shutdown;
// transport reader closure cancels subscriptions on client disconnect.
package ssefiber
