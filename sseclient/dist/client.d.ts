export type ConnectionState = "disconnected" | "connecting" | "connected" | "reconnecting" | "degraded" | "failed";
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
export interface StreamEvent<T = unknown> {
    id: string | null;
    name: string;
    payload: T;
}
export interface HeartbeatEvent {
    timestamp: string;
    scope_key?: string;
}
export interface StreamGapEvent {
    reason: string;
    last_event_id?: string;
    fallback_transport?: string;
    resume_supported?: boolean;
    requires_gap_reconcile?: boolean;
    timestamp?: string;
}
export interface ClientOptions {
    url: string;
    heartbeatMs?: number;
    retryMs?: number;
    heartbeatTimeoutMs?: number;
    maxReconnectAttempts?: number;
    enableClientTuning?: boolean;
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
export declare function createSSEClient(options: ClientOptions): SSEClient;
export default createSSEClient;
