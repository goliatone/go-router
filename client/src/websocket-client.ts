/**
 * GoRouter WebSocket Client Library
 * A comprehensive WebSocket client library with authentication, reconnection, and message queuing
 * 
 * @version 1.0.0
 * @author GoRouter Team
 * @license MIT
 */

// Type definitions
export interface WebSocketClientOptions {
    // Connection settings
    autoReconnect?: boolean;
    maxReconnectAttempts?: number;
    reconnectDelay?: number;
    maxReconnectDelay?: number;
    reconnectDecay?: number;
    
    // Authentication
    token?: string | null;
    tokenRefreshCallback?: (() => Promise<string>) | null;
    
    // Heartbeat/keepalive
    heartbeatInterval?: number;
    heartbeatTimeout?: number;
    
    // Message handling
    queueMessages?: boolean;
    maxQueueSize?: number;
    
    // Debugging
    debug?: boolean;
    logLevel?: 'debug' | 'info' | 'warn' | 'error';
}

export interface UserInfo {
    userId: string;
    username: string;
    role: string;
}

export interface ConnectionMetrics {
    connectTime: number | null;
    reconnectCount: number;
    messagessent: number;
    messagesReceived: number;
    lastError: Error | null;
    uptime: number;
    latency: number | null;
}

export interface WebSocketMessage {
    type: string;
    id?: string;
    [key: string]: any;
}

export interface PendingCommand {
    resolve: (value: any) => void;
    reject: (reason: any) => void;
    timeout: number;
    command: string;
}

export type ConnectionState = 
    | 'disconnected'
    | 'connecting' 
    | 'connected'
    | 'reconnecting'
    | 'authenticating'
    | 'authenticated'
    | 'closing'
    | 'closed';

export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

// Constants
export const CONNECTION_STATES: Record<string, ConnectionState> = {
    DISCONNECTED: 'disconnected',
    CONNECTING: 'connecting',
    CONNECTED: 'connected',
    RECONNECTING: 'reconnecting',
    AUTHENTICATING: 'authenticating',
    AUTHENTICATED: 'authenticated',
    CLOSING: 'closing',
    CLOSED: 'closed'
} as const;

export const MESSAGE_TYPES = {
    // Authentication
    AUTH: 'auth',
    AUTH_SUCCESS: 'auth_success',
    AUTH_ERROR: 'auth_error',
    AUTH_REQUIRED: 'auth_required',
    
    // Chat & Communication
    CHAT_MESSAGE: 'chat_message',
    ADMIN_COMMAND: 'admin_command',
    
    // System messages
    GET_USERS: 'get_users',
    USERS_LIST: 'users_list',
    USER_JOINED: 'user_joined',
    USER_LEFT: 'user_left',
    
    // Control messages
    PING: 'ping',
    PONG: 'pong',
    
    // Notifications
    ADMIN_ANNOUNCEMENT: 'admin_announcement',
    WELCOME: 'welcome',
    ERROR: 'error'
} as const;

const DEFAULT_OPTIONS: Required<WebSocketClientOptions> = {
    // Connection settings
    autoReconnect: true,
    maxReconnectAttempts: 5,
    reconnectDelay: 1000,
    maxReconnectDelay: 30000,
    reconnectDecay: 1.5,
    
    // Authentication
    token: null,
    tokenRefreshCallback: null,
    
    // Heartbeat/keepalive
    heartbeatInterval: 30000,
    heartbeatTimeout: 5000,
    
    // Message handling
    queueMessages: true,
    maxQueueSize: 100,
    
    // Debugging
    debug: false,
    logLevel: 'info'
};

/**
 * Simple EventEmitter implementation with proper typing
 */
export class EventEmitter<TEventMap extends Record<string, any> = Record<string, any>> {
    private _events: Partial<Record<keyof TEventMap, Array<(...args: any[]) => void>>> = {};

    on<K extends keyof TEventMap>(event: K, listener: (...args: TEventMap[K]) => void): this {
        if (!this._events[event]) {
            this._events[event] = [];
        }
        this._events[event]!.push(listener);
        return this;
    }

