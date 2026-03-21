package ssefiber

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/eventstream"
)

const (
	defaultPath                 = "/events"
	defaultCursorQueryParam     = "cursor"
	defaultHeartbeatQueryParam  = "heartbeat_ms"
	defaultRetryQueryParam      = "retry_ms"
	defaultHeartbeatInterval    = 15 * time.Second
	defaultRetryInterval        = 3 * time.Second
	defaultMinHeartbeatInterval = 5 * time.Second
	defaultMaxHeartbeatInterval = 60 * time.Second
	defaultMinRetryInterval     = 1 * time.Second
	defaultMaxRetryInterval     = 30 * time.Second
)

// ScopeResolver maps request state into an eventstream subscription scope.
type ScopeResolver func(router.Context) (eventstream.Scope, error)

// Option mutates SSE mount and handler configuration.
type Option func(*Config)

// Config defines the public SSE mount and handler surface.
type Config struct {
	Path                 string
	Stream               eventstream.Stream
	ScopeResolver        ScopeResolver
	Middlewares          []router.MiddlewareFunc
	HeartbeatInterval    time.Duration
	RetryInterval        time.Duration
	CursorQueryParam     string
	HeartbeatQueryParam  string
	RetryQueryParam      string
	AllowClientTuning    bool
	MinHeartbeatInterval time.Duration
	MaxHeartbeatInterval time.Duration
	MinRetryInterval     time.Duration
	MaxRetryInterval     time.Duration
}

type effectiveConfig struct {
	heartbeat time.Duration
	retry     time.Duration
}

type heartbeatFrame struct {
	Timestamp string `json:"timestamp"`
	ScopeKey  string `json:"scope_key"`
}

type streamGapFrame struct {
	Reason               string `json:"reason"`
	LastEventID          string `json:"last_event_id,omitempty"`
	FallbackTransport    string `json:"fallback_transport"`
	ResumeSupported      bool   `json:"resume_supported"`
	RequiresGapReconcile bool   `json:"requires_gap_reconcile"`
	Timestamp            string `json:"timestamp"`
}

func defaultConfig() Config {
	return Config{
		Path:                 defaultPath,
		HeartbeatInterval:    defaultHeartbeatInterval,
		RetryInterval:        defaultRetryInterval,
		CursorQueryParam:     defaultCursorQueryParam,
		HeartbeatQueryParam:  defaultHeartbeatQueryParam,
		RetryQueryParam:      defaultRetryQueryParam,
		AllowClientTuning:    false,
		MinHeartbeatInterval: defaultMinHeartbeatInterval,
		MaxHeartbeatInterval: defaultMaxHeartbeatInterval,
		MinRetryInterval:     defaultMinRetryInterval,
		MaxRetryInterval:     defaultMaxRetryInterval,
	}
}

// MountFiber registers the SSE handler on a Fiber-backed go-router router.
func MountFiber(r router.Router[*fiber.App], opts ...Option) error {
	if r == nil {
		return fmt.Errorf("sse fiber mount requires router")
	}

	cfg := buildConfig(opts...)
	if err := validateConfig(cfg); err != nil {
		return err
	}

	r.Get(cfg.Path, handlerWithConfig(cfg), cloneMiddlewares(cfg.Middlewares)...)
	return nil
}

// Handler returns the public go-router handler surface for SSE.
func Handler(opts ...Option) router.HandlerFunc {
	return handlerWithConfig(buildConfig(opts...))
}

