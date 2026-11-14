---
title: go-router SSE Runtime Stream
status: draft
owner: go-router
scope: generic event stream hub + Fiber SSE mount + dedicated browser SSE client
---

# go-router SSE Runtime Stream

## Context

`go-admin` now has a lightweight command runtime that can dispatch small
admin actions over RPC and refresh page fragments without a full reload.
That covers request/response interactions, but it does not provide
authoritative push feedback for command lifecycle, delayed completion, or
related resource state changes.

`wizard-flow` already contains a working SSE implementation with the exact
behavior needed for this class of UI:

- bounded in-memory replay
- stable cursor ids
- tenant-scoped subscriptions
- `Last-Event-ID` resume
- `stream_gap` signaling when replay is unavailable
- heartbeat keepalive
- reconnect/failover/recovery behavior in the browser client

This document captures how to extract and generalize that behavior into
`go-router` so it can support `go-admin` command feedback and other admin
runtime streams.

This design incorporates the review feedback for the feature request:

- v1 is Fiber-first for native go-router handlers
- it does not claim generic `net/http` streaming support on Fiber
- it does not reuse websocket hub/history as the core server abstraction
- it ships a dedicated browser client for SSE
- it keeps the stream contract transport-neutral so WebSocket can fit later

## Goals

- Provide a reusable server-side event stream primitive with:
  - bounded replay buffer
  - stable cursor ids
  - generic scoped subscriptions
  - gap detection when replay is unavailable
  - subscriber backpressure handling
  - explicit stats and observability hooks
- Provide a reusable Fiber SSE mount/helper that:
  - emits `text/event-stream`
  - supports `Last-Event-ID`
  - supports `cursor` query fallback
  - emits `retry:`
  - emits `heartbeat` and `stream_gap` control frames
  - fits go-router/Fiber conventions similarly to `rpcfiber`
- Provide a dedicated browser SSE client with:
  - reconnect and backoff
  - heartbeat timeout detection
  - cursor resume handling
  - failover and recovery callbacks
  - diagnostics for support and runtime introspection
- Keep the abstractions generic enough for:
  - command accepted
  - command running
  - command completed
  - command failed
  - resource state changed
- Preserve a clean path for future WebSocket support under the same logical
  stream contract.

## Non-goals

- Wiring the new stream into `go-admin` in this repo
- Implementing WebSocket transport in v1
- Reusing websocket hub/history types as the stream core
- Providing generic `HandlerFromHTTP` streaming support on Fiber in this pass
- Shipping wizard-specific envelope types or naming into `go-router`

## Constraints And Design Principles

### Fiber-first server scope

`go-router` can stream correctly from native handlers using `SendStream`.
It does not currently provide true streaming for `net/http`-adapted handlers
on Fiber. The SSE feature must therefore be explicit about scope:

- v1 server mount targets native go-router handlers on Fiber
- `HTTPRouter` support is possible for direct handler usage, but Fiber is the
  primary supported adapter for the packaged mount helper
- a future `HandlerFromHTTPStream` helper can be designed separately if
  generic `net/http` streaming is required

### Transport-neutral stream core

The retained event stream must not be coupled to SSE framing. The hub stores
generic records with cursors, scope, event name, and payload. SSE is one
transport that reads from that stream and adds control frames such as
heartbeat and gap signaling.

### Dedicated SSE browser client

The existing TypeScript client in this repo is explicitly websocket-branded.
This feature must ship as a dedicated SSE client package and build surface
rather than extending the websocket client in place.

### Explicit observability

The repo has a minimal logger abstraction, not a metrics SDK. This feature
must expose explicit hooks and stats snapshots rather than implying direct
integration with Prometheus, OpenTelemetry, or other collectors.

## Proposed Package Layout

### Go packages

- `github.com/goliatone/go-router/eventstream`
  - generic replayable scoped stream hub
- `github.com/goliatone/go-router/ssefiber`
  - Fiber/go-router SSE endpoint mount and handler helpers

Rationale:

- `eventstream` keeps replay/cursor/subscription logic independent from HTTP
  and reusable by future transports
