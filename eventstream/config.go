package eventstream

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBufferSize is the default retained-record capacity per published scope.
	DefaultBufferSize = 256
	// DefaultSubscriberQueueSize is the default buffered queue capacity per subscriber.
	DefaultSubscriberQueueSize = 32
	dropReasonSlowConsumer     = "slow_consumer"
	dropReasonClientDisconnect = "client_disconnect"
	dropReasonCursorNotFound   = "cursor_not_found"
)

// Option mutates stream configuration before construction.
type Option func(*Config)

// Config defines the constructor-time surface for stream behavior.
type Config struct {
	BufferSize          int
	SubscriberQueueSize int
	Matcher             MatchFunc
	Hooks               Hooks
}

// DefaultConfig returns the normalized constructor defaults for the stream.
func DefaultConfig() Config {
	return Config{
		BufferSize:          DefaultBufferSize,
		SubscriberQueueSize: DefaultSubscriberQueueSize,
		Matcher:             ExactMatch,
	}
}

// New constructs an in-memory scoped stream with bounded replay and live fanout.
func New(opts ...Option) Stream {
	cfg := DefaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	cfg = cfg.normalized()
	return &stream{
		cfg:         cfg,
		buckets:     map[string]*scopeBucket{},
		subscribers: map[*subscriber]struct{}{},
		stats: Stats{
			DropReasons: map[string]int64{},
		},
	}
}

// WithBufferSize sets retained replay capacity per published scope.
func WithBufferSize(size int) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.BufferSize = size
		}
	}
}

// WithSubscriberQueueSize sets the live delivery queue size per subscriber.
func WithSubscriberQueueSize(size int) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.SubscriberQueueSize = size
		}
	}
}

// WithSubscriberBuffer is an alias for WithSubscriberQueueSize.
func WithSubscriberBuffer(size int) Option {
	return WithSubscriberQueueSize(size)
}

// WithMatcher overrides the default exact scope matcher.
func WithMatcher(matcher MatchFunc) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.Matcher = matcher
		}
	}
}

// WithHooks configures optional observability callbacks.
func WithHooks(hooks Hooks) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.Hooks = hooks
		}
	}
}

func (c Config) normalized() Config {
	if c.BufferSize <= 0 {
		c.BufferSize = DefaultBufferSize
	}
	if c.SubscriberQueueSize <= 0 {
		c.SubscriberQueueSize = DefaultSubscriberQueueSize
	}
	if c.Matcher == nil {
		c.Matcher = ExactMatch
	}
	return c
}

// ExactMatch requires the published scope to equal the subscription scope.
func ExactMatch(subscription Scope, published Scope) bool {
	if len(subscription) != len(published) {
		return false
	}
	for key, value := range subscription {
		if published[key] != value {
			return false
		}
	}
	return true
}

// SubsetMatch allows broader subscriptions, such as tenant-only listeners, to
// receive more specific published scopes that include additional labels.
func SubsetMatch(subscription Scope, published Scope) bool {
	if len(subscription) > len(published) {
		return false
	}
	for key, value := range subscription {
		if published[key] != value {
			return false
		}
	}
	return true
}

type stream struct {
	mu          sync.RWMutex
	cfg         Config
	nextCursor  uint64
	buckets     map[string]*scopeBucket
	subscribers map[*subscriber]struct{}
	stats       Stats
}

type scopeBucket struct {
	scope   Scope
	key     string
	records []Record
}

type subscriber struct {
	stream   *stream
	scope    Scope
	scopeKey string
	ctx      context.Context
	in       chan Record
	out      chan Record
	done     chan struct{}
	stopOnce sync.Once
}

func (s *stream) Publish(scope Scope, event Event) Record {
	scope = cloneScope(scope)
	now := time.Now().UTC()
	dropHooks := make([]DropEvent, 0)

	s.mu.Lock()
	s.nextCursor++
	storedRecord := Record{
		Cursor:      strconv.FormatUint(s.nextCursor, 10),
		ScopeKey:    canonicalScopeKey(scope),
		Event:       cloneEvent(event),
		PublishedAt: now,
	}
	if storedRecord.Event.Timestamp.IsZero() {
		storedRecord.Event.Timestamp = storedRecord.PublishedAt
	}

	bucket := s.ensureBucketLocked(scope, storedRecord.ScopeKey)
	if len(bucket.records) == s.cfg.BufferSize {
		bucket.records = append(bucket.records[:0], bucket.records[1:]...)
		s.stats.BufferedRecords--
	}
	bucket.records = append(bucket.records, storedRecord)
	s.stats.BufferedRecords++
	s.stats.PublishedCount++

	var slowConsumers []*subscriber
	for sub := range s.subscribers {
		if !s.cfg.Matcher(sub.scope, bucket.scope) {
			continue
		}
		select {
		case sub.in <- cloneRecord(storedRecord):
		default:
			slowConsumers = append(slowConsumers, sub)
		}
	}
	for _, sub := range slowConsumers {
		if drop := s.removeSubscriberLocked(sub, dropReasonSlowConsumer); drop != nil {
			dropHooks = append(dropHooks, *drop)
		}
	}
	s.mu.Unlock()

	s.dispatchPublishHook(PublishEvent{
		Scope:    scope,
		ScopeKey: storedRecord.ScopeKey,
		Record:   storedRecord,
	})
	s.dispatchDropHooks(dropHooks)

	return cloneRecord(storedRecord)
}