func handlerWithConfig(cfg Config) router.HandlerFunc {
	return func(ctx router.Context) error {
		if err := validateConfig(cfg); err != nil {
			return err
		}

		scope, err := cfg.ScopeResolver(ctx)
		if err != nil {
			return err
		}

		cursor := resolveCursor(ctx, cfg.CursorQueryParam)
		effective := resolveEffectiveConfig(cfg, ctx)

		sub, err := cfg.Stream.Subscribe(ctx.Context(), scope, cursor)
		if err != nil {
			return err
		}

		ctx.SetHeader("Content-Type", "text/event-stream")
		ctx.SetHeader("Cache-Control", "no-cache")
		ctx.SetHeader("Connection", "keep-alive")
		ctx.SetHeader("X-Accel-Buffering", "no")

		reader, writer := io.Pipe()
		go func() {
			defer writer.Close()

			if effective.retry > 0 {
				if err := writeRetryDirective(writer, effective.retry); err != nil {
					_ = writer.CloseWithError(err)
					return
				}
			}

			if sub.CursorGap {
				if err := writeGapFrame(writer, streamGapFrame{
					Reason:               sub.CursorGapReason,
					LastEventID:          cursor,
					FallbackTransport:    "polling",
					ResumeSupported:      false,
					RequiresGapReconcile: true,
					Timestamp:            time.Now().UTC().Format(time.RFC3339Nano),
				}); err != nil {
					_ = writer.CloseWithError(err)
				}
				return
			}

			var (
				heartbeatTicker *time.Ticker
				heartbeatC      <-chan time.Time
			)
			if effective.heartbeat > 0 {
				heartbeatTicker = time.NewTicker(effective.heartbeat)
				heartbeatC = heartbeatTicker.C
				defer heartbeatTicker.Stop()
			}

			for {
				select {
				case <-ctx.Context().Done():
					return
				case now := <-heartbeatC:
					if err := writeHeartbeatFrame(writer, heartbeatFrame{
						Timestamp: now.UTC().Format(time.RFC3339Nano),
						ScopeKey:  sub.ScopeKey,
					}); err != nil {
						_ = writer.CloseWithError(err)
						return
					}
				case record, ok := <-sub.Records:
					if !ok {
						return
					}
					if err := writeRecordFrame(writer, record); err != nil {
						_ = writer.CloseWithError(err)
						return
					}
				}
			}
		}()

		return ctx.SendStream(reader)
	}
}

// WithPath overrides the mounted route path.
func WithPath(path string) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.Path = path
		}
	}
}

// WithStream configures the backing event stream.
func WithStream(stream eventstream.Stream) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.Stream = stream
		}
	}
}

// WithScopeResolver configures request-to-scope resolution.
func WithScopeResolver(resolver ScopeResolver) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.ScopeResolver = resolver
		}
	}
}

// WithMiddlewares appends middleware only for the mounted SSE route.
func WithMiddlewares(middlewares ...router.MiddlewareFunc) Option {
	return func(cfg *Config) {
		if cfg == nil || len(middlewares) == 0 {
			return
		}
		cfg.Middlewares = append(cfg.Middlewares, middlewares...)
	}
}

// WithHeartbeatInterval overrides the default heartbeat interval.
func WithHeartbeatInterval(interval time.Duration) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.HeartbeatInterval = interval
		}
	}
}

// WithRetryInterval overrides the default retry interval.
func WithRetryInterval(interval time.Duration) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.RetryInterval = interval
		}
	}
}

// WithCursorQueryParam overrides the fallback cursor query parameter name.
func WithCursorQueryParam(name string) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.CursorQueryParam = name
		}
	}
}

// WithHeartbeatQueryParam overrides the heartbeat tuning query parameter name.
func WithHeartbeatQueryParam(name string) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.HeartbeatQueryParam = name
		}
	}
}

// WithRetryQueryParam overrides the retry tuning query parameter name.
func WithRetryQueryParam(name string) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.RetryQueryParam = name
		}
	}
}

// WithAllowClientTuning enables opt-in query-param tuning for heartbeat/retry.
func WithAllowClientTuning(allow bool) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.AllowClientTuning = allow
		}
	}
}

// WithHeartbeatBounds overrides accepted heartbeat tuning limits.
func WithHeartbeatBounds(min, max time.Duration) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.MinHeartbeatInterval = min
			cfg.MaxHeartbeatInterval = max
		}
	}
}

// WithRetryBounds overrides accepted retry tuning limits.
func WithRetryBounds(min, max time.Duration) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.MinRetryInterval = min
			cfg.MaxRetryInterval = max
		}
	}
}