    once<K extends keyof TEventMap>(event: K, listener: (...args: TEventMap[K]) => void): this {
        const onceWrapper = (...args: TEventMap[K]) => {
            this.off(event, onceWrapper);
            listener(...args);
        };
        return this.on(event, onceWrapper);
    }

    off<K extends keyof TEventMap>(event: K, listener?: (...args: TEventMap[K]) => void): this {
        if (!this._events[event]) return this;
        
        if (!listener) {
            delete this._events[event];
            return this;
        }
        
        const listeners = this._events[event]!;
        const index = listeners.indexOf(listener);
        if (index !== -1) {
            listeners.splice(index, 1);
        }
        return this;
    }

    emit<K extends keyof TEventMap>(event: K, ...args: any[]): boolean {
        if (!this._events[event]) return false;
        
        const listeners = this._events[event]!.slice();
        listeners.forEach(listener => {
            try {
                listener(...args);
            } catch (error) {
                console.error(`Error in event listener for '${String(event)}':`, error);
            }
        });
        return true;
    }

    listenerCount<K extends keyof TEventMap>(event: K): number {
        return this._events[event] ? this._events[event]!.length : 0;
    }
}

// Event map for WebSocketClient
export interface WebSocketClientEventMap {
    connected: [{ url: string; reconnectCount: number }];
    disconnected: [{ code: number; reason: string; wasAuthenticated: boolean }];
    error: [Error];
    stateChange: [{ oldState: ConnectionState; newState: ConnectionState }];
    message: [WebSocketMessage];
    auth_success: [any];
    auth_failed: [Error];
    reconnecting: [{ attempt: number; maxAttempts: number; delay: number }];
    reconnectFailed: [{ attempts: number; lastError: Error | null }];
    heartbeatTimeout: [];
    [key: string]: any[];
}

/**
 * WebSocket Client with comprehensive features and full TypeScript support
 */
export class WebSocketClient extends EventEmitter<WebSocketClientEventMap> {
    // Static properties for constants
    static CONNECTION_STATES: typeof CONNECTION_STATES;
    static MESSAGE_TYPES: typeof MESSAGE_TYPES;
    static EventEmitter: typeof EventEmitter;
    
    public readonly url: string;
    public readonly options: Required<WebSocketClientOptions>;
    
    // Connection state
    public state: ConnectionState = CONNECTION_STATES.DISCONNECTED;
    public ws: WebSocket | null = null;
    public lastError: Error | null = null;
    
    // Authentication state
    public authenticated: boolean = false;
    public userInfo: UserInfo | null = null;
    
    // Reconnection state
    public reconnectAttempts: number = 0;
    private reconnectTimer: number | null = null;
    private shouldReconnect: boolean = false;
    
    // Message handling
    private messageQueue: WebSocketMessage[] = [];
    private pendingCommands = new Map<string, PendingCommand>();
    private commandId: number = 0;
    
    // Heartbeat
    private heartbeatTimer: number | null = null;
    private heartbeatTimeoutTimer: number | null = null;
    private lastPongTime: number | null = null;
    private heartbeatSentTime: number = 0;
    
    // Connection metrics
    public metrics: Omit<ConnectionMetrics, 'uptime' | 'latency'> = {
        connectTime: null,
        reconnectCount: 0,
        messagessent: 0,
        messagesReceived: 0,
        lastError: null
    };

    /**
     * Create a WebSocket client
     * @param url - WebSocket server URL
     * @param options - Configuration options
     */
    constructor(url: string, options: WebSocketClientOptions = {}) {
        super();
        
        this.url = url;
        this.options = { ...DEFAULT_OPTIONS, ...options };
        
        // Bind methods to preserve `this` context
        this._onOpen = this._onOpen.bind(this);
        this._onClose = this._onClose.bind(this);
        this._onError = this._onError.bind(this);
        this._onMessage = this._onMessage.bind(this);
    }

