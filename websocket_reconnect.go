package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ReconnectManager manages WebSocket reconnection logic
type ReconnectManager struct {
	// Client state tracking
	sessions   map[string]*ClientSession
	sessionsMu sync.RWMutex

	// Configuration
	config ReconnectConfig

	// Message queue manager
	queueManager *MessageQueueManager
}

// ReconnectConfig contains reconnection configuration
type ReconnectConfig struct {
	// Enable reconnection support
	Enabled bool

	// Session timeout (how long to keep session after disconnect)
	SessionTimeout time.Duration

	// Maximum reconnection attempts
	MaxReconnectAttempts int

	// Reconnection backoff settings
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64

	// Message queue settings
	EnableMessageQueue bool
	MaxQueueSize       int
	QueueTTL           time.Duration

	// Heartbeat settings
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
}

// ClientSession represents a client's session state
type ClientSession struct {
	ID             string
	ClientID       string
	State          map[string]any
	LastSeen       time.Time
	DisconnectTime time.Time
	ReconnectToken string
	Subscriptions  []string // Room/channel subscriptions
	MessageQueue   *MessageQueue

	mu sync.RWMutex
}

// MessageQueue stores messages for offline clients
type MessageQueue struct {
	messages []*QueuedMessage
	maxSize  int
	ttl      time.Duration
	mu       sync.RWMutex
}

// QueuedMessage represents a queued message
type QueuedMessage struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Data      any            `json:"data"`
	Metadata  map[string]any `json:"metadata"`
	Timestamp time.Time      `json:"timestamp"`
	Attempts  int            `json:"attempts"`
}

// MessageQueueManager manages message queues for all clients
type MessageQueueManager struct {
	queues   map[string]*MessageQueue
	queuesMu sync.RWMutex
	config   MessageQueueConfig

	// Persistence backend (optional)
	storage MessageQueueStorage
}

// MessageQueueConfig contains message queue configuration
type MessageQueueConfig struct {
	MaxQueueSize        int
	MessageTTL          time.Duration
	MaxDeliveryAttempts int
	PersistQueues       bool
}

// MessageQueueStorage interface for persistent queue storage
type MessageQueueStorage interface {
	Store(ctx context.Context, clientID string, messages []*QueuedMessage) error
	Retrieve(ctx context.Context, clientID string) ([]*QueuedMessage, error)
	Delete(ctx context.Context, clientID string) error
}

// DefaultReconnectConfig returns default reconnection configuration
func DefaultReconnectConfig() ReconnectConfig {
	return ReconnectConfig{
		Enabled:              true,
		SessionTimeout:       5 * time.Minute,
		MaxReconnectAttempts: 5,
		InitialBackoff:       1 * time.Second,
		MaxBackoff:           30 * time.Second,
		BackoffFactor:        2.0,
		EnableMessageQueue:   true,
		MaxQueueSize:         100,
		QueueTTL:             5 * time.Minute,
		HeartbeatInterval:    30 * time.Second,
		HeartbeatTimeout:     60 * time.Second,
	}
}

// NewReconnectManager creates a new reconnection manager
func NewReconnectManager(config ReconnectConfig) *ReconnectManager {
	if config.SessionTimeout == 0 {
		config.SessionTimeout = 5 * time.Minute
	}

	mgr := &ReconnectManager{
		sessions: make(map[string]*ClientSession),
		config:   config,
	}

	if config.EnableMessageQueue {
		mgr.queueManager = NewMessageQueueManager(MessageQueueConfig{
			MaxQueueSize:        config.MaxQueueSize,
			MessageTTL:          config.QueueTTL,
			MaxDeliveryAttempts: 3,
		})
	}

	// Start cleanup routine
	go mgr.cleanupSessions()

	return mgr
}

// CreateSession creates a new session for a client
func (m *ReconnectManager) CreateSession(client WSClient) (*ClientSession, error) {
	session := &ClientSession{
		ID:             generateID(),
		ClientID:       client.ID(),
		State:          make(map[string]any),
		LastSeen:       time.Now(),
		ReconnectToken: generateSecureToken(),
		Subscriptions:  make([]string, 0),
	}

	if m.config.EnableMessageQueue {
		session.MessageQueue = NewMessageQueue(m.config.MaxQueueSize, m.config.QueueTTL)
	}

	m.sessionsMu.Lock()
	m.sessions[session.ID] = session
	m.sessionsMu.Unlock()

	// Send session info to client
	client.SendJSON(map[string]any{
		"type":               "session_created",
		"session_id":         session.ID,
		"reconnect_token":    session.ReconnectToken,
		"heartbeat_interval": m.config.HeartbeatInterval.Seconds(),
	})

	return session, nil
}

