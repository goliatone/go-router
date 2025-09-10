/**
 * GoRouter WebSocket Client Library
 * A comprehensive WebSocket client library with authentication, reconnection, and message queuing
 *
 * @version 1.0.0
 * @author GoRouter Team
 * @license MIT
 */
export interface WebSocketClientOptions {
    autoReconnect?: boolean;
    maxReconnectAttempts?: number;
    reconnectDelay?: number;
    maxReconnectDelay?: number;
    reconnectDecay?: number;
    token?: string | null;
    tokenRefreshCallback?: (() => Promise<string>) | null;
    heartbeatInterval?: number;
    heartbeatTimeout?: number;
    queueMessages?: boolean;
    maxQueueSize?: number;
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
export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'authenticating' | 'authenticated' | 'closing' | 'closed';
export type LogLevel = 'debug' | 'info' | 'warn' | 'error';
export declare const CONNECTION_STATES: Record<string, ConnectionState>;
export declare const MESSAGE_TYPES: {
    readonly AUTH: "auth";
    readonly AUTH_SUCCESS: "auth_success";
    readonly AUTH_ERROR: "auth_error";
    readonly AUTH_REQUIRED: "auth_required";
    readonly CHAT_MESSAGE: "chat_message";
    readonly ADMIN_COMMAND: "admin_command";
    readonly GET_USERS: "get_users";
    readonly USERS_LIST: "users_list";
    readonly USER_JOINED: "user_joined";
    readonly USER_LEFT: "user_left";
    readonly PING: "ping";
    readonly PONG: "pong";
    readonly ADMIN_ANNOUNCEMENT: "admin_announcement";
    readonly WELCOME: "welcome";
    readonly ERROR: "error";
};
/**
 * Simple EventEmitter implementation with proper typing
 */
export declare class EventEmitter<TEventMap extends Record<string, any> = Record<string, any>> {
    private _events;
    on<K extends keyof TEventMap>(event: K, listener: (...args: TEventMap[K]) => void): this;
    once<K extends keyof TEventMap>(event: K, listener: (...args: TEventMap[K]) => void): this;
    off<K extends keyof TEventMap>(event: K, listener?: (...args: TEventMap[K]) => void): this;
    emit<K extends keyof TEventMap>(event: K, ...args: any[]): boolean;
    listenerCount<K extends keyof TEventMap>(event: K): number;
}
export interface WebSocketClientEventMap {
    connected: [{
        url: string;
        reconnectCount: number;
    }];
    disconnected: [{
        code: number;
        reason: string;
        wasAuthenticated: boolean;
    }];
    error: [Error];
    stateChange: [{
        oldState: ConnectionState;
        newState: ConnectionState;
    }];
    message: [WebSocketMessage];
    auth_success: [any];
    auth_failed: [Error];
    reconnecting: [{
        attempt: number;
        maxAttempts: number;
        delay: number;
    }];
    reconnectFailed: [{
        attempts: number;
        lastError: Error | null;
    }];
    heartbeatTimeout: [];
    [key: string]: any[];
}
/**
 * WebSocket Client with comprehensive features and full TypeScript support
 */
export declare class WebSocketClient extends EventEmitter<WebSocketClientEventMap> {
    readonly url: string;
    readonly options: Required<WebSocketClientOptions>;
    state: ConnectionState;
    ws: WebSocket | null;
    lastError: Error | null;
    authenticated: boolean;
    userInfo: UserInfo | null;
    reconnectAttempts: number;
    private reconnectTimer;
    private shouldReconnect;
    private messageQueue;
    private pendingCommands;
    private commandId;
    private heartbeatTimer;
    private heartbeatTimeoutTimer;
    private lastPongTime;
    private heartbeatSentTime;
    metrics: Omit<ConnectionMetrics, 'uptime' | 'latency'>;
    /**
     * Create a WebSocket client
     * @param url - WebSocket server URL
     * @param options - Configuration options
     */
    constructor(url: string, options?: WebSocketClientOptions);
    /**
     * Connect to the WebSocket server
     */
    connect(): Promise<void>;
    /**
     * Disconnect from the WebSocket server
     */
    disconnect(): void;
    /**
     * Reconnect to the WebSocket server
     */
    reconnect(): void;
    /**
     * Send a message to the server
     */
    send(message: WebSocketMessage): Promise<void>;
    /**
     * Send a command and wait for a response
     */
    sendCommand(command: string, data?: Record<string, any>, timeout?: number): Promise<any>;
    /**
     * Check if the client is connected
     */
    isConnected(): boolean;
    /**
     * Check if the client is authenticated
     */
    isAuthenticated(): boolean;
    /**
     * Get current connection state
     */
    getConnectionState(): ConnectionState;
    /**
     * Get number of queued messages
     */
    getQueuedMessageCount(): number;
    /**
     * Get connection metrics
     */
    getMetrics(): ConnectionMetrics;
    /**
     * Get user information
     */
    getUserInfo(): UserInfo | null;
    private _setState;
    private _onOpen;
    private _onClose;
    private _onError;
    private _onMessage;
    private _handleMessage;
    private _handleAuthSuccess;
    private _handleAuthError;
    private _handleAuthRequired;
    private _handlePong;
    private _startHeartbeat;
    private _stopHeartbeat;
    private _queueMessage;
    private _processMessageQueue;
    private _scheduleReconnect;
    private _clearReconnectTimer;
    private _cleanup;
    private _generateCommandId;
    private _log;
}
export default WebSocketClient;