    /**
     * Connect to the WebSocket server
     */
    async connect(): Promise<void> {
        return new Promise((resolve, reject) => {
            if (this.state === CONNECTION_STATES.CONNECTING || this.state === CONNECTION_STATES.CONNECTED) {
                resolve();
                return;
            }

            this._log('debug', 'Initiating connection...');
            this._setState(CONNECTION_STATES.CONNECTING);
            this.shouldReconnect = true;

            // Clear any existing connection
            this._cleanup();

            // Build WebSocket URL with token if provided
            let wsUrl = this.url;
            if (this.options.token) {
                const separator = this.url.includes('?') ? '&' : '?';
                wsUrl = `${this.url}${separator}token=${encodeURIComponent(this.options.token)}`;
            }

            try {
                this.ws = new WebSocket(wsUrl);
                this.ws.onopen = this._onOpen;
                this.ws.onclose = this._onClose;
                this.ws.onerror = this._onError;
                this.ws.onmessage = this._onMessage;

                // Set up connection timeout
                const connectTimeout = setTimeout(() => {
                    if (this.state === CONNECTION_STATES.CONNECTING) {
                        this._log('error', 'Connection timeout');
                        this.disconnect();
                        reject(new Error('Connection timeout'));
                    }
                }, 10000);

                // Wait for successful connection
                this.once('connected', () => {
                    clearTimeout(connectTimeout);
                    resolve();
                });

                this.once('error', (error) => {
                    clearTimeout(connectTimeout);
                    reject(error);
                });

            } catch (error) {
                this._log('error', 'Failed to create WebSocket connection:', error);
                this._setState(CONNECTION_STATES.DISCONNECTED);
                reject(error);
            }
        });
    }

    /**
     * Disconnect from the WebSocket server
     */
    disconnect(): void {
        this._log('debug', 'Disconnecting...');
        this.shouldReconnect = false;
        this._setState(CONNECTION_STATES.CLOSING);
        
        this._stopHeartbeat();
        this._clearReconnectTimer();
        
        if (this.ws && this.ws.readyState === WebSocket.OPEN) {
            this.ws.close(1000, 'Client disconnect');
        } else {
            this._cleanup();
            this._setState(CONNECTION_STATES.DISCONNECTED);
        }
    }

    /**
     * Reconnect to the WebSocket server
     */
    reconnect(): void {
        this._log('debug', 'Manual reconnect requested');
        this.reconnectAttempts = 0;
        this.disconnect();
        setTimeout(() => this.connect(), 100);
    }

    /**
     * Send a message to the server
     */
    async send(message: WebSocketMessage): Promise<void> {
        return new Promise((resolve, reject) => {
            if (!this.isConnected()) {
                if (this.options.queueMessages) {
                    this._queueMessage(message);
                    this._log('debug', 'Message queued (not connected)', message);
                    resolve();
                } else {
                    reject(new Error('Not connected'));
                }
                return;
            }

            try {
                const messageStr = JSON.stringify(message);
                this.ws!.send(messageStr);
                this.metrics.messagessent++;
                this._log('debug', 'Message sent:', message);
                resolve();
            } catch (error) {
                this._log('error', 'Failed to send message:', error);
                reject(error);
            }
        });
    }

    /**
     * Send a command and wait for a response
     */
    async sendCommand(command: string, data: Record<string, any> = {}, timeout: number = 5000): Promise<any> {
        return new Promise((resolve, reject) => {
            const commandId = this._generateCommandId();
            const message: WebSocketMessage = {
                type: command,
                id: commandId,
                ...data
            };

            // Set up timeout
            const timeoutTimer = setTimeout(() => {
                this.pendingCommands.delete(commandId);
                reject(new Error(`Command timeout: ${command}`));
            }, timeout);

            // Store command callback
            this.pendingCommands.set(commandId, {
                resolve,
                reject,
                timeout: timeoutTimer,
                command
            });

            // Send command
            this.send(message).catch(error => {
                clearTimeout(timeoutTimer);
                this.pendingCommands.delete(commandId);
                reject(error);
            });
        });
    }

    /**
     * Check if the client is connected
     */
    isConnected(): boolean {
        return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
    }

    /**
     * Check if the client is authenticated
     */
    isAuthenticated(): boolean {
        return this.authenticated && this.userInfo !== null;
    }

    /**
     * Get current connection state
     */
    getConnectionState(): ConnectionState {
        return this.state;
    }

    /**
     * Get number of queued messages
     */
    getQueuedMessageCount(): number {
        return this.messageQueue.length;
    }