// HandleReconnect handles client reconnection
func (m *ReconnectManager) HandleReconnect(ctx context.Context, client WSClient, sessionID, token string) (*ClientSession, error) {
	m.sessionsMu.RLock()
	session, exists := m.sessions[sessionID]
	m.sessionsMu.RUnlock()

	if !exists {
		return nil, errors.New("session not found")
	}

	// Validate token
	if session.ReconnectToken != token {
		return nil, errors.New("invalid reconnect token")
	}

	// Check if session is expired
	if time.Since(session.DisconnectTime) > m.config.SessionTimeout {
		m.RemoveSession(sessionID)
		return nil, errors.New("session expired")
	}

	// Update session
	session.mu.Lock()
	session.LastSeen = time.Now()
	session.DisconnectTime = time.Time{}
	session.mu.Unlock()

	// Restore subscriptions
	for _, sub := range session.Subscriptions {
		if err := client.JoinWithContext(ctx, sub); err != nil {
			// Log error but continue
			fmt.Printf("Failed to restore subscription %s: %v\n", sub, err)
		}
	}

	// Deliver queued messages
	if session.MessageQueue != nil {
		messages := session.MessageQueue.Flush()
		for _, msg := range messages {
			if err := client.SendJSON(msg); err != nil {
				// Re-queue if delivery fails
				session.MessageQueue.Add(msg)
				break
			}
		}
	}

	// Send reconnect success
	client.SendJSON(map[string]any{
		"type":            "reconnect_success",
		"session_id":      session.ID,
		"queued_messages": session.MessageQueue.Size(),
		"subscriptions":   session.Subscriptions,
	})

	return session, nil
}

// HandleDisconnect handles client disconnection
func (m *ReconnectManager) HandleDisconnect(sessionID string) {
	m.sessionsMu.RLock()
	session, exists := m.sessions[sessionID]
	m.sessionsMu.RUnlock()

	if !exists {
		return
	}

	session.mu.Lock()
	session.DisconnectTime = time.Now()
	session.mu.Unlock()
}

// QueueMessage queues a message for a disconnected client
func (m *ReconnectManager) QueueMessage(sessionID string, message *QueuedMessage) error {
	m.sessionsMu.RLock()
	session, exists := m.sessions[sessionID]
	m.sessionsMu.RUnlock()

	if !exists {
		return errors.New("session not found")
	}

	if session.MessageQueue == nil {
		return errors.New("message queue not enabled")
	}

	return session.MessageQueue.Add(message)
}

// RemoveSession removes a session
func (m *ReconnectManager) RemoveSession(sessionID string) {
	m.sessionsMu.Lock()
	defer m.sessionsMu.Unlock()

	delete(m.sessions, sessionID)
}

// cleanupSessions periodically cleans up expired sessions
func (m *ReconnectManager) cleanupSessions() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.sessionsMu.Lock()
		now := time.Now()

		for id, session := range m.sessions {
			// Check if session is expired
			if !session.DisconnectTime.IsZero() &&
				now.Sub(session.DisconnectTime) > m.config.SessionTimeout {
				delete(m.sessions, id)
			}
		}

		m.sessionsMu.Unlock()
	}
}

// MessageQueue implementation

// NewMessageQueue creates a new message queue
func NewMessageQueue(maxSize int, ttl time.Duration) *MessageQueue {
	return &MessageQueue{
		messages: make([]*QueuedMessage, 0, maxSize),
		maxSize:  maxSize,
		ttl:      ttl,
	}
}

// Add adds a message to the queue
func (q *MessageQueue) Add(msg *QueuedMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check size limit
	if len(q.messages) >= q.maxSize {
		// Remove oldest message
		q.messages = q.messages[1:]
	}

	msg.Timestamp = time.Now()
	q.messages = append(q.messages, msg)

	return nil
}

// Flush returns all messages and clears the queue
func (q *MessageQueue) Flush() []*QueuedMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Filter out expired messages
	now := time.Now()
	valid := make([]*QueuedMessage, 0, len(q.messages))

	for _, msg := range q.messages {
		if now.Sub(msg.Timestamp) <= q.ttl {
			valid = append(valid, msg)
		}
	}

	q.messages = q.messages[:0]
	return valid
}

