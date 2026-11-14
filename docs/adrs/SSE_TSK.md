# SSE Runtime Stream Task Plan - go-router

This document organizes the SSE runtime stream implementation in `github.com/goliatone/go-router` into actionable phases and tasks. Mark tasks complete with `- [x]` as work progresses.

## Phase 1: Establish Package Structure and Public API
**Reference:** "Proposed Package Layout" and "Server Architecture" sections of `SSE_TDD.md`

### Phase 1 Context
- **Why**  
  The feature needs clear package boundaries before implementation so the stream core, Fiber integration, and browser client do not collapse into existing websocket-specific code.
- **What**  
  Create the new package layout for `eventstream`, `ssefiber`, and `sseclient`, and define the initial public API surfaces and config types.
- **How**  
  1. Create `eventstream` package with core public types and constructor surface.  
  2. Create `ssefiber` package with mount/handler/config types.  
  3. Create `sseclient` package structure with TypeScript entrypoint and build metadata.  
  4. Add package doc comments describing scope and transport boundaries.

### Phase 1 Tasks:
- [x] **Task 1.1**: Create `eventstream` package with public types (`Scope`, `MatchFunc`, `Event`, `Record`, `Subscription`, `Stats`, `Stream`).
- [x] **Task 1.2**: Add `eventstream` constructor/config surface (`New`, options, defaults, matcher selection, built-in matcher helpers).
- [x] **Task 1.3**: Create `ssefiber` package with `MountFiber`, `Handler`, `Option`, `Config`, and `ScopeResolver`.
- [x] **Task 1.4**: Create `sseclient/` package structure with `package.json`, `src/client.ts`, and README stub.
- [x] **Task 1.5**: Add package doc comments describing Fiber-first scope, generic stream ownership, and dedicated SSE client intent.

## Phase 2: Implement Event Stream Core
**Reference:** "1. Generic event stream hub" section of `SSE_TDD.md`

### Phase 2 Context
- **Why**  
  Replay, stable cursors, scoped delivery, and subscriber lifecycle are the foundation for both SSE and any future realtime transport.
- **What**  
  Implement the in-memory stream hub that stores retained records, assigns cursors, partitions by scope, and fans out live events to subscribers.
- **How**  
  1. Define the internal stream struct and configuration defaults.  
  2. Normalize scopes into deterministic scope keys.  
  3. Add configurable scope matching with a safe default.  
  4. Assign global monotonic cursors.  
  5. Retain a bounded per-published-scope replay buffer.  
  6. Deliver ordered records to matching subscribers over bounded channels.

### Phase 2 Tasks:
- [x] **Task 2.1**: Implement stream constructor with configurable buffer size and subscriber queue size.
- [x] **Task 2.2**: Implement deterministic scope normalization, canonical scope key generation, and built-in exact/subset matcher helpers.
- [x] **Task 2.3**: Implement `Publish(scope, event)` with global cursor assignment and per-scope retention.
- [x] **Task 2.4**: Implement `Subscribe(ctx, scope, afterCursor)` with atomic replay snapshot plus live registration and a single ordered records stream.
- [x] **Task 2.5**: Ensure subscriber cleanup on context cancellation and safe concurrent access.

## Phase 3: Add Replay, Gap Detection, and Stats
**Reference:** "Replay model", "Backpressure model", and "Observability hooks" sections of `SSE_TDD.md`

### Phase 3 Context
- **Why**  
  The stream is only useful for admin/runtime UIs if reconnects can resume safely and signal when authoritative replay is no longer possible.
- **What**  
  Add replay-after-cursor behavior, cursor gap signaling, drop accounting, and snapshot stats.
- **How**  
  1. Detect cursor presence within the retained matched replay set.  
  2. Return ordered replay records when the cursor is available.  
  3. Mark gaps when replay is unavailable but matched retained state exists.  
  4. Track resume count, active subscribers, buffered records, and drop reasons.  
  5. Drop slow consumers when subscriber queues fill.

