/**
 * GoRouter WebSocket Client Library TypeScript Definitions
 * 
 * @version 1.0.0
 * @author GoRouter Team
 */

declare module 'websocket-client' {
    // Connection states enum
    export interface ConnectionStates {
        readonly DISCONNECTED: 'disconnected';
        readonly CONNECTING: 'connecting';
        readonly CONNECTED: 'connected';
        readonly RECONNECTING: 'reconnecting';
        readonly AUTHENTICATING: 'authenticating';
        readonly AUTHENTICATED: 'authenticated';
        readonly CLOSING: 'closing';
        readonly CLOSED: 'closed';
    }

    // Message types enum
    export interface MessageTypes {
        readonly AUTH: 'auth';
        readonly AUTH_SUCCESS: 'auth_success';
        readonly AUTH_ERROR: 'auth_error';
        readonly AUTH_REQUIRED: 'auth_required';
        readonly CHAT_MESSAGE: 'chat_message';
        readonly ADMIN_COMMAND: 'admin_command';
        readonly GET_USERS: 'get_users';
        readonly USERS_LIST: 'users_list';
        readonly USER_JOINED: 'user_joined';
        readonly USER_LEFT: 'user_left';
        readonly PING: 'ping';
        readonly PONG: 'pong';
        readonly ADMIN_ANNOUNCEMENT: 'admin_announcement';
        readonly WELCOME: 'welcome';
        readonly ERROR: 'error';
    }

    // Configuration options
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

    // Event listener function types
    export type EventListener<T = any> = (data: T) => void;

    // Connection metrics interface
    export interface ConnectionMetrics {
        connectTime: number | null;
        reconnectCount: number;
        messagessent: number;
        messagesReceived: number;
        lastError: Error | null;
        uptime: number;
        latency: number | null;
    }

    // User information interface
    export interface UserInfo {
        userId: string;
        username: string;
        role: string;
    }

    // State change event data
    export interface StateChangeEvent {
        oldState: string;
        newState: string;
    }

    // Connection event data
    export interface ConnectionEvent {
        url: string;
        reconnectCount: number;
    }

    // Disconnection event data
    export interface DisconnectionEvent {
        code: number;
        reason: string;
        wasAuthenticated: boolean;
    }

    // Reconnection event data
    export interface ReconnectionEvent {
        attempt: number;
        maxAttempts: number;
        delay: number;
    }

    // Reconnect failed event data
    export interface ReconnectFailedEvent {
        attempts: number;
        lastError: Error | null;
    }

    // Generic message interface
    export interface WebSocketMessage {
        type: string;
        id?: string;
        [key: string]: any;
    }

    // Specific message types
    export interface AuthMessage extends WebSocketMessage {
        type: 'auth';
        token: string;
    }

    export interface AuthSuccessMessage extends WebSocketMessage {
        type: 'auth_success';
        message: string;
        user_id: string;
        username: string;
        role: string;
    }

    export interface AuthErrorMessage extends WebSocketMessage {
        type: 'auth_error';
        message: string;
    }

    export interface ChatMessage extends WebSocketMessage {
        type: 'chat_message';
        text?: string;
        user_id?: string;
        username?: string;
        role?: string;
        timestamp?: string;
    }

    export interface AdminCommandMessage extends WebSocketMessage {
        type: 'admin_command';
        command: string;
        [key: string]: any;
    }

    export interface UsersListMessage extends WebSocketMessage {
        type: 'users_list';
        users: UserInfo[];
        count: number;
    }

    export interface UserJoinedMessage extends WebSocketMessage {
        type: 'user_joined';
        user_id: string;
        username: string;
        role: string;
        timestamp: string;
    }

    export interface UserLeftMessage extends WebSocketMessage {
        type: 'user_left';
        user_id: string;
        username: string;
        timestamp: string;
    }

    export interface PingMessage extends WebSocketMessage {
        type: 'ping';
    }

    export interface PongMessage extends WebSocketMessage {
        type: 'pong';
        timestamp?: string;
    }

    export interface ErrorMessage extends WebSocketMessage {
        type: 'error';
        message: string;
    }

    // EventEmitter class
    export class EventEmitter {
        constructor();
        
        on<T = any>(event: string, listener: EventListener<T>): this;
        once<T = any>(event: string, listener: EventListener<T>): this;
        off(event: string, listener?: EventListener): this;
        emit(event: string, ...args: any[]): boolean;
        listenerCount(event: string): number;
    }

