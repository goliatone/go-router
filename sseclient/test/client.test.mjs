import test from "node:test";
import assert from "node:assert/strict";

import createSSEClient from "../dist/client.mjs";

const encoder = new TextEncoder();
const originalFetch = globalThis.fetch;
const originalLocation = globalThis.location;
const originalRandom = Math.random;

test.afterEach(() => {
  globalThis.fetch = originalFetch;
  globalThis.location = originalLocation;
  Math.random = originalRandom;
});

test("parses frames, tracks lastEventId, and reconnects with cursor and tuning params", async () => {
  const urls = [];
  const authHeaders = [];
  let callCount = 0;

  globalThis.location = { href: "https://example.test/admin" };
  Math.random = () => 0;

  globalThis.fetch = async (url, init = {}) => {
    callCount += 1;
    urls.push(String(url));
    authHeaders.push(new Headers(init.headers).get("Authorization"));

    if (callCount === 1) {
      return new Response(
        createStream(["id: 42\nevent: lifecycle\ndata: first line\ndata: second line\n\n"], init.signal),
        { status: 200 },
      );
    }

    return new Response(createStream([], init.signal, { keepOpen: true }), { status: 200 });
  };

  const received = [];
  const client = createSSEClient({
    url: "/events",
    retryMs: 5,
    heartbeatTimeoutMs: 1000,
    enableClientTuning: true,
    heartbeatMs: 25,
    getHeaders: async () => ({ Authorization: "Bearer token" }),
    onEvent: (event) => received.push(event),
  });

  client.start();

  await waitFor(() => received.length === 1);
  await waitFor(() => urls.length >= 2);

  const diagnostics = client.getDiagnostics();
  assert.equal(received[0].id, "42");
  assert.equal(received[0].payload, "first line\nsecond line");
  assert.equal(diagnostics.lastEventId, "42");
  assert.equal(authHeaders[0], "Bearer token");
  assert.match(urls[1], /cursor=42/);
  assert.match(urls[1], /heartbeat_ms=25/);
  assert.match(urls[1], /retry_ms=5/);

  client.stop();
});

test("fails over on stream_gap and requests a snapshot", async () => {
  let snapshotRequests = 0;
  const failovers = [];

  globalThis.fetch = async (_url, init = {}) =>
    new Response(
      createStream(
        [
          "event: stream_gap\n" +
            'data: {"reason":"cursor_not_found","last_event_id":"42","requires_gap_reconcile":true}\n\n',
        ],
        init.signal,
      ),
      { status: 200 },
    );

  const client = createSSEClient({
    url: "https://example.test/events",
    heartbeatTimeoutMs: 1000,
    onRequestSnapshot: () => {
      snapshotRequests += 1;
    },
    onFailover: (reason, diagnostics) => {
      failovers.push({ reason, diagnostics });
    },
  });

  client.start();

  await waitFor(() => failovers.length === 1);

  const diagnostics = client.getDiagnostics();
  assert.equal(snapshotRequests, 1);
  assert.equal(failovers[0].reason, "stream_gap");
  assert.equal(diagnostics.connectionState, "failed");
  assert.equal(diagnostics.failoverTriggered, true);
  assert.equal(diagnostics.failoverReason, "stream_gap");
  assert.equal(diagnostics.gapEventsReceived, 1);
});

test("marks the connection degraded before heartbeat timeout failover", async () => {
  const states = [];

  globalThis.fetch = async (_url, init = {}) =>
    new Response(createStream([], init.signal, { keepOpen: true }), { status: 200 });

  const client = createSSEClient({
    url: "https://example.test/events",
    heartbeatTimeoutMs: 10,
    onConnectionStateChange: (state) => {
      states.push(state);
    },
  });

  client.start();

  await waitFor(() => states.includes("degraded"));
  await waitFor(() => client.getDiagnostics().connectionState === "failed");

  assert.ok(states.includes("connected"));
  assert.ok(states.includes("degraded"));
  assert.equal(client.getDiagnostics().failoverReason, "heartbeat_timeout");
});

test("fails over on unrecoverable auth failure", async () => {
  globalThis.fetch = async () => new Response("unauthorized", { status: 401 });

  const client = createSSEClient({
    url: "https://example.test/events",
  });

  client.start();

  await waitFor(() => client.getDiagnostics().connectionState === "failed");
  assert.equal(client.getDiagnostics().failoverReason, "auth_failed");
});

test("attemptRecovery clears failover and resumes the stream", async () => {
  let callCount = 0;
  let recoveries = 0;
  const received = [];

  globalThis.fetch = async (_url, init = {}) => {
    callCount += 1;
    if (callCount === 1) {
      return new Response(
        createStream(
          [
            "event: stream_gap\n" +
              'data: {"reason":"cursor_not_found","requires_gap_reconcile":true}\n\n',
          ],
          init.signal,
        ),
        { status: 200 },
      );
    }

    return new Response(
      createStream(["id: 77\nevent: lifecycle\ndata: {\"ok\":true}\n\n"], init.signal, { keepOpen: true }),
      { status: 200 },
    );
  };

  const client = createSSEClient({
    url: "https://example.test/events",
    heartbeatTimeoutMs: 1000,
    onEvent: (event) => received.push(event),
    onRecovery: () => {
      recoveries += 1;
    },
  });

  client.start();

  await waitFor(() => client.getDiagnostics().connectionState === "failed");

  client.attemptRecovery();

  await waitFor(() => recoveries === 1);
  await waitFor(() => received.length === 1);

  const diagnostics = client.getDiagnostics();
  assert.equal(diagnostics.connectionState, "connected");
  assert.equal(diagnostics.failoverTriggered, false);
  assert.equal(diagnostics.failoverReason, null);
  assert.equal(diagnostics.lastEventId, "77");

  client.stop();
});

test("fails over after reconnect exhaustion", async () => {
  Math.random = () => 0;
  globalThis.fetch = async () => {
    throw new Error("network down");
  };

  const client = createSSEClient({
    url: "https://example.test/events",
    retryMs: 5,
    maxReconnectAttempts: 1,
  });

  client.start();

  await waitFor(() => client.getDiagnostics().connectionState === "failed");
  assert.equal(client.getDiagnostics().failoverReason, "reconnect_exhausted");
});

function createStream(chunks, signal, options = {}) {
  const { keepOpen = false } = options;

  return new ReadableStream({
    start(controller) {
      if (signal?.aborted) {
        controller.close();
        return;
      }

      const close = () => {
        try {
          controller.close();
        } catch {}
      };

      signal?.addEventListener("abort", close, { once: true });

      if (chunks.length === 0) {
        if (!keepOpen) {
          close();
        }
        return;
      }

      let index = 0;
      const push = () => {
        if (index >= chunks.length) {
          if (!keepOpen) {
            close();
          }
          return;
        }

        controller.enqueue(encoder.encode(chunks[index]));
        index += 1;
        setTimeout(push, 0);
      };

      push();
    },
  });
}

async function waitFor(predicate, timeoutMs = 500) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) {
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
  throw new Error("condition not met before timeout");
}
