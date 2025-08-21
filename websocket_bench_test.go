package router_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/goliatone/go-router"
)

// Performance Benchmark Suite for WebSocket Implementation
// Task 6.5: Performance benchmarks and analysis

// Benchmark: Message Write Performance
func BenchmarkWebSocketWriteMessage(b *testing.B) {
	ctx := newMockWebSocketContext()
	ctx.mockUpgrade()

	data := []byte("Hello, WebSocket!")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx.WriteMessage(TextMessage, data)
	}
}

// Benchmark: Message Read Performance
func BenchmarkWebSocketReadMessage(b *testing.B) {
	ctx := newMockWebSocketContext()
	ctx.mockUpgrade()

	// Pre-populate messages
	for i := 0; i < b.N; i++ {
		ctx.WriteMessage(TextMessage, []byte("test message"))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx.ReadMessage()
	}
}

// Benchmark: JSON Write Performance
func BenchmarkWebSocketWriteJSON(b *testing.B) {
	ctx := newMockWebSocketContext()
	ctx.mockUpgrade()

	data := map[string]interface{}{
		"id":        123,
		"message":   "test message",
		"timestamp": time.Now(),
		"data": map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx.WriteJSON(data)
	}
}

// Benchmark: JSON Read Performance
func BenchmarkWebSocketReadJSON(b *testing.B) {
	ctx := newMockWebSocketContext()
	ctx.mockUpgrade()

	// Pre-populate JSON messages
	testData := map[string]string{"test": "data"}
	jsonData, _ := json.Marshal(testData)
	for i := 0; i < b.N; i++ {
		ctx.WriteMessage(TextMessage, jsonData)
	}

	var result map[string]string

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx.ReadJSON(&result)
	}
}

// Benchmark: Concurrent Write Performance
func BenchmarkWebSocketConcurrentWrites(b *testing.B) {
	ctx := newMockWebSocketContext()
	ctx.mockUpgrade()

	data := []byte("concurrent message")

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx.WriteMessage(TextMessage, data)
		}
	})
}

// Benchmark: Large Message Performance
func BenchmarkWebSocketLargeMessage(b *testing.B) {
	sizes := []int{
		1024,    // 1KB
		10240,   // 10KB
		102400,  // 100KB
		1048576, // 1MB
	}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			ctx := newMockWebSocketContext()
			ctx.mockUpgrade()

			data := bytes.Repeat([]byte("x"), size)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				ctx.WriteMessage(BinaryMessage, data)
			}
		})
	}
}

// Benchmark: Connection Pool Operations
func BenchmarkConnectionPool(b *testing.B) {
	b.Run("Add", func(b *testing.B) {
		pool := NewConnectionPool(10000)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx := newMockWebSocketContext()
			ctx.mockUpgrade()
			pool.Add(ctx)
		}
	})

	b.Run("Get", func(b *testing.B) {
		pool := NewConnectionPool(1000)

		// Pre-populate pool
		contexts := make([]WebSocketContext, 100)
		for i := 0; i < 100; i++ {
			ctx := newMockWebSocketContext()
			ctx.mockUpgrade()
			contexts[i] = ctx
			pool.Add(ctx)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			pool.Get(contexts[i%100].ConnectionID())
		}
	})

	b.Run("Broadcast", func(b *testing.B) {
		pool := NewConnectionPool(100)

		// Pre-populate pool
		for i := 0; i < 100; i++ {
			ctx := newMockWebSocketContext()
			ctx.mockUpgrade()
			pool.Add(ctx)
		}

		data := []byte("broadcast message")

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			pool.Broadcast(TextMessage, data)
		}
	})
}

// Benchmark: JSON Message Router
func BenchmarkJSONMessageRouter(b *testing.B) {
	router := NewJSONMessageRouter(1024 * 1024)

	// Register handlers
	router.Register("test", func(ctx WebSocketContext, msg *JSONMessage) error {
		return nil
	})

	router.Register("echo", func(ctx WebSocketContext, msg *JSONMessage) error {
		return ctx.WriteJSON(msg)
	})

	ctx := newMockWebSocketContext()
	ctx.mockUpgrade()

	// Prepare test message
	testMsg := JSONMessage{
		Type:      "test",
		ID:        "123",
		Timestamp: time.Now(),
		Data:      json.RawMessage(`{"key":"value"}`),
	}

	msgData, _ := json.Marshal(testMsg)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx.WriteMessage(TextMessage, msgData)
		router.Route(ctx)
	}
}

// Benchmark: Deadline Manager
func BenchmarkDeadlineManager(b *testing.B) {
	config := WebSocketConfig{
		PingPeriod:   100 * time.Millisecond,
		PongWait:     200 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	}

	ctx := newMockWebSocketContext()
	ctx.mockUpgrade()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		manager := NewDeadlineManager(ctx, config)
		manager.Start()
		manager.HealthCheck()
		manager.Stop()
	}
}

