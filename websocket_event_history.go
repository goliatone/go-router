package router

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// EventHistory stores event history for replay
type EventHistory struct {
	events   []*EventMessage
	eventsMu sync.RWMutex

	maxSize int
	ttl     time.Duration

	// Index for fast lookup
	byType      map[string][]*EventMessage
	byNamespace map[string][]*EventMessage
	byClient    map[string][]*EventMessage
	indexMu     sync.RWMutex
}

// EventHistoryFilter filters events in history
type EventHistoryFilter struct {
	// Filter by event type
	Type string

	// Filter by namespace
	Namespace string

	// Filter by client ID
	ClientID string

	// Filter by time range
	Since  *time.Time
	Before *time.Time

	// Maximum number of events to return
	Limit int

	// Offset for pagination
	Offset int
}

// NewEventHistory creates a new event history
func NewEventHistory(maxSize int, ttl time.Duration) *EventHistory {
	h := &EventHistory{
		events:      make([]*EventMessage, 0, maxSize),
		maxSize:     maxSize,
		ttl:         ttl,
		byType:      make(map[string][]*EventMessage),
		byNamespace: make(map[string][]*EventMessage),
		byClient:    make(map[string][]*EventMessage),
	}

	// Start cleanup goroutine
	go h.cleanup()

	return h
}

// Add adds an event to history
func (h *EventHistory) Add(event *EventMessage) {
	h.eventsMu.Lock()
	h.indexMu.Lock()
	defer h.eventsMu.Unlock()
	defer h.indexMu.Unlock()

	// Check size limit
	if len(h.events) >= h.maxSize {
		// Remove oldest event
		oldest := h.events[0]
		h.events = h.events[1:]
		h.removeFromIndex(oldest)
	}

	// Add to history
	h.events = append(h.events, event)

	// Update indexes
	h.addToIndex(event)
}

// Get retrieves events from history based on filter
func (h *EventHistory) Get(filter EventHistoryFilter) []*EventMessage {
	h.eventsMu.RLock()
	defer h.eventsMu.RUnlock()

	var filtered []*EventMessage

	// Use index if possible
	if filter.Type != "" {
		h.indexMu.RLock()
		filtered = h.byType[filter.Type]
		h.indexMu.RUnlock()
	} else if filter.Namespace != "" {
		h.indexMu.RLock()
		filtered = h.byNamespace[filter.Namespace]
		h.indexMu.RUnlock()
	} else if filter.ClientID != "" {
		h.indexMu.RLock()
		filtered = h.byClient[filter.ClientID]
		h.indexMu.RUnlock()
	} else {
		filtered = h.events
	}

	// Apply additional filters
	result := make([]*EventMessage, 0)
	for _, event := range filtered {
		if h.matchesFilter(event, filter) {
			result = append(result, event)
		}
	}

	// Apply pagination
	if filter.Offset > 0 {
		if filter.Offset >= len(result) {
			return nil
		}
		result = result[filter.Offset:]
	}

	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}

	return result
}

// Replay replays events to a client
func (h *EventHistory) Replay(client WSClient, filter EventHistoryFilter) error {
	events := h.Get(filter)

	for _, event := range events {
		// Create replay event with metadata
		replayEvent := &EventMessage{
			ID:        event.ID,
			Type:      event.Type,
			Namespace: event.Namespace,
			Data:      event.Data,
			Metadata: map[string]any{
				"replay":            true,
				"originalTimestamp": event.Timestamp,
			},
			Timestamp: time.Now(),
		}

		if err := client.SendJSON(replayEvent); err != nil {
			return err
		}

		// Small delay between replayed events
		time.Sleep(10 * time.Millisecond)
	}

	return nil
}

// Clear clears all history
func (h *EventHistory) Clear() {
	h.eventsMu.Lock()
	h.indexMu.Lock()
	defer h.eventsMu.Unlock()
	defer h.indexMu.Unlock()

	h.events = h.events[:0]
	h.byType = make(map[string][]*EventMessage)
	h.byNamespace = make(map[string][]*EventMessage)
	h.byClient = make(map[string][]*EventMessage)
}