func (s *stream) Subscribe(ctx context.Context, scope Scope, afterCursor string) (*Subscription, error) {
	if ctx == nil {
		return nil, fmt.Errorf("eventstream subscribe requires context")
	}

	scope = cloneScope(scope)
	scopeKey := canonicalScopeKey(scope)
	subscribeHook := SubscribeEvent{
		Scope:       cloneScope(scope),
		ScopeKey:    scopeKey,
		AfterCursor: afterCursor,
	}

	s.mu.Lock()
	replay, cursorGap, cursorGapReason := s.replayLocked(scope, afterCursor)
	if cursorGap {
		s.stats.DropReasons[dropReasonCursorNotFound]++
		subscribeHook.CursorGap = true
		subscribeHook.CursorGapReason = cursorGapReason
		s.mu.Unlock()
		s.dispatchSubscribeHook(subscribeHook)
		s.dispatchDropHook(&DropEvent{
			Scope:    cloneScope(scope),
			ScopeKey: scopeKey,
			Reason:   cursorGapReason,
		})
		return newGapSubscription(scope, scopeKey, cursorGapReason), nil
	}

	sub := &subscriber{
		stream:   s,
		scope:    scope,
		scopeKey: scopeKey,
		ctx:      ctx,
		in:       make(chan Record, s.cfg.SubscriberQueueSize),
		out:      make(chan Record, s.cfg.SubscriberQueueSize),
		done:     make(chan struct{}),
	}
	s.subscribers[sub] = struct{}{}
	s.stats.ActiveSubscribers = len(s.subscribers)
	if afterCursor != "" {
		s.stats.ResumeCount++
	}
	s.mu.Unlock()

	subscribeHook.ReplayCount = len(replay)
	s.dispatchSubscribeHook(subscribeHook)
	go sub.run(replay)

	return &Subscription{
		Scope:    cloneScope(scope),
		ScopeKey: scopeKey,
		Records:  sub.out,
	}, nil
}

func (s *stream) SnapshotStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := s.stats
	stats.DropReasons = cloneDropReasons(stats.DropReasons)
	return stats
}

func (s *stream) ensureBucketLocked(scope Scope, scopeKey string) *scopeBucket {
	if bucket, ok := s.buckets[scopeKey]; ok {
		return bucket
	}
	bucket := &scopeBucket{
		scope: cloneScope(scope),
		key:   scopeKey,
	}
	s.buckets[scopeKey] = bucket
	return bucket
}

func (s *stream) replayLocked(scope Scope, afterCursor string) ([]Record, bool, string) {
	if afterCursor == "" {
		return nil, false, ""
	}

	matched := make([]Record, 0)
	hasRetainedMatch := false
	for _, bucket := range s.buckets {
		if !s.cfg.Matcher(scope, bucket.scope) {
			continue
		}
		if len(bucket.records) > 0 {
			hasRetainedMatch = true
		}
		matched = append(matched, bucket.records...)
	}
	if len(matched) == 0 {
		return nil, false, ""
	}

	sort.Slice(matched, func(i, j int) bool {
		return compareCursor(matched[i].Cursor, matched[j].Cursor) < 0
	})

	for idx := range matched {
		if matched[idx].Cursor != afterCursor {
			continue
		}
		if idx+1 >= len(matched) {
			return nil, false, ""
		}
		replay := make([]Record, 0, len(matched[idx+1:]))
		for _, record := range matched[idx+1:] {
			replay = append(replay, cloneRecord(record))
		}
		return replay, false, ""
	}

	if hasRetainedMatch {
		return nil, true, dropReasonCursorNotFound
	}
	return nil, false, ""
}

func (s *stream) removeSubscriber(sub *subscriber, reason string) {
	if sub == nil {
		return
	}
	var drop *DropEvent
	s.mu.Lock()
	drop = s.removeSubscriberLocked(sub, reason)
	s.mu.Unlock()
	s.dispatchDropHook(drop)
}

