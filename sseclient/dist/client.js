"use strict";
var __GoRouterSSEClientModule = (() => {
  var __defProp = Object.defineProperty;
  var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
  var __getOwnPropNames = Object.getOwnPropertyNames;
  var __hasOwnProp = Object.prototype.hasOwnProperty;
  var __export = (target, all) => {
    for (var name in all)
      __defProp(target, name, { get: all[name], enumerable: true });
  };
  var __copyProps = (to, from, except, desc) => {
    if (from && typeof from === "object" || typeof from === "function") {
      for (let key of __getOwnPropNames(from))
        if (!__hasOwnProp.call(to, key) && key !== except)
          __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
    }
    return to;
  };
  var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);

  // src/client.ts
  var client_exports = {};
  __export(client_exports, {
    createSSEClient: () => createSSEClient,
    default: () => client_default
  });
  var DEFAULT_RETRY_MS = 3e3;
  var DEFAULT_HEARTBEAT_TIMEOUT_MS = 45e3;
  var DEFAULT_MAX_RECONNECT_ATTEMPTS = 5;
  var MAX_RECONNECT_DELAY_MS = 3e4;
  var RECONNECT_JITTER_RATIO = 0.2;
  var FetchSSEClient = class {
    constructor(options) {
      this.options = options;
      this.running = false;
      this.connectLoop = null;
      this.controller = null;
      this.heartbeatDegradeTimer = null;
      this.heartbeatFailoverTimer = null;
      this.reconnectTimer = null;
      this.reconnectWaiterResolve = null;
      this.reconnectAttempt = 0;
      this.serverRetryMs = null;
      this.recoveryPending = false;
      if (!options.url || options.url.trim() === "") {
        throw new Error("go-router SSE client requires a url");
      }
      this.diagnosticsState = {
        connectionState: "disconnected",
        lastEventId: null,
        lastHeartbeatAt: null,
        lastEventAt: null,
        reconnectAttempts: 0,
        totalEventsReceived: 0,
        gapEventsReceived: 0,
        failoverTriggered: false,
        failoverReason: null,
        streamUrl: options.url
      };
    }
    start() {
      if (this.diagnosticsState.failoverTriggered) {
        return;
      }
      if (this.running) {
        return;
      }
      this.running = true;
      this.ensureConnectLoop();
    }
    stop() {
      var _a;
      this.running = false;
      this.recoveryPending = false;
      this.clearReconnectTimer();
      this.clearHeartbeatTimers();
      (_a = this.controller) == null ? void 0 : _a.abort();
      this.controller = null;
      if (!this.diagnosticsState.failoverTriggered) {
        this.setConnectionState("disconnected");
      }
    }
    isConnected() {
      return this.diagnosticsState.connectionState === "connected";
    }
    getDiagnostics() {
      return { ...this.diagnosticsState };
    }
    triggerFailover(reason) {
      this.enterFailover(reason);
    }
    attemptRecovery() {
      if (!this.diagnosticsState.failoverTriggered) {
        return;
      }
      this.diagnosticsState.failoverTriggered = false;
      this.diagnosticsState.failoverReason = null;
      this.recoveryPending = true;
      this.reconnectAttempt = 0;
      this.diagnosticsState.reconnectAttempts = 0;
      this.running = true;
      if (this.connectLoop) {
        return;
      }
      this.ensureConnectLoop();
    }
    ensureConnectLoop() {
      if (!this.running || this.connectLoop || this.diagnosticsState.failoverTriggered) {
        return;
      }
      this.connectLoop = this.run();
      void this.connectLoop.finally(() => {
        this.connectLoop = null;
        if (this.running && !this.diagnosticsState.failoverTriggered) {
          this.ensureConnectLoop();
          return;
        }
        if (!this.running && !this.diagnosticsState.failoverTriggered) {
          this.setConnectionState("disconnected");
        }
      });
    }
    async run() {
      var _a, _b;
      while (this.running) {
        const isReconnect = this.reconnectAttempt > 0;
        this.setConnectionState(isReconnect ? "reconnecting" : "connecting");
        try {
          const requestURL = this.buildRequestURL(isReconnect);
          this.diagnosticsState.streamUrl = requestURL;
          const headers = await this.resolveHeaders();
          if (!this.running) {
            return;
          }
          this.controller = new AbortController();
          const response = await fetch(requestURL, {
            method: "GET",
            headers,
            signal: this.controller.signal
          });
          if (response.status === 401 || response.status === 403) {
            this.enterFailover("auth_failed");
            return;
          }
          if (!response.ok) {
            throw new Error(`SSE request failed with status ${response.status}`);
          }
          if (!response.body) {
            throw new Error("SSE response body is not readable");
          }
          this.reconnectAttempt = 0;
          this.diagnosticsState.reconnectAttempts = 0;
          this.setConnectionState("connected");
          this.armHeartbeatTimers();
          if (this.recoveryPending) {
            this.recoveryPending = false;
            (_b = (_a = this.options).onRecovery) == null ? void 0 : _b.call(_a, this.getDiagnostics());
          }
          await this.consume(response.body);
          if (!this.running || this.diagnosticsState.failoverTriggered) {
            return;
          }
          await this.scheduleReconnect();
        } catch (error) {
          if (!this.running) {
            return;
          }
          if (this.diagnosticsState.failoverTriggered) {
            return;
          }
          if (isAbortError(error) && !this.running) {
            return;
          }
          await this.scheduleReconnect();
        } finally {
          this.controller = null;
          this.clearHeartbeatTimers();
        }
      }
    }
    async consume(stream) {
      const reader = stream.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      try {
        while (this.running) {
          const { done, value } = await reader.read();
          if (done) {
            return;
          }
          buffer += decoder.decode(value, { stream: true });
          const frames = splitFrames(buffer);
          buffer = frames.remainder;
          for (const raw of frames.frames) {
            const frame = parseFrame(raw);
            if (frame) {
              this.dispatch(frame);
              if (!this.running || this.diagnosticsState.failoverTriggered) {
                return;
              }
            }
          }
        }
      } finally {
        reader.releaseLock();
      }
    }
    dispatch(frame) {
      if (frame.retry !== null && frame.retry > 0) {
        this.serverRetryMs = frame.retry;
      }
      if (frame.data === "" && frame.id === null && frame.event === "message") {
        return;
      }
      const payload = parseJSON(frame.data);
      switch (frame.event) {
        case "heartbeat":
          this.handleHeartbeat(payload);
          return;
        case "stream_gap":
          this.handleStreamGap(payload);
          return;
        default:
          this.handleDomainEvent({
            id: frame.id,
            name: frame.event || "message",
            payload
          });
      }
    }
    handleDomainEvent(event) {
      var _a, _b;
      if (event.id) {
        this.diagnosticsState.lastEventId = event.id;
      }
      this.diagnosticsState.totalEventsReceived += 1;
      this.diagnosticsState.lastEventAt = (/* @__PURE__ */ new Date()).toISOString();
      (_b = (_a = this.options).onEvent) == null ? void 0 : _b.call(_a, event);
    }
    handleHeartbeat(event) {
      var _a, _b, _c;
      this.diagnosticsState.lastHeartbeatAt = (_a = event.timestamp) != null ? _a : (/* @__PURE__ */ new Date()).toISOString();
      if (this.diagnosticsState.connectionState === "degraded") {
        this.setConnectionState("connected");
      }
      this.armHeartbeatTimers();
      (_c = (_b = this.options).onHeartbeat) == null ? void 0 : _c.call(_b, event);
    }
    handleStreamGap(event) {
      var _a, _b, _c, _d;
      this.diagnosticsState.gapEventsReceived += 1;
      (_b = (_a = this.options).onStreamGap) == null ? void 0 : _b.call(_a, event);
      (_d = (_c = this.options).onRequestSnapshot) == null ? void 0 : _d.call(_c);
      this.enterFailover("stream_gap");
    }
    armHeartbeatTimers() {
      const timeoutMs = this.resolveHeartbeatTimeoutMs();
      if (timeoutMs <= 0) {
        return;
      }
      this.clearHeartbeatTimers();
      this.heartbeatDegradeTimer = setTimeout(() => {
        if (!this.running || this.diagnosticsState.failoverTriggered) {
          return;
        }
        this.setConnectionState("degraded");
        this.heartbeatFailoverTimer = setTimeout(() => {
          if (!this.running || this.diagnosticsState.failoverTriggered) {
            return;
          }
          if (this.diagnosticsState.connectionState === "degraded") {
            this.enterFailover("heartbeat_timeout");
          }
        }, timeoutMs);
      }, timeoutMs);
    }
    clearHeartbeatTimers() {
      if (this.heartbeatDegradeTimer) {
        clearTimeout(this.heartbeatDegradeTimer);
        this.heartbeatDegradeTimer = null;
      }
      if (this.heartbeatFailoverTimer) {
        clearTimeout(this.heartbeatFailoverTimer);
        this.heartbeatFailoverTimer = null;
      }
    }
    async scheduleReconnect() {
      this.reconnectAttempt += 1;
      this.diagnosticsState.reconnectAttempts = this.reconnectAttempt;
      if (this.reconnectAttempt > this.resolveMaxReconnectAttempts()) {
        this.enterFailover("reconnect_exhausted");
        return;
      }
      this.setConnectionState("reconnecting");
      const delay = this.computeReconnectDelay(this.reconnectAttempt);
      await new Promise((resolve) => {
        this.clearReconnectTimer();
        this.reconnectWaiterResolve = () => {
          this.reconnectWaiterResolve = null;
          resolve();
        };
        this.reconnectTimer = setTimeout(() => {
          var _a;
          this.reconnectTimer = null;
          (_a = this.reconnectWaiterResolve) == null ? void 0 : _a.call(this);
        }, delay);
      });
    }
    clearReconnectTimer() {
      if (this.reconnectTimer) {
        clearTimeout(this.reconnectTimer);
        this.reconnectTimer = null;
      }
      if (this.reconnectWaiterResolve) {
        const resolve = this.reconnectWaiterResolve;
        this.reconnectWaiterResolve = null;
        resolve();
      }
    }
    setConnectionState(nextState) {
      var _a, _b;
      if (this.diagnosticsState.connectionState === nextState) {
        return;
      }
      this.diagnosticsState.connectionState = nextState;
      (_b = (_a = this.options).onConnectionStateChange) == null ? void 0 : _b.call(_a, nextState, this.getDiagnostics());
    }
    enterFailover(reason) {
      var _a, _b, _c;
      if (this.diagnosticsState.failoverTriggered) {
        return;
      }
      this.running = false;
      this.diagnosticsState.failoverTriggered = true;
      this.diagnosticsState.failoverReason = reason;
      this.clearReconnectTimer();
      this.clearHeartbeatTimers();
      (_a = this.controller) == null ? void 0 : _a.abort();
      this.controller = null;
      this.setConnectionState("failed");
      (_c = (_b = this.options).onFailover) == null ? void 0 : _c.call(_b, reason, this.getDiagnostics());
    }
    async resolveHeaders() {
      var _a, _b;
      const headers = new Headers();
      headers.set("Accept", "text/event-stream");
      try {
        const provided = await ((_b = (_a = this.options).getHeaders) == null ? void 0 : _b.call(_a));
        appendHeaders(headers, provided);
        return headers;
      } catch (e) {
        this.enterFailover("auth_failed");
        throw new Error("auth_failed");
      }
    }
    buildRequestURL(isReconnect) {
      var _a;
      const base = typeof ((_a = globalThis.location) == null ? void 0 : _a.href) === "string" && globalThis.location.href !== "" ? globalThis.location.href : "http://localhost";
      const url = new URL(this.options.url, base);
      if (isReconnect && this.diagnosticsState.lastEventId) {
        url.searchParams.set("cursor", this.diagnosticsState.lastEventId);
      }
      if (this.options.enableClientTuning) {
        if (typeof this.options.heartbeatMs === "number" && this.options.heartbeatMs > 0) {
          url.searchParams.set("heartbeat_ms", String(this.options.heartbeatMs));
        }
        if (typeof this.options.retryMs === "number" && this.options.retryMs > 0) {
          url.searchParams.set("retry_ms", String(this.options.retryMs));
        }
      }
      return url.toString();
    }
    computeReconnectDelay(attempt) {
      const base = this.resolveRetryMs();
      const withoutJitter = Math.min(base * 2 ** Math.max(0, attempt - 1), MAX_RECONNECT_DELAY_MS);
      const jitter = withoutJitter * RECONNECT_JITTER_RATIO * Math.random();
      return Math.round(withoutJitter + jitter);
    }
    resolveRetryMs() {
      if (typeof this.serverRetryMs === "number" && this.serverRetryMs > 0) {
        return this.serverRetryMs;
      }
      if (typeof this.options.retryMs === "number" && this.options.retryMs > 0) {
        return this.options.retryMs;
      }
      return DEFAULT_RETRY_MS;
    }
    resolveHeartbeatTimeoutMs() {
      if (typeof this.options.heartbeatTimeoutMs === "number" && this.options.heartbeatTimeoutMs > 0) {
        return this.options.heartbeatTimeoutMs;
      }
      if (typeof this.options.heartbeatMs === "number" && this.options.heartbeatMs > 0) {
        return Math.max(this.options.heartbeatMs * 2, DEFAULT_HEARTBEAT_TIMEOUT_MS);
      }
      return DEFAULT_HEARTBEAT_TIMEOUT_MS;
    }
    resolveMaxReconnectAttempts() {
      if (typeof this.options.maxReconnectAttempts === "number" && this.options.maxReconnectAttempts >= 0) {
        return this.options.maxReconnectAttempts;
      }
      return DEFAULT_MAX_RECONNECT_ATTEMPTS;
    }
  };
  function appendHeaders(target, source) {
    if (!source) {
      return;
    }
    if (source instanceof Headers) {
      source.forEach((value, key) => {
        target.set(key, value);
      });
      return;
    }
    if (Array.isArray(source)) {
      for (const [key, value] of source) {
        target.set(key, value);
      }
      return;
    }
    for (const [key, value] of Object.entries(source)) {
      target.set(key, value);
    }
  }
  function splitFrames(input) {
    var _a;
    const normalized = input.replace(/\r\n/g, "\n");
    const parts = normalized.split("\n\n");
    if (parts.length === 1) {
      return { frames: [], remainder: normalized };
    }
    return {
      frames: parts.slice(0, -1),
      remainder: (_a = parts[parts.length - 1]) != null ? _a : ""
    };
  }
  function parseFrame(input) {
    const lines = input.split("\n");
    const dataLines = [];
    let id = null;
    let event = "message";
    let retry = null;
    for (const line of lines) {
      if (line === "" || line.startsWith(":")) {
        continue;
      }
      const separator = line.indexOf(":");
      const field = separator === -1 ? line : line.slice(0, separator);
      const value = separator === -1 ? "" : line.slice(separator + 1).replace(/^ /, "");
      switch (field) {
        case "id":
          id = value;
          break;
        case "event":
          event = value || "message";
          break;
        case "data":
          dataLines.push(value);
          break;
        case "retry": {
          const parsed = Number.parseInt(value, 10);
          retry = Number.isNaN(parsed) ? null : parsed;
          break;
        }
        default:
          break;
      }
    }
    if (dataLines.length === 0 && id === null && retry === null && event === "message") {
      return null;
    }
    return {
      id,
      event,
      data: dataLines.join("\n"),
      retry
    };
  }
  function parseJSON(value) {
    if (value === "") {
      return null;
    }
    try {
      return JSON.parse(value);
    } catch (e) {
      return value;
    }
  }
  function isAbortError(error) {
    return error instanceof Error && error.name === "AbortError";
  }
  function createSSEClient(options) {
    return new FetchSSEClient(options);
  }
  var client_default = createSSEClient;
  return __toCommonJS(client_exports);
})();
var GoRouterSSEClient = __GoRouterSSEClientModule.default || __GoRouterSSEClientModule;
//# sourceMappingURL=client.js.map
