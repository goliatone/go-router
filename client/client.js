"use strict";
var __WebSocketClientModule = (() => {
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

  // src/websocket-client.ts
  var websocket_client_exports = {};
  __export(websocket_client_exports, {
    CONNECTION_STATES: () => CONNECTION_STATES,
    EventEmitter: () => EventEmitter,
    MESSAGE_TYPES: () => MESSAGE_TYPES,
    WebSocketClient: () => WebSocketClient,
    default: () => websocket_client_default
  });
  var CONNECTION_STATES = {
    DISCONNECTED: "disconnected",
    CONNECTING: "connecting",
    CONNECTED: "connected",
    RECONNECTING: "reconnecting",
    AUTHENTICATING: "authenticating",
    AUTHENTICATED: "authenticated",
    CLOSING: "closing",
    CLOSED: "closed"
  };
  var MESSAGE_TYPES = {
    // Authentication
    AUTH: "auth",
    AUTH_SUCCESS: "auth_success",
    AUTH_ERROR: "auth_error",
    AUTH_REQUIRED: "auth_required",
    // Chat & Communication
    CHAT_MESSAGE: "chat_message",
    ADMIN_COMMAND: "admin_command",
    // System messages
    GET_USERS: "get_users",
    USERS_LIST: "users_list",
    USER_JOINED: "user_joined",
    USER_LEFT: "user_left",
    // Control messages
    PING: "ping",
    PONG: "pong",
    // Notifications
    ADMIN_ANNOUNCEMENT: "admin_announcement",
    WELCOME: "welcome",
    ERROR: "error",
    // Custom message types
    CUSTOM: "custom"
  };
  var DEFAULT_OPTIONS = {
    // Connection settings
    autoReconnect: true,
    maxReconnectAttempts: 5,
    reconnectDelay: 1e3,
    maxReconnectDelay: 3e4,
    reconnectDecay: 1.5,
    // Authentication
    token: null,
    tokenRefreshCallback: null,
    // Heartbeat/keepalive
    heartbeatInterval: 3e4,
    heartbeatTimeout: 5e3,
    // Message handling
    queueMessages: true,
    maxQueueSize: 100,
    // Debugging
    debug: false,
    logLevel: "info"
  };
  var EventEmitter = class {
    constructor() {
      this._events = {};
    }
    on(event, listener) {
      if (!this._events[event]) {
        this._events[event] = [];
      }
      this._events[event].push(listener);
      return this;
    }
    once(event, listener) {
      const onceWrapper = (...args) => {
        this.off(event, onceWrapper);
        listener(...args);
      };
      return this.on(event, onceWrapper);
    }
    off(event, listener) {
      if (!this._events[event])
        return this;
      if (!listener) {
        delete this._events[event];
        return this;
      }
      const listeners = this._events[event];
      const index = listeners.indexOf(listener);
      if (index !== -1) {
        listeners.splice(index, 1);
      }
      return this;
    }
    emit(event, ...args) {
      if (!this._events[event])
        return false;
      const listeners = this._events[event].slice();
      listeners.forEach((listener) => {
        try {
          listener(...args);
        } catch (error) {
          console.error(`Error in event listener for '${String(event)}':`, error);
        }
      });
      return true;
    }
    listenerCount(event) {
      return this._events[event] ? this._events[event].length : 0;
    }
  };
  var WebSocketClient = class extends EventEmitter {
    /**
     * Create a WebSocket client
     * @param url - WebSocket server URL
     * @param options - Configuration options
     */
    constructor(url, options = {}) {
      super();
      // Connection state
      this.state = CONNECTION_STATES.DISCONNECTED;
      this.ws = null;
      this.lastError = null;
      // Authentication state
      this.authenticated = false;
      this.userInfo = null;
      // Reconnection state
      this.reconnectAttempts = 0;
      this.reconnectTimer = null;
      this.shouldReconnect = false;
      // Message handling
      this.messageQueue = [];
      this.pendingCommands = /* @__PURE__ */ new Map();
      this.commandId = 0;
      // Heartbeat
      this.heartbeatTimer = null;
      this.heartbeatTimeoutTimer = null;
      this.lastPongTime = null;
      this.heartbeatSentTime = 0;
      // Connection metrics
      this.metrics = {
        connectTime: null,
        reconnectCount: 0,
        messagessent: 0,
        messagesReceived: 0,
        lastError: null
      };
      this.url = url;
      this.options = { ...DEFAULT_OPTIONS, ...options };
      this._onOpen = this._onOpen.bind(this);
      this._onClose = this._onClose.bind(this);
      this._onError = this._onError.bind(this);
      this._onMessage = this._onMessage.bind(this);
    }
    /**
     * Connect to the WebSocket server
     */
    async connect() {
      return new Promise((resolve, reject) => {
        if (this.state === CONNECTION_STATES.CONNECTING || this.state === CONNECTION_STATES.CONNECTED) {
          resolve();
          return;
        }
        this._log("debug", "Initiating connection...");
        this._setState(CONNECTION_STATES.CONNECTING);
        this.shouldReconnect = true;
        this._cleanup();
        let wsUrl = this.url;
        if (this.options.token) {
          const separator = this.url.includes("?") ? "&" : "?";
          wsUrl = `${this.url}${separator}token=${encodeURIComponent(this.options.token)}`;
        }
        try {
          this.ws = new WebSocket(wsUrl);
          this.ws.onopen = this._onOpen;
          this.ws.onclose = this._onClose;
          this.ws.onerror = this._onError;
          this.ws.onmessage = this._onMessage;
          const connectTimeout = setTimeout(() => {
            if (this.state === CONNECTION_STATES.CONNECTING) {
              this._log("error", "Connection timeout");
              this.disconnect();
              reject(new Error("Connection timeout"));
            }
          }, 1e4);
          this.once("connected", () => {
            clearTimeout(connectTimeout);
            resolve();
          });
          this.once("error", (error) => {
            clearTimeout(connectTimeout);
            reject(error);
          });
        } catch (error) {
          this._log("error", "Failed to create WebSocket connection:", error);
          this._setState(CONNECTION_STATES.DISCONNECTED);
          reject(error);
        }
      });
    }
    /**
     * Disconnect from the WebSocket server
     */
    disconnect() {
      this._log("debug", "Disconnecting...");
      this.shouldReconnect = false;
      this._setState(CONNECTION_STATES.CLOSING);
      this._stopHeartbeat();
      this._clearReconnectTimer();
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.close(1e3, "Client disconnect");
      } else {
        this._cleanup();
        this._setState(CONNECTION_STATES.DISCONNECTED);
      }
    }
    /**
     * Reconnect to the WebSocket server
     */
    reconnect() {
      this._log("debug", "Manual reconnect requested");
      this.reconnectAttempts = 0;
      this.disconnect();
      setTimeout(() => this.connect(), 100);
    }
    /**
     * Send a message to the server
     */
    async send(message) {
      return new Promise((resolve, reject) => {
        if (!this.isConnected()) {
          if (this.options.queueMessages) {
            this._queueMessage(message);
            this._log("debug", "Message queued (not connected)", message);
            resolve();
          } else {
            reject(new Error("Not connected"));
          }
          return;
        }
        try {
          const messageStr = JSON.stringify(message);
          this.ws.send(messageStr);
          this.metrics.messagessent++;
          this._log("debug", "Message sent:", message);
          resolve();
        } catch (error) {
          this._log("error", "Failed to send message:", error);
          reject(error);
        }
      });
    }
    /**
     * Send a command and wait for a response
     */
    async sendCommand(command, data = {}, timeout = 5e3) {
      return new Promise((resolve, reject) => {
        const commandId = this._generateCommandId();
        const message = {
          type: command,
          id: commandId,
          ...data
        };
        const timeoutTimer = setTimeout(() => {
          this.pendingCommands.delete(commandId);
          reject(new Error(`Command timeout: ${command}`));
        }, timeout);
        this.pendingCommands.set(commandId, {
          resolve,
          reject,
          timeout: timeoutTimer,
          command
        });
        this.send(message).catch((error) => {
          clearTimeout(timeoutTimer);
          this.pendingCommands.delete(commandId);
          reject(error);
        });
      });
    }
    /**
     * Check if the client is connected
     */
    isConnected() {
      return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
    }
    /**
     * Check if the client is authenticated
     */
    isAuthenticated() {
      return this.authenticated && this.userInfo !== null;
    }
    /**
     * Get current connection state
     */
    getConnectionState() {
      return this.state;
    }
    /**
     * Get number of queued messages
     */
    getQueuedMessageCount() {
      return this.messageQueue.length;
    }
    /**
     * Get connection metrics
     */
    getMetrics() {
      return {
        ...this.metrics,
        uptime: this.metrics.connectTime ? Date.now() - this.metrics.connectTime : 0,
        latency: this.lastPongTime ? this.lastPongTime - this.heartbeatSentTime : null
      };
    }
    /**
     * Get user information
     */
    getUserInfo() {
      return this.userInfo;
    }
    // Private methods
    _setState(newState) {
      if (this.state !== newState) {
        const oldState = this.state;
        this.state = newState;
        this._log("debug", `State changed: ${oldState} -> ${newState}`);
        this.emit("stateChange", { oldState, newState });
      }
    }
    _onOpen() {
      this._log("info", "WebSocket connected");
      this._setState(CONNECTION_STATES.CONNECTED);
      this.metrics.connectTime = Date.now();
      this.metrics.reconnectCount = this.reconnectAttempts;
      this.reconnectAttempts = 0;
      this.lastError = null;
      this._startHeartbeat();
      this._processMessageQueue();
      this.emit("connected", {
        url: this.url,
        reconnectCount: this.metrics.reconnectCount
      });
    }
    _onClose(event) {
      this._log("info", `WebSocket closed: ${event.code} ${event.reason}`);
      this._cleanup();
      this._setState(CONNECTION_STATES.DISCONNECTED);
      this.authenticated = false;
      this.userInfo = null;
      this.emit("disconnected", {
        code: event.code,
        reason: event.reason,
        wasAuthenticated: this.authenticated
      });
      if (this.shouldReconnect && this.options.autoReconnect) {
        this._scheduleReconnect();
      }
    }
    _onError(error) {
      this._log("error", "WebSocket error:", error);
      const errorObj = new Error("WebSocket error");
      this.lastError = errorObj;
      this.metrics.lastError = errorObj;
      this.emit("error", errorObj);
    }
    _onMessage(event) {
      this.metrics.messagesReceived++;
      try {
        const data = JSON.parse(event.data);
        this._log("debug", "Message received:", data);
        this._handleMessage(data);
      } catch (error) {
        this._log("error", "Failed to parse message:", error);
        this.emit("error", new Error("Invalid message format"));
      }
    }
    _handleMessage(data) {
      const { type, id } = data;
      if (id && this.pendingCommands.has(id)) {
        const command = this.pendingCommands.get(id);
        clearTimeout(command.timeout);
        this.pendingCommands.delete(id);
        if (type === MESSAGE_TYPES.ERROR) {
          command.reject(new Error(data.message || "Command failed"));
        } else {
          command.resolve(data);
        }
        return;
      }
      switch (type) {
        case MESSAGE_TYPES.AUTH_SUCCESS:
          this._handleAuthSuccess(data);
          break;
        case MESSAGE_TYPES.AUTH_ERROR:
          this._handleAuthError(data);
          break;
        case MESSAGE_TYPES.AUTH_REQUIRED:
          this._handleAuthRequired(data);
          break;
        case MESSAGE_TYPES.PONG:
          this._handlePong();
          break;
        case MESSAGE_TYPES.ERROR:
          this.emit("error", new Error(data.message || "Server error"));
          break;
        default:
          this.emit("message", data);
          this.emit(type, data);
          break;
      }
    }
    _handleAuthSuccess(data) {
      this._log("info", "Authentication successful:", data);
      this.authenticated = true;
      this.userInfo = {
        userId: data.user_id,
        username: data.username,
        role: data.role
      };
      this._setState(CONNECTION_STATES.AUTHENTICATED);
      this.emit("auth_success", data);
    }
    _handleAuthError(data) {
      this._log("error", "Authentication failed:", data);
      this.authenticated = false;
      this.userInfo = null;
      this.emit("auth_failed", new Error(data.message || "Authentication failed"));
    }
    _handleAuthRequired(data) {
      this._log("debug", "Authentication required:", data);
      this._setState(CONNECTION_STATES.AUTHENTICATING);
      if (this.options.token) {
        this.send({
          type: MESSAGE_TYPES.AUTH,
          token: this.options.token
        });
      } else {
        this.emit("auth_failed", new Error("No authentication token provided"));
      }
    }
    _handlePong() {
      this.lastPongTime = Date.now();
      this._log("debug", "Pong received");
      if (this.heartbeatTimeoutTimer) {
        clearTimeout(this.heartbeatTimeoutTimer);
        this.heartbeatTimeoutTimer = null;
      }
    }
    _startHeartbeat() {
      if (this.options.heartbeatInterval <= 0)
        return;
      this._stopHeartbeat();
      this.heartbeatTimer = setInterval(() => {
        if (this.isConnected()) {
          this.heartbeatSentTime = Date.now();
          this.send({ type: MESSAGE_TYPES.PING });
          this.heartbeatTimeoutTimer = setTimeout(() => {
            this._log("warn", "Heartbeat timeout - connection may be dead");
            this.emit("heartbeatTimeout", []);
            if (this.options.autoReconnect) {
              this.reconnect();
            }
          }, this.options.heartbeatTimeout);
        }
      }, this.options.heartbeatInterval);
    }
    _stopHeartbeat() {
      if (this.heartbeatTimer) {
        clearInterval(this.heartbeatTimer);
        this.heartbeatTimer = null;
      }
      if (this.heartbeatTimeoutTimer) {
        clearTimeout(this.heartbeatTimeoutTimer);
        this.heartbeatTimeoutTimer = null;
      }
    }
    _queueMessage(message) {
      if (this.messageQueue.length >= this.options.maxQueueSize) {
        this.messageQueue.shift();
      }
      this.messageQueue.push(message);
    }
    _processMessageQueue() {
      if (this.messageQueue.length === 0)
        return;
      this._log("debug", `Processing ${this.messageQueue.length} queued messages`);
      const queue = this.messageQueue.slice();
      this.messageQueue = [];
      queue.forEach((message) => {
        this.send(message).catch((error) => {
          this._log("error", "Failed to send queued message:", error);
        });
      });
    }
    _scheduleReconnect() {
      if (this.reconnectAttempts >= this.options.maxReconnectAttempts) {
        this._log("error", "Max reconnection attempts reached");
        this.emit("reconnectFailed", {
          attempts: this.reconnectAttempts,
          lastError: this.lastError
        });
        return;
      }
      this.reconnectAttempts++;
      const delay = Math.min(
        this.options.reconnectDelay * Math.pow(this.options.reconnectDecay, this.reconnectAttempts - 1),
        this.options.maxReconnectDelay
      );
      this._log("info", `Scheduling reconnect attempt ${this.reconnectAttempts}/${this.options.maxReconnectAttempts} in ${delay}ms`);
      this._setState(CONNECTION_STATES.RECONNECTING);
      this.emit("reconnecting", {
        attempt: this.reconnectAttempts,
        maxAttempts: this.options.maxReconnectAttempts,
        delay
      });
      this._clearReconnectTimer();
      this.reconnectTimer = setTimeout(() => {
        if (this.shouldReconnect) {
          this.connect().catch((error) => {
            this._log("error", "Reconnection failed:", error);
            this._scheduleReconnect();
          });
        }
      }, delay);
    }
    _clearReconnectTimer() {
      if (this.reconnectTimer) {
        clearTimeout(this.reconnectTimer);
        this.reconnectTimer = null;
      }
    }
    _cleanup() {
      this._stopHeartbeat();
      if (this.ws) {
        this.ws.onopen = null;
        this.ws.onclose = null;
        this.ws.onerror = null;
        this.ws.onmessage = null;
        this.ws = null;
      }
      this.pendingCommands.forEach((command) => {
        clearTimeout(command.timeout);
        command.reject(new Error("Connection closed"));
      });
      this.pendingCommands.clear();
    }
    _generateCommandId() {
      return `cmd_${++this.commandId}_${Date.now()}`;
    }
    _log(level, ...args) {
      if (!this.options.debug)
        return;
      const levels = { debug: 0, info: 1, warn: 2, error: 3 };
      const currentLevel = levels[this.options.logLevel] || 1;
      const messageLevel = levels[level] || 1;
      if (messageLevel >= currentLevel) {
        console[level](`[WebSocketClient:${level.toUpperCase()}]`, ...args);
      }
    }
  };
  WebSocketClient.CONNECTION_STATES = CONNECTION_STATES;
  WebSocketClient.MESSAGE_TYPES = MESSAGE_TYPES;
  WebSocketClient.EventEmitter = EventEmitter;
  var websocket_client_default = WebSocketClient;
  return __toCommonJS(websocket_client_exports);
})();
/**
 * GoRouter WebSocket Client Library
 * A comprehensive WebSocket client library with authentication, reconnection, and message queuing
 * 
 * @version 1.0.0
 * @author GoRouter Team
 * @license MIT
 */
var WebSocketClient = __WebSocketClientModule.default || __WebSocketClientModule;
//# sourceMappingURL=client.js.map