- `ssefiber` mirrors the existing `rpcfiber` integration style and makes the
  adapter boundary explicit

### Browser client package

- repo path: `sseclient/`
- intended package identity: dedicated SSE client package

The final published npm name can be decided during implementation, but the
repo structure should be independent from the websocket client:

- `sseclient/package.json`
- `sseclient/src/client.ts`
- `sseclient/README.md`

This keeps the browser contract clean and avoids coupling generic SSE
runtime transport to `@goliatone/websocket-client`.

## Server Architecture

### 1. Generic event stream hub

The hub owns replay, stable cursor assignment, scope partitioning,
subscriber backpressure handling, and stats.

#### Core types

```go
package eventstream

type Scope map[string]string

type MatchFunc func(subscription Scope, published Scope) bool

type Event struct {
    Name       string            `json:"name"`
    Payload    json.RawMessage   `json:"payload,omitempty"`
    Timestamp  time.Time         `json:"timestamp"`
    Metadata   map[string]string `json:"metadata,omitempty"`
}

type Record struct {
    Cursor      string    `json:"cursor"`
    ScopeKey    string    `json:"scope_key"`
    Event       Event     `json:"event"`
    PublishedAt time.Time `json:"published_at"`
}

type Subscription struct {
    Scope           Scope
    ScopeKey        string
    Records         <-chan Record
    CursorGap       bool
    CursorGapReason string
}

type Stats struct {
    PublishedCount     int64
    ResumeCount        int64
    ActiveSubscribers  int
    BufferedRecords    int
    DropReasons        map[string]int64
}

type Stream interface {
    Publish(scope Scope, event Event) Record
    Subscribe(ctx context.Context, scope Scope, afterCursor string) (*Subscription, error)
    SnapshotStats() Stats
}
```

#### Scope model

`Scope` is generic key/value data, for example:

- `{"tenant":"t1"}`
- `{"tenant":"t1","resource":"esign:agreement:123"}`
- `{"tenant":"t1","channel":"admin.commands"}`

The hub does not interpret scope semantics. It normalizes the scope into a
stable canonical `ScopeKey` by sorting keys and serializing as a deterministic
string. Published records are retained by that key.

Subscription matching is configurable at stream construction time via a
matcher function:

- default matcher: exact match on the full normalized scope
- optional matcher: subset/hierarchical matching for cases such as
  tenant-wide subscriptions receiving resource-scoped events
- apps may provide a custom matcher when their routing semantics differ

The stream should ship with built-in matcher helpers, for example:

- `eventstream.ExactMatch` (default)
- `eventstream.SubsetMatch`

The matcher is used for:

- live delivery decisions
- replay buffer selection when a subscriber resumes

Replay selection for non-exact matchers is defined as: gather all retained
scope buckets whose published scope matches the subscriber scope, merge the
records, and order them by global cursor before delivery.

#### Replay model

- each published scope key has its own bounded replay buffer
- cursors are global monotonic ids for the hub instance
- subscribe performs an atomic cutover:
  - determine the matched scope buckets for the requested subscription
  - snapshot replay state for those buckets
  - register the live subscriber
  - return a single ordered `Records` stream that yields replay first and
    live records after the cutover point
- on subscribe:
  - no cursor: start live
  - cursor found in retained matched buffers: replay later events, then go
    live on the same ordered stream
  - cursor missing and matched retained data exists: mark `CursorGap=true`

#### Backpressure model

- subscribers receive ordered records over bounded channels owned by the
  stream core
- if a subscriber channel is full, the subscriber is dropped
- drop reason is recorded as `slow_consumer`

Drop reasons:

- `slow_consumer`
- `client_disconnect`
- `cursor_not_found`

#### Observability hooks

The hub exposes optional hooks:

```go
type Hooks struct {
    OnPublish   func(PublishEvent)
    OnSubscribe func(SubscribeEvent)
    OnDrop      func(DropEvent)
}
```

These are for logging, counters, tracing, or test instrumentation. The hub
also exposes `SnapshotStats()` for pull-style inspection.

Hook execution must not hold stream locks. Implementations should either:

- invoke hooks outside critical sections and document that hooks must return
  quickly, or
- dispatch hooks asynchronously with bounded internal protection

The implementation should recover from hook panics so observability cannot
take down the stream.

### 2. SSE Fiber mount/helper

The SSE layer reads from `eventstream.Stream`, frames SSE responses, emits
heartbeat/gap control frames, and manages request lifetime.

#### Mount surface

Preferred API:

```go
package ssefiber

type Option func(*Config)

type ScopeResolver func(router.Context) (eventstream.Scope, error)

func MountFiber(r router.Router[*fiber.App], opts ...Option) error
func Handler(opts ...Option) router.HandlerFunc
```

#### Config

```go
type Config struct {
    Path                string
    Stream              eventstream.Stream
    ScopeResolver       ScopeResolver
    HeartbeatInterval   time.Duration
    RetryInterval       time.Duration
    CursorQueryParam    string
    HeartbeatQueryParam string
    RetryQueryParam     string
    AllowClientTuning   bool
    MinHeartbeatInterval time.Duration
    MaxHeartbeatInterval time.Duration
    MinRetryInterval    time.Duration
    MaxRetryInterval    time.Duration
    Hooks               Hooks
}
```

Defaults:

- `Path`: `/events`
- `HeartbeatInterval`: `15s`
- `RetryInterval`: `3s`
- `CursorQueryParam`: `cursor`
- `HeartbeatQueryParam`: `heartbeat_ms`
- `RetryQueryParam`: `retry_ms`
- `AllowClientTuning`: `false`
- `MinHeartbeatInterval`: `5s`
- `MaxHeartbeatInterval`: `60s`
- `MinRetryInterval`: `1s`
- `MaxRetryInterval`: `30s`

Behavior:

1. Resolve scope from request context using `ScopeResolver`.
2. Read resume cursor:
   - `Last-Event-ID` header first
   - query param fallback second
3. Resolve effective heartbeat/retry intervals:
   - start from server config defaults
   - if `AllowClientTuning` is enabled, read `heartbeat_ms` / `retry_ms`
     query params
   - ignore invalid values
   - clamp accepted values to configured min/max bounds
4. Subscribe to the stream.
5. Set SSE headers:
   - `Content-Type: text/event-stream`
   - `Cache-Control: no-cache`
   - `Connection: keep-alive`
   - `X-Accel-Buffering: no`
6. Emit initial `retry:` directive using the effective retry interval.
7. If replay gap exists:
   - emit `stream_gap`
   - close the SSE stream
   - rely on the browser client to request an authoritative snapshot or
     fail over
8. Emit ordered records from the subscription stream.
9. Emit periodic `heartbeat` control frames using the effective heartbeat
   interval.

#### Why a Fiber-specific mount

This makes the feature honest about the current adapter behavior. It avoids
promising streaming behavior that `HandlerFromHTTP` on Fiber does not yet
provide.

#### SSE frame contract

Retained domain events:

```text
id: 42
event: lifecycle
data: {"command":"review.notify","status":"running","timestamp":"..."}
```

Control frames are not retained in replay:

```text
event: heartbeat
data: {"timestamp":"...","scope_key":"tenant=t1"}
```

```text
event: stream_gap
data: {
  "reason":"cursor_not_found",
  "last_event_id":"42",
  "fallback_transport":"polling",
  "resume_supported":false,
  "requires_gap_reconcile":true,
  "timestamp":"..."
}
```

Rules:

- only retained domain events include `id: <cursor>`
- heartbeat does not advance cursor
- `stream_gap` does not advance cursor
- `retry:` is emitted once at stream start
- after `stream_gap`, the server closes the SSE response

### 3. Dedicated browser SSE client

The browser transport should use `fetch` stream consumption rather than
`EventSource` because it needs:

- explicit authorization headers
- custom reconnect logic
- explicit heartbeat timeout detection
- deterministic cursor resume behavior
- richer diagnostics and callbacks

#### Client package structure

Proposed:

- `sseclient/src/client.ts`
- `sseclient/package.json`
- `sseclient/README.md`

The package is intentionally separate from the websocket client.

#### Client API