// Benchmark: Compression Manager
func BenchmarkCompressionManager(b *testing.B) {
	config := DefaultCompressionConfig()
	manager := NewCompressionManager(config)

	testData := []struct {
		name string
		size int
	}{
		{"Small", 100},
		{"Medium", 1024},
		{"Large", 10240},
	}

	for _, td := range testData {
		b.Run(td.name, func(b *testing.B) {
			data := bytes.Repeat([]byte("a"), td.size)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				manager.ShouldCompress(data)
			}
		})
	}
}

// Benchmark: Subprotocol Negotiation
func BenchmarkSubprotocolNegotiation(b *testing.B) {
	negotiator := NewSubprotocolNegotiator()

	// Register multiple protocols
	protocols := []string{"chat", "echo", "api", "stream", "control"}
	for _, p := range protocols {
		negotiator.Register(SubprotocolHandler{
			Name:    p,
			Version: "1.0",
		})
	}

	requested := []string{"unknown", "chat", "echo", "api"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		negotiator.NegotiateProtocol(requested)
	}
}

// Benchmark: Error Creation
func BenchmarkWebSocketError(b *testing.B) {
	cause := fmt.Errorf("underlying error")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := &WebSocketError{
			Code:    CloseProtocolError,
			Message: "protocol error",
			Cause:   cause,
		}
		_ = err.Error()
	}
}

// Benchmark: Origin Validation
func BenchmarkOriginValidation(b *testing.B) {
	allowed := []string{
		"https://example.com",
		"https://app.example.com",
		"https://api.example.com",
		"http://localhost:3000",
	}

	origins := []string{
		"https://example.com",
		"https://evil.com",
		"https://app.example.com",
		"http://localhost:3000",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		origin := origins[i%len(origins)]
		validateTestOrigin(origin, allowed)
	}
}

// Benchmark: Memory Allocation for Different Message Sizes
func BenchmarkMemoryAllocation(b *testing.B) {
	sizes := []int{64, 256, 1024, 4096, 16384}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			ctx := newMockWebSocketContext()
			ctx.mockUpgrade()

			data := make([]byte, size)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				ctx.WriteMessage(BinaryMessage, data)
				ctx.ReadMessage()
			}
		})
	}
}

// Benchmark: Concurrent Connection Handling
func BenchmarkConcurrentConnections(b *testing.B) {
	pool := NewConnectionPool(1000)

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx := newMockWebSocketContext()
			ctx.mockUpgrade()

			pool.Add(ctx)
			pool.Get(ctx.ConnectionID())
			pool.Remove(ctx.ConnectionID())
		}
	})
}

// Benchmark: Message Type Conversion
func BenchmarkMessageTypeOperations(b *testing.B) {
	messageTypes := []int{
		TextMessage,
		BinaryMessage,
		CloseMessage,
		PingMessage,
		PongMessage,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msgType := messageTypes[i%len(messageTypes)]

		// Simulate type checking operations
		switch msgType {
		case TextMessage:
			_ = "text"
		case BinaryMessage:
			_ = "binary"
		case CloseMessage:
			_ = "close"
		case PingMessage:
			_ = "ping"
		case PongMessage:
			_ = "pong"
		}
	}
}

// Benchmark: Config Validation
func BenchmarkConfigValidation(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		config := WebSocketConfig{
			ReadBufferSize:   1024,
			WriteBufferSize:  1024,
			HandshakeTimeout: 10 * time.Second,
			PingPeriod:       30 * time.Second,
			PongWait:         60 * time.Second,
			Origins:          []string{"https://example.com"},
			Subprotocols:     []string{"chat", "echo"},
		}

		// Simulate validation
		if config.ReadBufferSize <= 0 {
			config.ReadBufferSize = 1024
		}
		if config.WriteBufferSize <= 0 {
			config.WriteBufferSize = 1024
		}
		if config.HandshakeTimeout <= 0 {
			config.HandshakeTimeout = 10 * time.Second
		}
	}
}

// Benchmark: Broadcasting to Multiple Connections
func BenchmarkBroadcastJSON(b *testing.B) {
	numConnections := []int{10, 50, 100, 500}

	for _, n := range numConnections {
		b.Run(fmt.Sprintf("Connections_%d", n), func(b *testing.B) {
			// Create connections
			connections := make([]WebSocketContext, n)
			for i := 0; i < n; i++ {
				ctx := newMockWebSocketContext()
				ctx.mockUpgrade()
				connections[i] = ctx
			}

			testData := map[string]interface{}{
				"type":    "broadcast",
				"message": "test",
				"time":    time.Now(),
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				BroadcastJSON(connections, testData, 1024*1024)
			}
		})
	}
}