// Size returns the number of events in history
func (h *EventHistory) Size() int {
	h.eventsMu.RLock()
	defer h.eventsMu.RUnlock()
	return len(h.events)
}

func (h *EventHistory) matchesFilter(event *EventMessage, filter EventHistoryFilter) bool {
	// Check type
	if filter.Type != "" && event.Type != filter.Type {
		return false
	}

	// Check namespace
	if filter.Namespace != "" && event.Namespace != filter.Namespace {
		return false
	}

	// Check time range
	if filter.Since != nil && event.Timestamp.Before(*filter.Since) {
		return false
	}
	if filter.Before != nil && event.Timestamp.After(*filter.Before) {
		return false
	}

	return true
}

func (h *EventHistory) addToIndex(event *EventMessage) {
	// Add to type index
	if event.Type != "" {
		h.byType[event.Type] = append(h.byType[event.Type], event)
	}

	// Add to namespace index
	if event.Namespace != "" {
		h.byNamespace[event.Namespace] = append(h.byNamespace[event.Namespace], event)
	}

	// Add to client index (if client ID is in metadata)
	if clientID, ok := event.Metadata["clientID"].(string); ok && clientID != "" {
		h.byClient[clientID] = append(h.byClient[clientID], event)
	}
}

func (h *EventHistory) removeFromIndex(event *EventMessage) {
	// Remove from type index
	if event.Type != "" {
		events := h.byType[event.Type]
		h.removeFromSlice(&events, event)
		h.byType[event.Type] = events
	}

	// Remove from namespace index
	if event.Namespace != "" {
		events := h.byNamespace[event.Namespace]
		h.removeFromSlice(&events, event)
		h.byNamespace[event.Namespace] = events
	}

	// Remove from client index
	if clientID, ok := event.Metadata["clientID"].(string); ok && clientID != "" {
		events := h.byClient[clientID]
		h.removeFromSlice(&events, event)
		h.byClient[clientID] = events
	}
}

func (h *EventHistory) removeFromSlice(slice *[]*EventMessage, event *EventMessage) {
	for i, e := range *slice {
		if e == event {
			*slice = append((*slice)[:i], (*slice)[i+1:]...)
			break
		}
	}
}

func (h *EventHistory) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		h.cleanupOldEvents()
	}
}

func (h *EventHistory) cleanupOldEvents() {
	if h.ttl == 0 {
		return
	}

	h.eventsMu.Lock()
	h.indexMu.Lock()
	defer h.eventsMu.Unlock()
	defer h.indexMu.Unlock()

	cutoff := time.Now().Add(-h.ttl)

	// Find first event that is not expired
	firstValid := 0
	for i, event := range h.events {
		if event.Timestamp.After(cutoff) {
			firstValid = i
			break
		}
		h.removeFromIndex(event)
	}

	// Remove expired events
	if firstValid > 0 {
		h.events = h.events[firstValid:]
	}
}

// EventBatcher batches events for efficient processing
type EventBatcher struct {
	batch      []*EventMessage
	batchMu    sync.Mutex
	maxSize    int
	interval   time.Duration
	flushTimer *time.Timer
	processor  func([]*EventMessage)
}

// NewEventBatcher creates a new event batcher
func NewEventBatcher(maxSize int, interval time.Duration, processor func([]*EventMessage)) *EventBatcher {
	if maxSize == 0 {
		maxSize = 100
	}
	if interval == 0 {
		interval = 100 * time.Millisecond
	}

	return &EventBatcher{
		maxSize:   maxSize,
		interval:  interval,
		processor: processor,
	}
}

// Add adds an event to the batch
func (b *EventBatcher) Add(event *EventMessage) {
	b.batchMu.Lock()
	defer b.batchMu.Unlock()

	b.batch = append(b.batch, event)

	// Start flush timer if needed
	if b.flushTimer == nil {
		b.flushTimer = time.AfterFunc(b.interval, b.flush)
	}

	// Flush if batch is full
	if len(b.batch) >= b.maxSize {
		b.flushNow()
	}
}