```ts
export type ConnectionState =
  | "disconnected"
  | "connecting"
  | "connected"
  | "reconnecting"
  | "degraded"
  | "failed";

export interface Diagnostics {
  connectionState: ConnectionState;
  lastEventId: string | null;
  lastHeartbeatAt: string | null;
  lastEventAt: string | null;
  reconnectAttempts: number;
  totalEventsReceived: number;
  gapEventsReceived: number;
  failoverTriggered: boolean;
  failoverReason: string | null;
  streamUrl: string;
}

export interface ClientOptions {
  url: string;
  heartbeatMs?: number;
  retryMs?: number;
  heartbeatTimeoutMs?: number;
  maxReconnectAttempts?: number;
  getHeaders?: () => HeadersInit | Promise<HeadersInit>;
  onEvent?: (event: StreamEvent) => void;
  onHeartbeat?: (event: HeartbeatEvent) => void;
  onStreamGap?: (event: StreamGapEvent) => void;
  onConnectionStateChange?: (state: ConnectionState, diagnostics: Diagnostics) => void;
  onFailover?: (reason: string, diagnostics: Diagnostics) => void;
  onRecovery?: (diagnostics: Diagnostics) => void;
  onRequestSnapshot?: () => void;
}

export interface SSEClient {
  start(): void;
  stop(): void;
  isConnected(): boolean;
  getDiagnostics(): Diagnostics;
  triggerFailover(reason: string): void;
  attemptRecovery(): void;
}
```

#### Client behavior

- open stream with `Accept: text/event-stream`
- send auth/custom headers via `getHeaders`
- include `cursor` query param on reconnect when `lastEventId` is known
- include `heartbeat_ms` / `retry_ms` query params only when the deployment
  opts into client-tunable intervals
- parse frames incrementally
- reset heartbeat timeout on `heartbeat`
- mark connection `degraded` after missed heartbeat window
- trigger failover on:
  - `stream_gap`
  - repeated reconnect exhaustion
  - sustained heartbeat timeout
  - unrecoverable auth failure
- invoke `onRequestSnapshot` when recovery requires authoritative reconcile
- support `attemptRecovery()` from failover state

#### Dedicated client output

The client build should mirror the websocket client structure but remain
independent:

- dev bundle
- minified bundle
- TypeScript declarations

## Transport-Neutral Event Model

The stream core should carry domain events, not transport control frames.

Recommended application event names:

- `lifecycle`
- `resource_state`
- `resource_updated`
- `notification`

Recommended payload examples for admin command feedback:

```json
{
  "command": "esign.agreements.notify_reviewers",
  "status": "accepted",
  "command_id": "cmd_123",
  "resource_id": "agreement_42",
  "timestamp": "2026-03-20T10:00:00Z"
}
```

```json
{
  "command": "esign.agreements.notify_reviewers",
  "status": "completed",
  "command_id": "cmd_123",
  "resource_id": "agreement_42",
  "fragments": [
    "#agreement-review-status-panel",
    "#review-participants-panel"
  ],
  "timestamp": "2026-03-20T10:00:03Z"
}
```

```json
{
  "resource": "esign.agreement",
  "resource_id": "agreement_42",
  "change": "state_changed",
  "state": "review_closed",
  "timestamp": "2026-03-20T10:00:03Z"
}
```

This keeps `go-router` generic while fitting the intended `go-admin`
command runtime.

## Example Usage

### Server

```go
hub := eventstream.New(
    eventstream.WithBufferSize(512),
    eventstream.WithSubscriberBuffer(32),
    eventstream.WithMatcher(eventstream.ExactMatch),
)

err := ssefiber.MountFiber(
    app.Router(),
    ssefiber.WithPath("/api/runtime/events"),
    ssefiber.WithStream(hub),
    ssefiber.WithScopeResolver(func(ctx router.Context) (eventstream.Scope, error) {
        return eventstream.Scope{
            "tenant": ctx.Header("X-Tenant-ID"),
            "channel": "admin.commands",
        }, nil
    }),
)
if err != nil {
    panic(err)
}
```

### Browser