### Phase 3 Tasks:
- [x] **Task 3.1**: Implement replay lookup across matched scope buffers and ordered replay delivery for resumed subscriptions.
- [x] **Task 3.2**: Implement cursor gap detection and `CursorGap` / `CursorGapReason`.
- [x] **Task 3.3**: Implement slow-consumer drop behavior and `slow_consumer` accounting.
- [x] **Task 3.4**: Implement disconnect and `client_disconnect` accounting.
- [x] **Task 3.5**: Implement `SnapshotStats()` including resume count, drop reasons, active subscribers, and buffered record totals.

## Phase 4: Add Stream Hooks and Internal Validation
**Reference:** "Explicit observability" and "Observability hooks" sections of `SSE_TDD.md`

### Phase 4 Context
- **Why**  
  The repo does not have a built-in metrics framework, so the stream must provide explicit hooks and defensible validation behavior at the package level.
- **What**  
  Add optional lifecycle hooks for publish/subscribe/drop events and validate config defaults and edge cases.
- **How**  
  1. Define hook event payloads.  
  2. Call hooks outside critical sections where appropriate.  
  3. Recover from hook panics and keep observability isolated from stream correctness.  
  4. Validate zero/negative config values and normalize to safe defaults.  
  5. Keep hook behavior optional and non-blocking from the caller point of view.

### Phase 4 Tasks:
- [x] **Task 4.1**: Define `Hooks` and hook event payload types for publish, subscribe, and drop operations.
- [x] **Task 4.2**: Wire hook invocation into stream lifecycle paths.
- [x] **Task 4.3**: Recover from hook panics and ensure hooks do not execute while holding stream locks.
- [x] **Task 4.4**: Normalize invalid stream config values to safe defaults.
- [x] **Task 4.5**: Add tests covering hooks and default normalization behavior.

## Phase 5: Implement Fiber SSE Framing and Handler Core
**Reference:** "2. SSE Fiber mount/helper" and "SSE frame contract" sections of `SSE_TDD.md`

### Phase 5 Context
- **Why**  
  The transport layer must translate retained stream records into standards-compliant SSE frames while staying honest about Fiber-only support in v1.
- **What**  
  Implement the core `ssefiber` handler that sets stream headers, reads from the stream subscription, and writes SSE frames to the response.
- **How**  
  1. Implement config defaults and options.  
  2. Resolve the request scope.  
  3. Resolve effective heartbeat/retry intervals from config and optional client tuning.  
  4. Subscribe to the event stream.  
  5. Set SSE headers and start streaming via `SendStream`.  
  6. Write ordered retained/live domain events as `id/event/data` frames.

### Phase 5 Tasks:
- [x] **Task 5.1**: Implement `ssefiber.Config` defaults and option wiring.
- [x] **Task 5.2**: Implement request scope resolution and request-time validation for missing stream/scope resolver.
- [x] **Task 5.3**: Implement client-tunable heartbeat/retry parsing with explicit opt-in, validation, and clamping.
- [x] **Task 5.4**: Implement SSE frame writers for generic event frames.
- [x] **Task 5.5**: Implement the streaming loop using `io.Pipe` plus `SendStream`.
- [x] **Task 5.6**: Ensure request cancellation cleanly closes the subscription and stream writer.

## Phase 6: Add Resume, Heartbeat, Retry, and Gap Control Frames
**Reference:** "Behavior" and "SSE frame contract" sections of `SSE_TDD.md`

### Phase 6 Context
- **Why**  
  Browser resilience depends on resume semantics, heartbeat visibility, retry hints, and explicit gap signaling.
- **What**  
  Extend the SSE layer to support `Last-Event-ID`, cursor query fallback, heartbeat control frames, `retry:` directives, and `stream_gap`.
- **How**  
  1. Read resume cursor from header then query param.  
  2. Emit initial `retry:` directive using the effective retry interval.  
  3. Emit `stream_gap` when replay is unavailable, then close the SSE response.  
  4. Emit periodic `heartbeat` frames using the effective heartbeat interval.  
  5. Keep control frames out of retained replay state.

### Phase 6 Tasks:
- [x] **Task 6.1**: Implement resume precedence for `Last-Event-ID` over `cursor`.
- [x] **Task 6.2**: Implement configurable `retry:` directive output.
- [x] **Task 6.3**: Implement `stream_gap` control frame payloads, emission rules, and stream termination behavior.
- [x] **Task 6.4**: Implement heartbeat scheduling, bounds, and `heartbeat` frame payloads.
- [x] **Task 6.5**: Add tests covering resume precedence, heartbeat, retry, client tuning, and gap-close behavior.

