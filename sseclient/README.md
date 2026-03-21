# `@goliatone/go-router-sse-client`

Dedicated browser SSE client for go-router runtime streams.

## What It Provides

- Fetch-based SSE consumption with custom request headers
- Cursor resume on reconnect using `Last-Event-ID` semantics
- Reconnect with exponential backoff and jitter
- Heartbeat timeout detection with `degraded` and `failed` states
- `stream_gap` failover handling and snapshot request hooks
- Diagnostics for support tooling and runtime inspection

## Installation

```bash
npm install @goliatone/go-router-sse-client
```

## Usage

```ts
import createSSEClient from "@goliatone/go-router-sse-client";

const client = createSSEClient({
  url: "/api/runtime/events",
  getHeaders: () => ({
    Authorization: `Bearer ${token}`,
  }),
  heartbeatMs: 15000,
  retryMs: 3000,
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

## Public API

`createSSEClient(options)` returns an `SSEClient` with:

- `start()`
- `stop()`
- `isConnected()`
- `getDiagnostics()`
- `triggerFailover(reason)`
- `attemptRecovery()`

Connection states:

- `disconnected`
- `connecting`
- `connected`
- `reconnecting`
- `degraded`
- `failed`

Diagnostics include:

- `lastEventId`
- `lastHeartbeatAt`
- `lastEventAt`
- `reconnectAttempts`
- `totalEventsReceived`
- `gapEventsReceived`
- `failoverTriggered`
- `failoverReason`
- `streamUrl`

## Failover And Recovery

The client enters failover when:

- the server emits `stream_gap`
- reconnect attempts are exhausted
- the heartbeat timeout is missed twice in a row
- the initial request fails with an unrecoverable auth status

When failover is triggered:

- `onFailover` receives the reason and diagnostics
- `onRequestSnapshot` runs for gap reconciliation
- automatic reconnect stops until `attemptRecovery()` is called

`attemptRecovery()` clears failover state and opens a fresh SSE connection. If
the recovery connection succeeds, `onRecovery` is invoked.

## Build And Test

```bash
npm install
npm run build
npm test
```

Build outputs:

- `dist/client.mjs`
- `dist/client.js`
- `dist/client.min.js`
- `dist/client.d.ts`
