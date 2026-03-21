// Package eventstream provides a transport-neutral runtime event stream surface
// for go-router. The package owns scoped publish/subscribe behavior, bounded
// replay, stable cursors, scope matching, drop accounting, stats snapshots, and
// optional observability hooks while leaving HTTP/SSE framing to higher-level
// adapters.
//
// v1 keeps stream ownership separate from websocket-specific hubs/history.
// Control frames such as heartbeats, retry directives, and stream gaps belong
// to transport adapters like ssefiber, not to this package.
package eventstream