    // Main WebSocket client class
    export class WebSocketClient extends EventEmitter {
        // Static constants
        static readonly CONNECTION_STATES: ConnectionStates;
        static readonly MESSAGE_TYPES: MessageTypes;
        static readonly EventEmitter: typeof EventEmitter;

        // Instance properties
        readonly url: string;
        readonly options: Required<WebSocketClientOptions>;
        state: string;
        authenticated: boolean;
        userInfo: UserInfo | null;

        constructor(url: string, options?: WebSocketClientOptions);

        // Connection management
        connect(): Promise<void>;
        disconnect(): void;
        reconnect(): void;

        // Message sending
        send(message: WebSocketMessage): Promise<void>;
        sendCommand<T = any>(command: string, data?: object, timeout?: number): Promise<T>;

        // State queries
        isConnected(): boolean;
        isAuthenticated(): boolean;
        getConnectionState(): string;
        getQueuedMessageCount(): number;
        getMetrics(): ConnectionMetrics;
        getUserInfo(): UserInfo | null;

        // Event listeners (extending EventEmitter)
        // Connection events
        on(event: 'connected', listener: EventListener<ConnectionEvent>): this;
        on(event: 'disconnected', listener: EventListener<DisconnectionEvent>): this;
        on(event: 'reconnecting', listener: EventListener<ReconnectionEvent>): this;
        on(event: 'reconnectFailed', listener: EventListener<ReconnectFailedEvent>): this;
        on(event: 'stateChange', listener: EventListener<StateChangeEvent>): this;
        on(event: 'error', listener: EventListener<Error>): this;
        on(event: 'heartbeatTimeout', listener: EventListener<void>): this;

        // Authentication events
        on(event: 'auth_success', listener: EventListener<AuthSuccessMessage>): this;
        on(event: 'auth_failed', listener: EventListener<Error>): this;

        // Message events
        on(event: 'message', listener: EventListener<WebSocketMessage>): this;
        on(event: 'chat_message', listener: EventListener<ChatMessage>): this;
        on(event: 'admin_command', listener: EventListener<AdminCommandMessage>): this;
        on(event: 'users_list', listener: EventListener<UsersListMessage>): this;
        on(event: 'user_joined', listener: EventListener<UserJoinedMessage>): this;
        on(event: 'user_left', listener: EventListener<UserLeftMessage>): this;
        on(event: 'admin_announcement', listener: EventListener<WebSocketMessage>): this;
        on(event: 'welcome', listener: EventListener<WebSocketMessage>): this;
        on(event: 'ping', listener: EventListener<PingMessage>): this;
        on(event: 'pong', listener: EventListener<PongMessage>): this;

        // Generic event listener
        on(event: string, listener: EventListener): this;

        // Once listeners
        once(event: 'connected', listener: EventListener<ConnectionEvent>): this;
        once(event: 'disconnected', listener: EventListener<DisconnectionEvent>): this;
        once(event: 'reconnecting', listener: EventListener<ReconnectionEvent>): this;
        once(event: 'reconnectFailed', listener: EventListener<ReconnectFailedEvent>): this;
        once(event: 'stateChange', listener: EventListener<StateChangeEvent>): this;
        once(event: 'error', listener: EventListener<Error>): this;
        once(event: 'heartbeatTimeout', listener: EventListener<void>): this;
        once(event: 'auth_success', listener: EventListener<AuthSuccessMessage>): this;
        once(event: 'auth_failed', listener: EventListener<Error>): this;
        once(event: 'message', listener: EventListener<WebSocketMessage>): this;
        once(event: string, listener: EventListener): this;

        // Remove listeners
        off(event: 'connected', listener?: EventListener<ConnectionEvent>): this;
        off(event: 'disconnected', listener?: EventListener<DisconnectionEvent>): this;
        off(event: 'reconnecting', listener?: EventListener<ReconnectionEvent>): this;
        off(event: 'reconnectFailed', listener?: EventListener<ReconnectFailedEvent>): this;
        off(event: 'stateChange', listener?: EventListener<StateChangeEvent>): this;
        off(event: 'error', listener?: EventListener<Error>): this;
        off(event: 'heartbeatTimeout', listener?: EventListener<void>): this;
        off(event: 'auth_success', listener?: EventListener<AuthSuccessMessage>): this;
        off(event: 'auth_failed', listener?: EventListener<Error>): this;
        off(event: 'message', listener?: EventListener<WebSocketMessage>): this;
        off(event: string, listener?: EventListener): this;
    }

    // Default export
    export default WebSocketClient;
}

// Global declaration for browser usage
declare global {
    interface Window {
        WebSocketClient: typeof import('websocket-client').WebSocketClient;
    }
}