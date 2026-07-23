import assert from 'node:assert/strict';
import test from 'node:test';
import { build } from 'esbuild';

const built = await build({
  entryPoints: [new URL('../src/client.ts', import.meta.url).pathname],
  bundle: true,
  format: 'esm',
  platform: 'browser',
  target: 'es2020',
  write: false,
});
const source = built.outputFiles[0].text;
const clientModule = await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);

function installHarness() {
  const timers = new Map();
  let timerID = 0;
  globalThis.setTimeout = (callback, delay = 0) => {
    timerID += 1;
    timers.set(timerID, { callback, delay });
    return timerID;
  };
  globalThis.clearTimeout = (id) => timers.delete(id);
  globalThis.setInterval = globalThis.setTimeout;
  globalThis.clearInterval = globalThis.clearTimeout;

  const sockets = [];
  class FakeWebSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;

    constructor(url) {
      this.url = url;
      this.readyState = FakeWebSocket.CONNECTING;
      sockets.push(this);
    }

    open() {
      this.readyState = FakeWebSocket.OPEN;
      this.onopen?.(new Event('open'));
    }

    close(code = 1000, reason = '') {
      this.readyState = FakeWebSocket.CLOSED;
      this.onclose?.({ code, reason });
    }

    send() {}
  }
  globalThis.WebSocket = FakeWebSocket;

  const runShortestTimer = () => {
    const entry = [...timers.entries()].sort((a, b) => a[1].delay - b[1].delay)[0];
    assert.ok(entry, 'expected a scheduled timer');
    timers.delete(entry[0]);
    entry[1].callback();
    return entry[1].delay;
  };
  return { sockets, timers, runShortestTimer };
}

test('short-lived successful upgrades consume the reconnect budget', async () => {
  const { sockets, runShortestTimer } = installHarness();
  const client = new clientModule.WebSocketClient('ws://example.test/ws', {
    maxReconnectAttempts: 2,
    reconnectDelay: 10,
    reconnectDecay: 2,
    reconnectStabilityMs: 1000,
    heartbeatInterval: 0,
  });

  const initial = client.connect();
  sockets[0].open();
  await initial;
  sockets[0].close();
  assert.equal(runShortestTimer(), 10);
  sockets[1].open();
  sockets[1].close();
  assert.equal(runShortestTimer(), 20);
  sockets[2].open();
  sockets[2].close();

  assert.equal(sockets.length, 3);
  assert.equal(client.reconnectAttempts, 2);
});

test('a stable connection resets the reconnect budget', async () => {
  const { sockets, timers, runShortestTimer } = installHarness();
  const client = new clientModule.WebSocketClient('ws://example.test/ws', {
    maxReconnectAttempts: 1,
    reconnectDelay: 10,
    reconnectStabilityMs: 50,
    heartbeatInterval: 0,
  });

  const initial = client.connect();
  sockets[0].open();
  await initial;
  sockets[0].close();
  runShortestTimer();
  sockets[1].open();

  const stable = [...timers.entries()].find(([, timer]) => timer.delay === 50);
  assert.ok(stable);
  timers.delete(stable[0]);
  stable[1].callback();
  sockets[1].close();
  runShortestTimer();

  assert.equal(sockets.length, 3);
});