    /**
     * Get connection metrics
     */
    getMetrics(): ConnectionMetrics {
        return {
            ...this.metrics,
            uptime: this.metrics.connectTime ? Date.now() - this.metrics.connectTime : 0,
            latency: this.lastPongTime ? this.lastPongTime - this.heartbeatSentTime : null
        };
    }

    /**
     * Get user information
     */
    getUserInfo(): UserInfo | null {
        return this.userInfo;
    }

    // Private methods

    private _setState(newState: ConnectionState): void {
        if (this.state !== newState) {
            const oldState = this.state;
            this.state = newState;
            this._log('debug', `State changed: ${oldState} -> ${newState}`);
            this.emit('stateChange', { oldState, newState });
        }
    }

    private _onOpen(): void {
        this._log('info', 'WebSocket connected');
        this._setState(CONNECTION_STATES.CONNECTED);
        this.metrics.connectTime = Date.now();
        this.metrics.reconnectCount = this.reconnectAttempts;
        this.reconnectAttempts = 0;
        this.lastError = null;
        
        this._startHeartbeat();
        this._processMessageQueue();
        
        this.emit('connected', {
            url: this.url,
            reconnectCount: this.metrics.reconnectCount
        });
    }

    private _onClose(event: CloseEvent): void {
        this._log('info', `WebSocket closed: ${event.code} ${event.reason}`);
        this._cleanup();
        this._setState(CONNECTION_STATES.DISCONNECTED);
        this.authenticated = false;
        this.userInfo = null;
        
        this.emit('disconnected', {
            code: event.code,
            reason: event.reason,
            wasAuthenticated: this.authenticated
        });

        if (this.shouldReconnect && this.options.autoReconnect) {
            this._scheduleReconnect();
        }
    }

    private _onError(error: Event): void {
        this._log('error', 'WebSocket error:', error);
        const errorObj = new Error('WebSocket error');
        this.lastError = errorObj;
        this.metrics.lastError = errorObj;
        this.emit('error', errorObj);
    }

    private _onMessage(event: MessageEvent): void {
        this.metrics.messagesReceived++;
        
        try {
            const data: WebSocketMessage = JSON.parse(event.data);
            this._log('debug', 'Message received:', data);
            this._handleMessage(data);
        } catch (error) {
            this._log('error', 'Failed to parse message:', error);
            this.emit('error', new Error('Invalid message format'));
        }
    }

