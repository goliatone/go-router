# GoRouter WebSocket Client Library

A comprehensive TypeScript WebSocket client library with authentication, reconnection, and message queuing capabilities.

## Features

- 🔒 **Authentication** - JWT token-based authentication with automatic retry
- 🔄 **Auto Reconnection** - Smart reconnection with exponential backoff
- 📦 **Message Queuing** - Queue messages when disconnected, send when reconnected
- 💓 **Heartbeat/Ping-Pong** - Keep connections alive with configurable heartbeat
- 🎯 **Command Pattern** - Send commands and wait for specific responses
- 📊 **Connection Metrics** - Track connection uptime, latency, message counts
- 🎭 **Event System** - Rich event system with TypeScript support
- ⚡ **Lightweight** - Only ~10KB minified
- 🌐 **Universal** - Works in browsers, Node.js, and React Native

## Installation

### From NPM (when published)
```bash
npm install @goliatone/websocket-client
```

### CDN Usage
```html
<script src="/client/client.min.js"></script>
```

### Local Development
```bash
# Install dependencies
npm install

# Build all versions
npm run build

# Development build (with source maps)
npm run build:dev

# Production build (minified)
npm run build:prod

# Generate TypeScript definitions
npm run build:types

# Watch mode for development
npm run watch
```

## Quick Start

```typescript
import WebSocketClient from '@goliatone/websocket-client';

// Create client with authentication
const client = new WebSocketClient('ws://localhost:3000/ws', {
    token: 'your-jwt-token-here',
    autoReconnect: true,
    debug: true
});

// Listen to events
client.on('connected', (data) => {
    console.log('Connected!', data);
});

client.on('auth_success', (data) => {
    console.log('Authenticated as:', data.username);
});

client.on('message', (message) => {
    console.log('Received:', message);
});

// Connect
await client.connect();

// Send messages
await client.send({
    type: 'chat_message',
    text: 'Hello, World!'
});

// Send commands and wait for response
try {
    const users = await client.sendCommand('get_users');
    console.log('Online users:', users);
} catch (error) {
    console.error('Command failed:', error);
}
```

## Configuration Options

```typescript
interface WebSocketClientOptions {
    // Connection settings
    autoReconnect?: boolean;        // Default: true
    maxReconnectAttempts?: number;  // Default: 5
    reconnectDelay?: number;        // Default: 1000ms
    maxReconnectDelay?: number;     // Default: 30000ms
    reconnectDecay?: number;        // Default: 1.5

    // Authentication
    token?: string | null;          // JWT token
    tokenRefreshCallback?: () => Promise<string>; // Token refresh function

    // Heartbeat/keepalive
    heartbeatInterval?: number;     // Default: 30000ms
    heartbeatTimeout?: number;      // Default: 5000ms

    // Message handling
    queueMessages?: boolean;        // Default: true
    maxQueueSize?: number;          // Default: 100

    // Debugging
    debug?: boolean;                // Default: false
    logLevel?: 'debug' | 'info' | 'warn' | 'error'; // Default: 'info'
}
```

## Events

### Connection Events
- `connected` - WebSocket connection established
- `disconnected` - WebSocket connection closed
- `reconnecting` - Attempting to reconnect
- `reconnectFailed` - All reconnection attempts failed
- `error` - Connection or protocol error
- `stateChange` - Connection state changed

### Authentication Events
- `auth_success` - Authentication successful
- `auth_failed` - Authentication failed

### Message Events
- `message` - Generic message received
- `[message_type]` - Specific message type events
- `heartbeatTimeout` - Heartbeat timeout (connection may be dead)

## API Reference

### Methods

#### `connect(): Promise<void>`
Connect to the WebSocket server.

#### `disconnect(): void`
Disconnect from the server.

#### `reconnect(): void`
Force reconnection (resets retry count).

#### `send(message: object): Promise<void>`
Send a message to the server.

#### `sendCommand(command: string, data?: object, timeout?: number): Promise<any>`
Send a command and wait for response.

#### `isConnected(): boolean`
Check if connected to server.

#### `isAuthenticated(): boolean`
Check if authenticated.

#### `getConnectionState(): ConnectionState`
Get current connection state.

#### `getMetrics(): ConnectionMetrics`
Get connection statistics.

#### `getUserInfo(): UserInfo | null`
Get authenticated user information.

### Connection States

- `disconnected` - Not connected
- `connecting` - Establishing connection
- `connected` - Connected but not authenticated
- `authenticating` - Sending authentication
- `authenticated` - Fully connected and authenticated
- `reconnecting` - Attempting to reconnect
- `closing` - Connection closing
- `closed` - Connection closed

## Build System

This library uses **esbuild** for ultra-fast compilation:

- **Source**: TypeScript with full type definitions
- **Output**: ES5-compatible JavaScript (IIFE format)
- **Minification**: Aggressive minification for production
- **Source Maps**: Available for debugging
- **Type Definitions**: Auto-generated from TypeScript source

### Build Outputs

- `client.js` - Development build with source maps (18.3KB)
- `client.min.js` - Production build, minified (10.4KB)
- `client.d.ts` - TypeScript definitions
- `client.js.map` - Source map for debugging

### Development Workflow

1. Edit TypeScript source in `src/client.ts`
2. Run `npm run build` or `npm run watch`
3. Built files are automatically embedded in Go binary
4. Test with your WebSocket server

## License

MIT License - see LICENSE file for details.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes in `src/client.ts`
4. Run `npm run build` to ensure it compiles
5. Test with the WebSocket server examples
6. Submit a pull request

## Changelog

### v1.0.0
- Initial TypeScript implementation
- Full type safety and IntelliSense support
- esbuild-based build system
- Comprehensive event system
- Authentication with JWT tokens
- Smart reconnection with exponential backoff
- Message queuing when disconnected
- Heartbeat/ping-pong for connection health
- Connection metrics and monitoring
- Universal compatibility (Browser/Node.js/React Native)