func (s *stream) removeSubscriberLocked(sub *subscriber, reason string) *DropEvent {
	var drop *DropEvent
	sub.stopOnce.Do(func() {
		if _, ok := s.subscribers[sub]; !ok {
			return
		}
		delete(s.subscribers, sub)
		s.stats.ActiveSubscribers = len(s.subscribers)
		if reason != "" {
			s.stats.DropReasons[reason]++
		}
		close(sub.done)
		close(sub.in)
		drop = &DropEvent{
			Scope:    cloneScope(sub.scope),
			ScopeKey: sub.scopeKey,
			Reason:   reason,
		}
	})
	return drop
}

func (s *subscriber) run(replay []Record) {
	defer close(s.out)
	defer s.stream.removeSubscriber(s, dropReasonClientDisconnect)

	for _, record := range replay {
		if !s.send(record) {
			return
		}
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.done:
			return
		case record, ok := <-s.in:
			if !ok {
				return
			}
			if !s.send(record) {
				return
			}
		}
	}
}

func (s *subscriber) send(record Record) bool {
	select {
	case <-s.ctx.Done():
		return false
	case <-s.done:
		return false
	case s.out <- record:
		return true
	}
}

func newGapSubscription(scope Scope, scopeKey string, reason string) *Subscription {
	records := make(chan Record)
	close(records)
	return &Subscription{
		Scope:           cloneScope(scope),
		ScopeKey:        scopeKey,
		Records:         records,
		CursorGap:       true,
		CursorGapReason: reason,
	}
}

func compareCursor(a string, b string) int {
	left, leftErr := strconv.ParseUint(strings.TrimSpace(a), 10, 64)
	right, rightErr := strconv.ParseUint(strings.TrimSpace(b), 10, 64)
	if leftErr == nil && rightErr == nil {
		switch {
		case left < right:
			return -1
		case left > right:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(a, b)
}

func canonicalScopeKey(scope Scope) string {
	if len(scope) == 0 {
		return ""
	}

	keys := make([]string, 0, len(scope))
	for key := range scope {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(scope[key]))
	}
	return strings.Join(parts, ";")
}

func cloneDropReasons(in map[string]int64) map[string]int64 {
	if len(in) == 0 {
		return map[string]int64{}
	}
	out := make(map[string]int64, len(in))
	maps.Copy(out, in)
	return out
}

func cloneScope(scope Scope) Scope {
	if len(scope) == 0 {
		return nil
	}
	keys := make([]string, 0, len(scope))
	for key := range scope {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	out := make(Scope, len(keys))
	for _, key := range keys {
		out[key] = scope[key]
	}
	return out
}

func cloneEvent(event Event) Event {
	out := event
	if len(event.Payload) > 0 {
		out.Payload = append([]byte(nil), event.Payload...)
	}
	if len(event.Metadata) > 0 {
		out.Metadata = make(map[string]string, len(event.Metadata))
		maps.Copy(out.Metadata, event.Metadata)
	}
	return out
}

func cloneRecord(record Record) Record {
	return Record{
		Cursor:      record.Cursor,
		ScopeKey:    record.ScopeKey,
		Event:       cloneEvent(record.Event),
		PublishedAt: record.PublishedAt,
	}
}

func (s *stream) dispatchPublishHook(event PublishEvent) {
	if s.cfg.Hooks.OnPublish == nil {
		return
	}
	go safeHookCall(func() {
		s.cfg.Hooks.OnPublish(PublishEvent{
			Scope:    cloneScope(event.Scope),
			ScopeKey: event.ScopeKey,
			Record:   cloneRecord(event.Record),
		})
	})
}

func (s *stream) dispatchSubscribeHook(event SubscribeEvent) {
	if s.cfg.Hooks.OnSubscribe == nil {
		return
	}
	go safeHookCall(func() {
		s.cfg.Hooks.OnSubscribe(SubscribeEvent{
			Scope:           cloneScope(event.Scope),
			ScopeKey:        event.ScopeKey,
			AfterCursor:     event.AfterCursor,
			ReplayCount:     event.ReplayCount,
			CursorGap:       event.CursorGap,
			CursorGapReason: event.CursorGapReason,
		})
	})
}

func (s *stream) dispatchDropHooks(events []DropEvent) {
	for _, event := range events {
		eventCopy := event
		s.dispatchDropHook(&eventCopy)
	}
}

func (s *stream) dispatchDropHook(event *DropEvent) {
	if event == nil || s.cfg.Hooks.OnDrop == nil {
		return
	}
	go safeHookCall(func() {
		s.cfg.Hooks.OnDrop(DropEvent{
			Scope:    cloneScope(event.Scope),
			ScopeKey: event.ScopeKey,
			Reason:   event.Reason,
		})
	})
}

func safeHookCall(fn func()) {
	defer func() {
		_ = recover()
	}()
	fn()
}