## Phase 7: Implement Fiber Mount Helper and Integration Surface
**Reference:** "Mount surface", "Config", and "Example Usage" sections of `SSE_TDD.md`

### Phase 7 Context
- **Why**  
  The feature should fit existing go-router conventions and be easy to mount similarly to `rpcfiber`.
- **What**  
  Add a mount helper that registers the SSE route on a Fiber-backed go-router router and documents the supported integration style.
- **How**  
  1. Implement `MountFiber(r, opts...)`.  
  2. Default the route path and route registration.  
  3. Keep the package explicit about Fiber-first support.  
  4. Expose the underlying handler for apps that want custom route registration.

### Phase 7 Tasks:
- [x] **Task 7.1**: Implement `MountFiber(r router.Router[*fiber.App], opts ...Option) error`.
- [x] **Task 7.2**: Implement `Handler(opts ...Option) router.HandlerFunc`.
- [x] **Task 7.3**: Validate mount-time errors for nil router, missing stream, and invalid path/config.
- [x] **Task 7.4**: Add Fiber integration tests for route mounting and end-to-end streaming.

## Phase 8: Create Dedicated Browser SSE Client Package
**Reference:** "3. Dedicated browser SSE client" section of `SSE_TDD.md`

### Phase 8 Context
- **Why**  
  The browser client is a deliverable of the feature and must be separated from the websocket-branded package.
- **What**  
  Build a dedicated TypeScript SSE client package with its own entrypoint, build scripts, and type declarations.
- **How**  
  1. Set up package metadata and build scripts.  
  2. Define the public client API and diagnostics types.  
  3. Implement start/stop lifecycle methods.  
  4. Generate distributable JS bundles and declarations.

### Phase 8 Tasks:
- [x] **Task 8.1**: Add `sseclient/package.json` with dedicated package identity and build scripts.
- [x] **Task 8.2**: Implement TypeScript public types (`ConnectionState`, `Diagnostics`, client options, event payload types).
- [x] **Task 8.3**: Implement `createSSEClient` / client constructor with start, stop, diagnostics, and recovery methods.
- [x] **Task 8.4**: Generate distributable bundle outputs and TypeScript declarations.
- [x] **Task 8.5**: Ensure package docs and metadata do not reuse websocket-specific naming.

## Phase 9: Implement Browser Frame Parsing, Resume, and Reconnect
**Reference:** "Client behavior" section of `SSE_TDD.md`

### Phase 9 Context
- **Why**  
  The client must be able to parse raw SSE frames, track cursor state, and survive normal disconnects or network churn.
- **What**  
  Implement fetch-stream reading, incremental frame parsing, cursor tracking, and reconnect/backoff behavior.
- **How**  
  1. Use `fetch` with `Accept: text/event-stream`.  
  2. Read and parse frames incrementally from the response body.  
  3. Track `lastEventId` from retained events.  
  4. Reconnect using exponential backoff with jitter.  
  5. Include `cursor` on reconnect when available.  
  6. Include `heartbeat_ms` / `retry_ms` only when client tuning is enabled for the deployment.

### Phase 9 Tasks:
- [x] **Task 9.1**: Implement incremental SSE frame parsing with support for `event`, `id`, and multi-line `data`.
- [x] **Task 9.2**: Implement retained event delivery and `lastEventId` tracking.
- [x] **Task 9.3**: Implement reconnect scheduling with exponential backoff and jitter.
- [x] **Task 9.4**: Implement reconnect URL generation with `cursor` and optional `heartbeat_ms` / `retry_ms` query params only when configured.
- [x] **Task 9.5**: Add tests covering parsing, cursor tracking, and reconnect URL behavior.

## Phase 10: Implement Browser Heartbeat, Failover, Recovery, and Diagnostics
**Reference:** "Client behavior" and "Dedicated client output" sections of `SSE_TDD.md`

### Phase 10 Context
- **Why**  
  The main value of the browser client is resilient admin behavior: detect degraded streams, fail over cleanly, and surface diagnostics for support and UI logic.