// Size returns the number of messages in the queue
func (q *MessageQueue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.messages)
}

// MessageQueueManager implementation

// NewMessageQueueManager creates a new message queue manager
func NewMessageQueueManager(config MessageQueueConfig) *MessageQueueManager {
	return &MessageQueueManager{
		queues: make(map[string]*MessageQueue),
		config: config,
	}
}

// GetQueue returns or creates a queue for a client
func (m *MessageQueueManager) GetQueue(clientID string) *MessageQueue {
	m.queuesMu.Lock()
	defer m.queuesMu.Unlock()

	queue, exists := m.queues[clientID]
	if !exists {
		queue = NewMessageQueue(m.config.MaxQueueSize, m.config.MessageTTL)
		m.queues[clientID] = queue
	}

	return queue
}

// RemoveQueue removes a client's queue
func (m *MessageQueueManager) RemoveQueue(clientID string) {
	m.queuesMu.Lock()
	defer m.queuesMu.Unlock()

	delete(m.queues, clientID)
}

// Heartbeat management

// HeartbeatManager manages client heartbeats
type HeartbeatManager struct {
	clients   map[string]*heartbeatClient
	clientsMu sync.RWMutex

	interval time.Duration
	timeout  time.Duration

	onTimeout func(clientID string)
}

type heartbeatClient struct {
	lastPing time.Time
	lastPong time.Time
	timer    *time.Timer
}

// NewHeartbeatManager creates a new heartbeat manager
func NewHeartbeatManager(interval, timeout time.Duration, onTimeout func(string)) *HeartbeatManager {
	return &HeartbeatManager{
		clients:   make(map[string]*heartbeatClient),
		interval:  interval,
		timeout:   timeout,
		onTimeout: onTimeout,
	}
}

// StartHeartbeat starts heartbeat monitoring for a client
func (h *HeartbeatManager) StartHeartbeat(client WSClient) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	hb := &heartbeatClient{
		lastPing: time.Now(),
	}

	// Send initial ping
	client.Send([]byte{PingMessage})

	// Start timeout timer
	hb.timer = time.AfterFunc(h.timeout, func() {
		h.handleTimeout(client.ID())
	})

	h.clients[client.ID()] = hb

	// Start ping ticker
	go h.sendPings(client)
}

// HandlePong handles a pong message from a client
func (h *HeartbeatManager) HandlePong(clientID string) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	hb, exists := h.clients[clientID]
	if !exists {
		return
	}

	hb.lastPong = time.Now()

	// Reset timeout timer
	if hb.timer != nil {
		hb.timer.Stop()
	}
	hb.timer = time.AfterFunc(h.timeout, func() {
		h.handleTimeout(clientID)
	})
}

// StopHeartbeat stops heartbeat monitoring for a client
func (h *HeartbeatManager) StopHeartbeat(clientID string) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	hb, exists := h.clients[clientID]
	if exists {
		if hb.timer != nil {
			hb.timer.Stop()
		}
		delete(h.clients, clientID)
	}
}

func (h *HeartbeatManager) sendPings(client WSClient) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for range ticker.C {
		h.clientsMu.RLock()
		_, exists := h.clients[client.ID()]
		h.clientsMu.RUnlock()

		if !exists {
			return
		}

		// Send ping
		if err := client.Send([]byte{PingMessage}); err != nil {
			return
		}
	}
}

func (h *HeartbeatManager) handleTimeout(clientID string) {
	h.clientsMu.Lock()
	delete(h.clients, clientID)
	h.clientsMu.Unlock()

	if h.onTimeout != nil {
		h.onTimeout(clientID)
	}
}

// Compression support

// CompressMessage compresses a message if it meets criteria
func CompressMessage(data []byte, config CompressionConfig) ([]byte, bool, error) {
	if !config.Enabled || len(data) < config.Threshold {
		return data, false, nil
	}

	// Use standard gzip compression (simplified)
	// In production, you'd use compress/gzip or similar
	compressed := data // Placeholder

	// Only use compression if it reduces size
	if len(compressed) < len(data) {
		return compressed, true, nil
	}

	return data, false, nil
}

// DecompressMessage decompresses a message
func DecompressMessage(data []byte) ([]byte, error) {
	// Use standard gzip decompression (simplified)
	// In production, you'd use compress/gzip or similar
	return data, nil
}

// Helper to generate secure reconnect token
func generateSecureToken() string {
	return generateID() + "-" + generateID()
}