func (b *EventBatcher) flush() {
	b.batchMu.Lock()
	defer b.batchMu.Unlock()
	b.flushNow()
}

func (b *EventBatcher) flushNow() {
	if len(b.batch) == 0 {
		return
	}

	// Process current batch
	if b.processor != nil {
		batch := b.batch
		go b.processor(batch)
	}

	// Reset batch and timer
	b.batch = nil
	if b.flushTimer != nil {
		b.flushTimer.Stop()
		b.flushTimer = nil
	}
}

// Close flushes any remaining events
func (b *EventBatcher) Close() {
	b.batchMu.Lock()
	defer b.batchMu.Unlock()

	if b.flushTimer != nil {
		b.flushTimer.Stop()
	}

	b.flushNow()
}

// EventThrottler throttles events by type
type EventThrottler struct {
	limits       map[string]*throttleLimit
	limitsMu     sync.RWMutex
	defaultLimit int
	window       time.Duration
	OnThrottled  func(event *EventMessage) // Optional handler for throttled events
}

type throttleLimit struct {
	count     int
	resetTime time.Time
	limit     int
	mu        sync.Mutex
}

// NewEventThrottler creates a new event throttler
func NewEventThrottler(defaultLimit int, window time.Duration) *EventThrottler {
	if defaultLimit == 0 {
		defaultLimit = 100
	}
	if window == 0 {
		window = 1 * time.Second
	}

	return &EventThrottler{
		limits:       make(map[string]*throttleLimit),
		defaultLimit: defaultLimit,
		window:       window,
	}
}

// Allow checks if an event should be allowed
func (t *EventThrottler) Allow(eventType string) bool {
	// First, try to get existing limit with read lock
	t.limitsMu.RLock()
	limit, exists := t.limits[eventType]
	t.limitsMu.RUnlock()

	if !exists {
		// Need to create new limit - use write lock and double-check
		t.limitsMu.Lock()
		// Double-check pattern: another goroutine might have created it
		if limit, exists = t.limits[eventType]; !exists {
			limit = &throttleLimit{
				resetTime: time.Now().Add(t.window),
				limit:     t.defaultLimit,
			}
			t.limits[eventType] = limit
		}
		t.limitsMu.Unlock()
	}

	limit.mu.Lock()
	defer limit.mu.Unlock()

	now := time.Now()
	if now.After(limit.resetTime) {
		limit.count = 0
		limit.resetTime = now.Add(t.window)
	}

	limit.count++
	// Use custom limit if set, otherwise use default limit
	effectiveLimit := limit.limit
	if effectiveLimit == 0 {
		effectiveLimit = t.defaultLimit
	}
	return limit.count <= effectiveLimit
}

// SetLimit sets a custom limit for an event type
func (t *EventThrottler) SetLimit(eventType string, limit int) {
	t.limitsMu.Lock()
	defer t.limitsMu.Unlock()

	if l, exists := t.limits[eventType]; exists {
		l.mu.Lock()
		l.limit = limit
		l.mu.Unlock()
	} else {
		t.limits[eventType] = &throttleLimit{
			limit:     limit,
			resetTime: time.Now().Add(t.window),
		}
	}
}

// ThrottleMiddleware creates middleware that throttles events
func ThrottleMiddleware(throttler *EventThrottler) EventMiddleware {
	return func(ctx context.Context, client WSClient, event *EventMessage, next EventMiddlewareNext) error {
		if !throttler.Allow(event.Type) {
			// Event is throttled - invoke observer if configured
			if throttler.OnThrottled != nil {
				throttler.OnThrottled(event)
			}
			return nil // Drop throttled events
		}

		return next(ctx, client, event)
	}
}

// generateID generates a unique ID for events and acknowledgments
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID
		return time.Now().Format("20060102150405.999999999")
	}
	return hex.EncodeToString(b)
}