    private _handleMessage(data: WebSocketMessage): void {
        const { type, id } = data;

        // Handle command responses
        if (id && this.pendingCommands.has(id)) {
            const command = this.pendingCommands.get(id)!;
            clearTimeout(command.timeout);
            this.pendingCommands.delete(id);
            
            if (type === MESSAGE_TYPES.ERROR) {
                command.reject(new Error(data.message || 'Command failed'));
            } else {
                command.resolve(data);
            }
            return;
        }

        // Handle specific message types
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
                this.emit('error', new Error(data.message || 'Server error'));
                break;
                
            default:
                // Emit generic message event
                this.emit('message', data);
                
                // Emit specific message type events
                this.emit(type, data);
                break;
        }
    }

    private _handleAuthSuccess(data: any): void {
        this._log('info', 'Authentication successful:', data);
        this.authenticated = true;
        this.userInfo = {
            userId: data.user_id,
            username: data.username,
            role: data.role
        };
        this._setState(CONNECTION_STATES.AUTHENTICATED);
        this.emit('auth_success', data);
    }

    private _handleAuthError(data: any): void {
        this._log('error', 'Authentication failed:', data);
        this.authenticated = false;
        this.userInfo = null;
        this.emit('auth_failed', new Error(data.message || 'Authentication failed'));
    }

    private _handleAuthRequired(data: any): void {
        this._log('debug', 'Authentication required:', data);
        this._setState(CONNECTION_STATES.AUTHENTICATING);
        
        if (this.options.token) {
            // Send authentication message
            this.send({
                type: MESSAGE_TYPES.AUTH,
                token: this.options.token
            });
        } else {
            this.emit('auth_failed', new Error('No authentication token provided'));
        }
    }

    private _handlePong(): void {
        this.lastPongTime = Date.now();
        this._log('debug', 'Pong received');
        
        if (this.heartbeatTimeoutTimer) {
            clearTimeout(this.heartbeatTimeoutTimer);
            this.heartbeatTimeoutTimer = null;
        }
    }

    private _startHeartbeat(): void {
        if (this.options.heartbeatInterval <= 0) return;
        
        this._stopHeartbeat();
        
        this.heartbeatTimer = setInterval(() => {
            if (this.isConnected()) {
                this.heartbeatSentTime = Date.now();
                this.send({ type: MESSAGE_TYPES.PING });
                
                // Set timeout for pong response
                this.heartbeatTimeoutTimer = setTimeout(() => {
                    this._log('warn', 'Heartbeat timeout - connection may be dead');
                    this.emit('heartbeatTimeout', []);
                    
                    if (this.options.autoReconnect) {
                        this.reconnect();
                    }
                }, this.options.heartbeatTimeout);
            }
        }, this.options.heartbeatInterval);
    }

    private _stopHeartbeat(): void {
        if (this.heartbeatTimer) {
            clearInterval(this.heartbeatTimer);
            this.heartbeatTimer = null;
        }
        
        if (this.heartbeatTimeoutTimer) {
            clearTimeout(this.heartbeatTimeoutTimer);
            this.heartbeatTimeoutTimer = null;
        }
    }

    private _queueMessage(message: WebSocketMessage): void {
        if (this.messageQueue.length >= this.options.maxQueueSize) {
            this.messageQueue.shift(); // Remove oldest message
        }
        this.messageQueue.push(message);
    }

    private _processMessageQueue(): void {
        if (this.messageQueue.length === 0) return;
        
        this._log('debug', `Processing ${this.messageQueue.length} queued messages`);
        
        const queue = this.messageQueue.slice();
        this.messageQueue = [];
        
        queue.forEach(message => {
            this.send(message).catch(error => {
                this._log('error', 'Failed to send queued message:', error);
            });
        });
    }

    private _scheduleReconnect(): void {
        if (this.reconnectAttempts >= this.options.maxReconnectAttempts) {
            this._log('error', 'Max reconnection attempts reached');
            this.emit('reconnectFailed', {
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

        this._log('info', `Scheduling reconnect attempt ${this.reconnectAttempts}/${this.options.maxReconnectAttempts} in ${delay}ms`);
        this._setState(CONNECTION_STATES.RECONNECTING);
        
        this.emit('reconnecting', {
            attempt: this.reconnectAttempts,
            maxAttempts: this.options.maxReconnectAttempts,
            delay
        });

        this._clearReconnectTimer();
        this.reconnectTimer = setTimeout(() => {
            if (this.shouldReconnect) {
                this.connect().catch(error => {
                    this._log('error', 'Reconnection failed:', error);
                    this._scheduleReconnect();
                });
            }
        }, delay);
    }

    private _clearReconnectTimer(): void {
        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = null;
        }
    }

    private _cleanup(): void {
        this._stopHeartbeat();
        
        if (this.ws) {
            this.ws.onopen = null;
            this.ws.onclose = null;
            this.ws.onerror = null;
            this.ws.onmessage = null;
            this.ws = null;
        }
        
        // Clear pending commands
        this.pendingCommands.forEach(command => {
            clearTimeout(command.timeout);
            command.reject(new Error('Connection closed'));
        });
        this.pendingCommands.clear();
    }

    private _generateCommandId(): string {
        return `cmd_${++this.commandId}_${Date.now()}`;
    }

    private _log(level: LogLevel, ...args: any[]): void {
        if (!this.options.debug) return;
        
        const levels: Record<LogLevel, number> = { debug: 0, info: 1, warn: 2, error: 3 };
        const currentLevel = levels[this.options.logLevel] || 1;
        const messageLevel = levels[level] || 1;
        
        if (messageLevel >= currentLevel) {
            console[level](`[WebSocketClient:${level.toUpperCase()}]`, ...args);
        }
    }
}

// Add static properties to the constructor
WebSocketClient.CONNECTION_STATES = CONNECTION_STATES;
WebSocketClient.MESSAGE_TYPES = MESSAGE_TYPES;
WebSocketClient.EventEmitter = EventEmitter;

// Export the constructor as default for IIFE format
export default WebSocketClient;