- **What**  
  Add heartbeat timeout detection, failover rules, recovery flow, snapshot callbacks, and diagnostics updates.
- **How**  
  1. Track heartbeat timestamps and lifecycle timestamps.  
  2. Move through connection states predictably.  
  3. Trigger failover on stream gap, reconnect exhaustion, heartbeat timeout, or unrecoverable auth failure.  
  4. Support manual recovery attempts.  
  5. Keep diagnostics current across all state changes.

### Phase 10 Tasks:
- [x] **Task 10.1**: Implement heartbeat timeout detection and `degraded` state transitions.
- [x] **Task 10.2**: Implement failover triggers for `stream_gap`, reconnect exhaustion, and auth failure.
- [x] **Task 10.3**: Implement `onRequestSnapshot`, `onFailover`, and `onRecovery` callback wiring.
- [x] **Task 10.4**: Implement diagnostics updates for connection state, counters, timestamps, and failover state.
- [x] **Task 10.5**: Add tests covering failover, recovery, heartbeat degradation, and diagnostics surfaces.

## Phase 11: Documentation and Examples
**Reference:** "Documentation Plan", "Example Usage", and "Acceptance Criteria" sections of `SSE_TDD.md`

### Phase 11 Context
- **Why**  
  The feature spans Go server code, Fiber route registration, and a separate browser package. Adoption will fail without concise guidance.
- **What**  
  Add package docs, README coverage, and examples showing how to mount the SSE endpoint and consume it from the browser client.
- **How**  
  1. Document the stream hub and SSE mount packages.  
  2. Add `sseclient` README content.  
  3. Update the top-level README with a short SSE section.  
  4. Document Fiber-first scope and non-goals clearly.

### Phase 11 Tasks:
- [x] **Task 11.1**: Add package docs for `eventstream`.
- [x] **Task 11.2**: Add package docs for `ssefiber`.
- [x] **Task 11.3**: Write `sseclient/README.md` with setup, API, and failover notes.
- [x] **Task 11.4**: Update top-level README with SSE usage, scope, and transport positioning.
- [x] **Task 11.5**: Add short example snippets for publishing command/resource lifecycle events.

## Phase 12: End-to-End Validation and Release Readiness
**Reference:** "Testing Plan", "Implementation Plan", and "Acceptance Criteria" sections of `SSE_TDD.md`

### Phase 12 Context
- **Why**  
  This feature has concurrency, streaming, and browser-runtime edges. End-to-end validation is needed before it can be considered reusable infrastructure.
- **What**  
  Run the full validation matrix, ensure the feature matches the TDD scope, and confirm docs/build outputs are complete.
- **How**  
  1. Run Go unit and integration tests.  
  2. Run TypeScript build and client tests.  
  3. Verify route mounting and example usage manually where needed.  
  4. Audit the output against the TDD acceptance criteria.

### Phase 12 Tasks:
- [x] **Task 12.1**: Run `go test` for `eventstream`, `ssefiber`, and impacted router packages.
- [x] **Task 12.2**: Run `npm` or package-local build/test commands for `sseclient`.
- [x] **Task 12.3**: Verify the generated client bundles and declaration files are produced correctly.
- [x] **Task 12.4**: Audit docs and package names to ensure no websocket-specific naming leaked into the SSE feature.
- [x] **Task 12.5**: Review implementation against `SSE_TDD.md` acceptance criteria and close gaps.

---

**Suggested Validation Commands**
- `go test ./...`
- `npm --prefix sseclient install`
- `npm --prefix sseclient run build`
- `npm --prefix sseclient test`

**Implementation Notes**
- Keep v1 explicit about Fiber-first support.
- Do not fold the stream core into websocket hub/history.
- Do not extend the existing websocket client package for SSE.
- Keep transport-neutral event semantics in the stream core; control frames belong to `ssefiber`, not `eventstream`.

**Open Decisions To Confirm Before Implementation Starts**
- Exact npm package name for `sseclient`
- Whether `ssefiber` should expose any `HTTPRouter` convenience in v1 or remain strictly Fiber-mounted
- Whether the browser client should export a minimal transport-neutral interface in addition to the SSE-specific client