func buildConfig(opts ...Option) Config {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	cfg.Path = normalizePath(cfg.Path, defaultPath)
	cfg.CursorQueryParam = normalizeQueryParam(cfg.CursorQueryParam, defaultCursorQueryParam)
	cfg.HeartbeatQueryParam = normalizeQueryParam(cfg.HeartbeatQueryParam, defaultHeartbeatQueryParam)
	cfg.RetryQueryParam = normalizeQueryParam(cfg.RetryQueryParam, defaultRetryQueryParam)
	cfg.MinHeartbeatInterval = normalizeDuration(cfg.MinHeartbeatInterval, defaultMinHeartbeatInterval)
	cfg.MaxHeartbeatInterval = normalizeDuration(cfg.MaxHeartbeatInterval, defaultMaxHeartbeatInterval)
	if cfg.MaxHeartbeatInterval < cfg.MinHeartbeatInterval {
		cfg.MaxHeartbeatInterval = cfg.MinHeartbeatInterval
	}
	cfg.MinRetryInterval = normalizeDuration(cfg.MinRetryInterval, defaultMinRetryInterval)
	cfg.MaxRetryInterval = normalizeDuration(cfg.MaxRetryInterval, defaultMaxRetryInterval)
	if cfg.MaxRetryInterval < cfg.MinRetryInterval {
		cfg.MaxRetryInterval = cfg.MinRetryInterval
	}
	cfg.HeartbeatInterval = clampDuration(
		normalizeDuration(cfg.HeartbeatInterval, defaultHeartbeatInterval),
		cfg.MinHeartbeatInterval,
		cfg.MaxHeartbeatInterval,
	)
	cfg.RetryInterval = clampDuration(
		normalizeDuration(cfg.RetryInterval, defaultRetryInterval),
		cfg.MinRetryInterval,
		cfg.MaxRetryInterval,
	)
	return cfg
}

func validateConfig(cfg Config) error {
	if cfg.Stream == nil {
		return fmt.Errorf("sse fiber handler requires event stream")
	}
	if cfg.ScopeResolver == nil {
		return fmt.Errorf("sse fiber handler requires scope resolver")
	}
	if strings.TrimSpace(cfg.Path) == "" {
		return fmt.Errorf("sse fiber handler requires path")
	}
	return nil
}

func writeRetryDirective(w io.Writer, retry time.Duration) error {
	if retry < 0 {
		retry = 0
	}
	_, err := fmt.Fprintf(w, "retry: %d\n\n", retry.Milliseconds())
	return err
}

func writeGapFrame(w io.Writer, payload streamGapFrame) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeFrame(w, "", "stream_gap", encoded)
}

func writeHeartbeatFrame(w io.Writer, payload heartbeatFrame) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return writeFrame(w, "", "heartbeat", encoded)
}

func writeRecordFrame(w io.Writer, record eventstream.Record) error {
	payload, err := json.Marshal(struct {
		Name      string            `json:"name"`
		Payload   json.RawMessage   `json:"payload,omitempty"`
		Timestamp time.Time         `json:"timestamp"`
		Metadata  map[string]string `json:"metadata,omitempty"`
	}{
		Name:      record.Event.Name,
		Payload:   record.Event.Payload,
		Timestamp: record.Event.Timestamp,
		Metadata:  record.Event.Metadata,
	})
	if err != nil {
		return err
	}
	return writeFrame(w, record.Cursor, record.Event.Name, payload)
}

func writeFrame(w io.Writer, id string, event string, data []byte) error {
	if id != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", id); err != nil {
			return err
		}
	}
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func resolveCursor(ctx router.Context, queryParam string) string {
	cursor := strings.TrimSpace(ctx.Header("Last-Event-ID"))
	if cursor != "" {
		return cursor
	}
	return strings.TrimSpace(ctx.Query(queryParam))
}

func resolveEffectiveConfig(cfg Config, ctx router.Context) effectiveConfig {
	effective := effectiveConfig{
		heartbeat: cfg.HeartbeatInterval,
		retry:     cfg.RetryInterval,
	}
	if !cfg.AllowClientTuning {
		return effective
	}

	if duration, ok := parseDurationMillis(ctx.Query(cfg.HeartbeatQueryParam)); ok {
		effective.heartbeat = clampDuration(duration, cfg.MinHeartbeatInterval, cfg.MaxHeartbeatInterval)
	}
	if duration, ok := parseDurationMillis(ctx.Query(cfg.RetryQueryParam)); ok {
		effective.retry = clampDuration(duration, cfg.MinRetryInterval, cfg.MaxRetryInterval)
	}
	return effective
}

func parseDurationMillis(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}

	millis, err := strconv.Atoi(raw)
	if err != nil || millis <= 0 {
		return 0, false
	}
	return time.Duration(millis) * time.Millisecond, true
}

func clampDuration(value time.Duration, min time.Duration, max time.Duration) time.Duration {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func normalizeDuration(value time.Duration, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func normalizeQueryParam(name string, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return fallback
	}
	return name
}

func cloneMiddlewares(in []router.MiddlewareFunc) []router.MiddlewareFunc {
	if len(in) == 0 {
		return nil
	}
	out := make([]router.MiddlewareFunc, len(in))
	copy(out, in)
	return out
}

func normalizePath(path string, fallback string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return fallback
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}
