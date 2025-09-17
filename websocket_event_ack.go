package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// EventAck represents an event acknowledgment
type EventAck struct {
	ID        string    `json:"id"`
	Success   bool      `json:"success"`
	Data      any       `json:"data,omitempty"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// AckHandler handles acknowledgments for events
type AckHandler func(ctx context.Context, ack *EventAck) error

// EventWithAck wraps an event with acknowledgment support
type EventWithAck struct {
	Event   *EventMessage
	AckChan chan *EventAck
	Timeout time.Duration
}

// AckManager manages event acknowledgments
type AckManager struct {
	pending   map[string]*pendingAck
	pendingMu sync.RWMutex

	defaultTimeout time.Duration
}

type pendingAck struct {
	event    *EventMessage
	ackChan  chan *EventAck
	timer    *time.Timer
	callback AckHandler
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewAckManager creates a new acknowledgment manager
func NewAckManager(defaultTimeout time.Duration) *AckManager {
	if defaultTimeout == 0 {
		defaultTimeout = 30 * time.Second
	}

	return &AckManager{
		pending:        make(map[string]*pendingAck),
		defaultTimeout: defaultTimeout,
	}
}

// SendWithAck sends an event and waits for acknowledgment
func (m *AckManager) SendWithAck(ctx context.Context, client WSClient, event *EventMessage, timeout time.Duration) (*EventAck, error) {
	if timeout == 0 {
		timeout = m.defaultTimeout
	}

	// Generate ACK ID if not present
	if event.AckID == "" {
		event.AckID = generateID()
	}

	// Create pending acknowledgment
	ackChan := make(chan *EventAck, 1)
	ackCtx, cancel := context.WithTimeout(ctx, timeout)

	pending := &pendingAck{
		event:   event,
		ackChan: ackChan,
		ctx:     ackCtx,
		cancel:  cancel,
	}

	// Set up cleanup timer before storing to avoid race conditions
	pending.timer = time.AfterFunc(timeout, func() {
		m.timeoutAck(event.AckID)
	})

	// Store pending ack
	m.pendingMu.Lock()
	m.pending[event.AckID] = pending
	m.pendingMu.Unlock()

	// Send the event
	if err := client.SendJSON(event); err != nil {
		m.cleanupAck(event.AckID)
		return nil, fmt.Errorf("failed to send event: %w", err)
	}

	// Wait for acknowledgment
	select {
	case ack := <-ackChan:
		return ack, nil
	case <-ackCtx.Done():
		m.cleanupAck(event.AckID)
		return nil, errors.New("acknowledgment timeout")
	}
}

// SendWithCallback sends an event and calls callback on acknowledgment
func (m *AckManager) SendWithCallback(ctx context.Context, client WSClient, event *EventMessage, callback AckHandler, timeout time.Duration) error {
	if timeout == 0 {
		timeout = m.defaultTimeout
	}

	// Generate ACK ID if not present
	if event.AckID == "" {
		event.AckID = generateID()
	}

	// Create pending acknowledgment
	ackCtx, cancel := context.WithTimeout(ctx, timeout)

	pending := &pendingAck{
		event:    event,
		callback: callback,
		ctx:      ackCtx,
		cancel:   cancel,
	}

	// Set up cleanup timer before storing to avoid race conditions
	pending.timer = time.AfterFunc(timeout, func() {
		m.timeoutAck(event.AckID)
	})

	// Store pending ack
	m.pendingMu.Lock()
	m.pending[event.AckID] = pending
	m.pendingMu.Unlock()

	// Send the event
	if err := client.SendJSON(event); err != nil {
		m.cleanupAck(event.AckID)
		return fmt.Errorf("failed to send event: %w", err)
	}

	return nil
}

// HandleAck processes an incoming acknowledgment
func (m *AckManager) HandleAck(ack *EventAck) error {
	m.pendingMu.Lock()
	pending, exists := m.pending[ack.ID]
	if !exists {
		m.pendingMu.Unlock()
		return fmt.Errorf("no pending acknowledgment for ID: %s", ack.ID)
	}

	// Create a copy of the needed fields under lock to avoid race conditions
	ackChan := pending.ackChan
	callback := pending.callback
	ctx := pending.ctx
	timer := pending.timer
	cancel := pending.cancel

	// Remove from pending map immediately to prevent double processing
	delete(m.pending, ack.ID)
	m.pendingMu.Unlock()

	// Stop timeout timer
	if timer != nil {
		timer.Stop()
	}

	// Handle based on type using copied values
	if ackChan != nil {
		// Channel-based acknowledgment
		select {
		case ackChan <- ack:
		default:
			// Channel might be closed
		}
	} else if callback != nil {
		// Callback-based acknowledgment
		go func() {
			if err := callback(ctx, ack); err != nil {
				// Log error (implementation would use actual logger)
				fmt.Printf("ACK callback error: %v\n", err)
			}
		}()
	}

	// Final cleanup (already removed from map, just clean up resources)
	if timer != nil {
		timer.Stop()
	}
	if cancel != nil {
		cancel()
	}
	if ackChan != nil {
		close(ackChan)
	}

	return nil
}

func (m *AckManager) timeoutAck(ackID string) {
	m.pendingMu.Lock()
	pending, exists := m.pending[ackID]
	if !exists {
		m.pendingMu.Unlock()
		return
	}

	// Create a copy of the needed fields under lock to avoid race conditions
	ackChan := pending.ackChan
	callback := pending.callback
	ctx := pending.ctx
	cancel := pending.cancel

	// Remove from pending map immediately to prevent double processing
	delete(m.pending, ackID)
	m.pendingMu.Unlock()

	// Create timeout acknowledgment
	timeoutAck := &EventAck{
		ID:        ackID,
		Success:   false,
		Error:     "acknowledgment timeout",
		Timestamp: time.Now(),
	}

	// Handle based on type using copied values
	if ackChan != nil {
		select {
		case ackChan <- timeoutAck:
		default:
			// Channel might be closed
		}
	} else if callback != nil {
		go func() {
			if err := callback(ctx, timeoutAck); err != nil {
				// Log error
				fmt.Printf("ACK timeout callback error: %v\n", err)
			}
		}()
	}

	// Final cleanup (already removed from map, just clean up resources)
	if cancel != nil {
		cancel()
	}
	if ackChan != nil {
		close(ackChan)
	}
}

func (m *AckManager) cleanupAck(ackID string) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()

	if pending, exists := m.pending[ackID]; exists {
		m.cleanupAckUnsafe(ackID, pending)
	}
}

func (m *AckManager) cleanupAckUnsafe(ackID string, pending *pendingAck) {
	if pending.timer != nil {
		pending.timer.Stop()
	}
	if pending.cancel != nil {
		pending.cancel()
	}
	if pending.ackChan != nil {
		close(pending.ackChan)
	}
	// Only delete from map if it still exists (might have been deleted already)
	delete(m.pending, ackID)
}

// CancelAck cancels a pending acknowledgment
func (m *AckManager) CancelAck(ackID string) {
	m.cleanupAck(ackID)
}

// PendingCount returns the number of pending acknowledgments
func (m *AckManager) PendingCount() int {
	m.pendingMu.RLock()
	defer m.pendingMu.RUnlock()
	return len(m.pending)
}

// AckMiddleware creates middleware that handles acknowledgments
func AckMiddleware(ackManager *AckManager) EventMiddleware {
	return func(ctx context.Context, client WSClient, event *EventMessage, next EventMiddlewareNext) error {
		// Check if this is an acknowledgment
		if event.Type == "ack" {
			// Parse acknowledgment data
			if ackData, ok := event.Data.(*EventAck); ok {
				return ackManager.HandleAck(ackData)
			}
		}

		// Continue with normal event processing
		return next(ctx, client, event)
	}
}

// EmitWithAck sends an event and waits for acknowledgment
func EmitWithAck(ctx context.Context, client WSClient, eventType string, data any, timeout time.Duration) (*EventAck, error) {
	// This would be integrated with the client's ack manager
	event := &EventMessage{
		ID:        generateID(),
		Type:      eventType,
		Data:      data,
		Timestamp: time.Now(),
		AckID:     generateID(),
	}

	// Create a temporary ack manager for this operation
	ackMgr := NewAckManager(timeout)
	return ackMgr.SendWithAck(ctx, client, event, timeout)
}

// RequestResponse implements request-response pattern using acknowledgments
func RequestResponse(ctx context.Context, client WSClient, request *EventMessage, timeout time.Duration) (*EventMessage, error) {
	// Add ACK ID to request
	if request.AckID == "" {
		request.AckID = generateID()
	}

	// Create response channel
	responseChan := make(chan *EventMessage, 1)

	// Set up timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Send request
	if err := client.SendJSON(request); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Wait for response
	select {
	case response := <-responseChan:
		return response, nil
	case <-timeoutCtx.Done():
		return nil, errors.New("request timeout")
	}
}

// BatchedAck allows acknowledging multiple events at once
type BatchedAck struct {
	IDs       []string  `json:"ids"`
	Success   bool      `json:"success"`
	Results   any       `json:"results,omitempty"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// AckBatcher batches acknowledgments for efficiency
type AckBatcher struct {
	batch      []*EventAck
	batchMu    sync.Mutex
	maxSize    int
	interval   time.Duration
	flushTimer *time.Timer
	flushFunc  func([]*EventAck)
}

// NewAckBatcher creates a new acknowledgment batcher
func NewAckBatcher(maxSize int, interval time.Duration, flushFunc func([]*EventAck)) *AckBatcher {
	if maxSize == 0 {
		maxSize = 100
	}
	if interval == 0 {
		interval = 100 * time.Millisecond
	}

	return &AckBatcher{
		maxSize:   maxSize,
		interval:  interval,
		flushFunc: flushFunc,
	}
}

// Add adds an acknowledgment to the batch
func (b *AckBatcher) Add(ack *EventAck) {
	b.batchMu.Lock()
	defer b.batchMu.Unlock()

	b.batch = append(b.batch, ack)

	// Start flush timer if needed
	if b.flushTimer == nil {
		b.flushTimer = time.AfterFunc(b.interval, b.flush)
	}

	// Flush if batch is full
	if len(b.batch) >= b.maxSize {
		b.flushNow()
	}
}

func (b *AckBatcher) flush() {
	b.batchMu.Lock()
	defer b.batchMu.Unlock()
	b.flushNow()
}

func (b *AckBatcher) flushNow() {
	if len(b.batch) == 0 {
		return
	}

	// Call flush function with current batch
	if b.flushFunc != nil {
		batch := b.batch
		go b.flushFunc(batch)
	}

	// Reset batch and timer
	b.batch = nil
	if b.flushTimer != nil {
		b.flushTimer.Stop()
		b.flushTimer = nil
	}
}

// Close flushes any remaining acknowledgments
func (b *AckBatcher) Close() {
	b.batchMu.Lock()
	defer b.batchMu.Unlock()

	if b.flushTimer != nil {
		b.flushTimer.Stop()
	}

	b.flushNow()
}