```ts
const client = createSSEClient({
  url: "/api/runtime/events",
  getHeaders: () => ({
    Authorization: `Bearer ${token}`,
  }),
  onEvent: (event) => {
    if (event.name === "lifecycle") {
      reconcileCommandState(event.payload);
    }
  },
  onStreamGap: () => {
    refreshAuthoritativeSnapshot();
  },
  onRequestSnapshot: () => {
    refreshAuthoritativeSnapshot();
  },
});

client.start();
```

## Testing Plan

### Go unit tests: `eventstream`

- publish delivers to active subscriber
- exact-match scope isolation prevents cross-scope delivery
- subset/custom matcher delivery works when configured
- replay returns ordered records after known cursor
- subscribe performs atomic replay-to-live cutover without gaps or duplicate
  delivery
- missing cursor with matched retained buffer marks gap
- empty buffer with unknown cursor does not mark gap
- slow consumer is dropped and counted
- disconnect removes subscriber and records drop reason
- stats snapshot reflects resumes, buffered records, active subscribers

### Fiber integration tests: `ssefiber`

- endpoint returns `text/event-stream`
- resume precedence uses `Last-Event-ID` before `cursor`
- `retry:` directive emitted
- client-tunable `heartbeat_ms` / `retry_ms` values are ignored unless
  explicitly enabled and are clamped when enabled
- heartbeat emitted at configured interval
- `stream_gap` emitted on missing replay cursor and the stream closes
- replayed records arrive before later live records on the ordered stream
- request cancel disconnects subscriber
- scope resolver failure returns request error

### Browser client tests: `sseclient`

- parses SSE frames correctly
- stores `lastEventId` from retained event frames
- reconnect appends cursor query param
- heartbeat timeout changes state to `degraded`
- max reconnect attempts triggers failover
- `stream_gap` triggers failover and snapshot request
- recovery path clears failover and resumes SSE
- auth failure hook can recover and reconnect

## Documentation Plan

- add package docs for `eventstream`
- add package docs for `ssefiber`
- add `sseclient/README.md`
- add a short top-level README section describing:
  - when to use SSE vs websocket
  - Fiber-first scope for the SSE mount
  - how to publish generic command/resource lifecycle events

## Implementation Plan

1. Add `eventstream` package
   - stream core
   - scope normalization
   - cursoring
   - replay buffers
   - stats/hooks
   - unit tests
2. Add `ssefiber` package
   - config/options
   - handler
   - mount helper
   - SSE framing helpers
   - Fiber integration tests
3. Add `sseclient` package
   - TypeScript client
   - build scripts
   - type definitions
   - client tests
4. Add docs
   - package docs
   - usage examples
   - operational notes on failover and gap handling

## Alternatives Considered

### Extend websocket hub/history

Rejected for v1.

Reasons:

- websocket hub types are transport-specific
- current history model is keyed for websocket use cases, not generic scoped
  cursor replay
- it would force SSE semantics into a websocket-centric abstraction

### Add SSE directly to the existing websocket client package

Rejected.

Reasons:

- it weakens package identity
- it mixes two transport lifecycles in one client surface
- a dedicated client is simpler to reason about and document

### Use browser `EventSource`

Rejected for the default client implementation.

Reasons:

- no custom auth headers
- weaker control over reconnect policy
- less visibility into diagnostics and degraded states

## Acceptance Criteria

- a generic server stream exists with replay, cursor resume, configurable
  scope matching, ordered delivery, and stats
- an SSE Fiber mount exists and emits `heartbeat` and `stream_gap`
- resume works via `Last-Event-ID` and `cursor`
- a dedicated browser SSE client exists in its own package
- tests cover basic delivery, isolation, replay, gap, heartbeat, reconnect,
  and failover behavior
- docs clearly state Fiber-first support and future WebSocket compatibility

## Open Questions

- should `ssefiber` support direct `HTTPRouter` mounting in v1, or should that
  remain an implementation detail after Fiber support is complete?
- what npm package name should be used for the dedicated client?
- should the browser client expose a small transport-neutral interface from day
  one, or only its SSE-specific API plus internal extension points?