// Benchmark: Factory Pattern Performance
func BenchmarkFactoryPattern(b *testing.B) {
	factory := &mockWebSocketFactory{}
	ctx := newMockWebSocketContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		factory.CanUpgrade(ctx)
		factory.CreateWebSocketContext(ctx)
	}
}

// Benchmark: Ping/Pong Operations
func BenchmarkPingPong(b *testing.B) {
	ctx := newMockWebSocketContext()
	ctx.mockUpgrade()

	pingData := []byte("ping")
	pongData := []byte("pong")

	b.Run("WritePing", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx.WritePing(pingData)
		}
	})

	b.Run("WritePong", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx.WritePong(pongData)
		}
	})
}

// Benchmark: Close Operations
func BenchmarkCloseOperations(b *testing.B) {
	b.Run("NormalClose", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx := newMockWebSocketContext()
			ctx.mockUpgrade()
			ctx.Close()
		}
	})

	b.Run("CloseWithStatus", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx := newMockWebSocketContext()
			ctx.mockUpgrade()
			ctx.CloseWithStatus(CloseNormalClosure, "goodbye")
		}
	})
}

// Benchmark: Connection State Checks
func BenchmarkConnectionState(b *testing.B) {
	ctx := newMockWebSocketContext()
	ctx.mockUpgrade()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx.IsConnected()
		ctx.IsWebSocket()
		ctx.ConnectionID()
	}
}

// Benchmark: Handler Chain Execution
func BenchmarkHandlerChain(b *testing.B) {
	handlers := []func(WebSocketContext) error{
		func(ctx WebSocketContext) error { return nil },
		func(ctx WebSocketContext) error { return nil },
		func(ctx WebSocketContext) error { return nil },
	}

	ctx := newMockWebSocketContext()
	ctx.mockUpgrade()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, handler := range handlers {
			handler(ctx)
		}
	}
}

// Performance Analysis Helper
type PerformanceMetrics struct {
	MessagesSent     int64
	MessagesReceived int64
	BytesSent        int64
	BytesReceived    int64
	Connections      int64
	Errors           int64
	Duration         time.Duration
	mu               sync.RWMutex
}

func (m *PerformanceMetrics) RecordSent(bytes int) {
	m.mu.Lock()
	m.MessagesSent++
	m.BytesSent += int64(bytes)
	m.mu.Unlock()
}

func (m *PerformanceMetrics) RecordReceived(bytes int) {
	m.mu.Lock()
	m.MessagesReceived++
	m.BytesReceived += int64(bytes)
	m.mu.Unlock()
}

func (m *PerformanceMetrics) Report() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	messagesPerSecond := float64(m.MessagesSent) / m.Duration.Seconds()
	bytesPerSecond := float64(m.BytesSent) / m.Duration.Seconds()

	return fmt.Sprintf(
		"Performance Report:\n"+
			"  Duration: %v\n"+
			"  Messages Sent: %d (%.2f/sec)\n"+
			"  Messages Received: %d\n"+
			"  Bytes Sent: %d (%.2f/sec)\n"+
			"  Bytes Received: %d\n"+
			"  Connections: %d\n"+
			"  Errors: %d\n",
		m.Duration,
		m.MessagesSent, messagesPerSecond,
		m.MessagesReceived,
		m.BytesSent, bytesPerSecond,
		m.BytesReceived,
		m.Connections,
		m.Errors,
	)
}

// Benchmark: End-to-End Performance
func BenchmarkEndToEndPerformance(b *testing.B) {
	metrics := &PerformanceMetrics{
		Duration: time.Duration(b.N) * time.Second,
	}

	pool := NewConnectionPool(100)

	// Simulate realistic WebSocket usage
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Create connection
		ctx := newMockWebSocketContext()
		ctx.mockUpgrade()
		pool.Add(ctx)
		metrics.Connections++

		// Send message
		msg := []byte(fmt.Sprintf("Message %d", i))
		if err := ctx.WriteMessage(TextMessage, msg); err == nil {
			metrics.RecordSent(len(msg))
		} else {
			metrics.Errors++
		}

		// Read message
		if _, data, err := ctx.ReadMessage(); err == nil {
			metrics.RecordReceived(len(data))
		} else {
			metrics.Errors++
		}

		// Occasional broadcast
		if i%10 == 0 {
			pool.Broadcast(TextMessage, []byte("broadcast"))
		}

		// Cleanup old connections
		if i%100 == 0 {
			pool.Remove(ctx.ConnectionID())
		}
	}

	b.Logf("\n%s", metrics.Report())
}
