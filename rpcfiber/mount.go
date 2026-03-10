package rpcfiber

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/gofiber/fiber/v2"
	cmdrpc "github.com/goliatone/go-command/rpc"
	"github.com/goliatone/go-router"
)

var requestMetaType = reflect.TypeFor[cmdrpc.RequestMeta]()

// MethodServer is the minimum go-command/rpc server surface required by the Fiber mount.
type MethodServer interface {
	Invoke(context.Context, string, any) (any, error)
	NewRequestForMethod(string) (any, error)
	EndpointsMeta() []cmdrpc.Endpoint
}

// MetaExtractor maps request state into rpc.RequestMeta.
type MetaExtractor func(router.Context, *cmdrpc.RequestMeta)

// BeforeInvokeHook runs after payload decode/meta merge and before RPC invoke.
type BeforeInvokeHook func(router.Context, string, any) error

// AfterInvokeHook runs after RPC invoke (success or failure).
type AfterInvokeHook func(router.Context, string, any, error)

// Option mutates MountFiber configuration.
type Option func(*Config)

// Config controls route mounting and metadata extraction behavior.
type Config struct {
	InvokePath    string
	EndpointsPath string

	MetaExtractors []MetaExtractor
	BeforeInvoke   BeforeInvokeHook
	AfterInvoke    AfterInvokeHook
}

func defaultConfig() Config {
	return Config{
		InvokePath:    "/api/rpc",
		EndpointsPath: "/api/rpc/endpoints",
		MetaExtractors: []MetaExtractor{
			ExtractMetaFromHeaders,
			ExtractMetaFromQuery,
			ExtractMetaFromParams,
			ExtractMetaFromContext,
		},
	}
}

// WithInvokePath overrides the default invoke route path.
func WithInvokePath(path string) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.InvokePath = path
		}
	}
}

// WithEndpointsPath overrides the default discovery route path.
func WithEndpointsPath(path string) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.EndpointsPath = path
		}
	}
}

// WithMetaExtractor appends a metadata extractor hook.
func WithMetaExtractor(extractor MetaExtractor) Option {
	return func(cfg *Config) {
		if cfg == nil || extractor == nil {
			return
		}
		cfg.MetaExtractors = append(cfg.MetaExtractors, extractor)
	}
}

// WithMetaExtractors replaces metadata extractor hooks.
func WithMetaExtractors(extractors ...MetaExtractor) Option {
	return func(cfg *Config) {
		if cfg == nil {
			return
		}
		cfg.MetaExtractors = nil
		for _, extractor := range extractors {
			if extractor != nil {
				cfg.MetaExtractors = append(cfg.MetaExtractors, extractor)
			}
		}
	}
}

// WithBeforeInvokeHook sets a pre-invoke hook.
func WithBeforeInvokeHook(hook BeforeInvokeHook) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.BeforeInvoke = hook
		}
	}
}

// WithAfterInvokeHook sets a post-invoke hook.
func WithAfterInvokeHook(hook AfterInvokeHook) Option {
	return func(cfg *Config) {
		if cfg != nil {
			cfg.AfterInvoke = hook
		}
	}
}

// MountFiber mounts RPC invoke/discovery endpoints on a Fiber-backed go-router Router.
func MountFiber(r router.Router[*fiber.App], srv MethodServer, opts ...Option) error {
	if r == nil {
		return fmt.Errorf("rpc fiber mount requires router")
	}
	if srv == nil {
		return fmt.Errorf("rpc fiber mount requires method server")
	}

	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	cfg.InvokePath = normalizePath(cfg.InvokePath, "/api/rpc")
	cfg.EndpointsPath = normalizePath(cfg.EndpointsPath, "/api/rpc/endpoints")

	handler := mountHandler{server: srv, cfg: cfg}
	r.Post(cfg.InvokePath, handler.handleInvoke)
	r.Get(cfg.EndpointsPath, handler.handleEndpoints)
	return nil
}

type mountHandler struct {
	server MethodServer
	cfg    Config
}

type invokeRequest struct {
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func (r invokeRequest) rawPayload() json.RawMessage {
	if hasPayload(r.Params) {
		return r.Params
	}
	return r.Payload
}

func (h mountHandler) handleInvoke(ctx router.Context) error {
	var req invokeRequest
	if err := ctx.Bind(&req); err != nil {
		return writeRPCError(
			ctx,
			http.StatusBadRequest,
			"RPC_INVALID_REQUEST",
			"invalid RPC request payload",
			map[string]any{"cause": err.Error()},
		)
	}

	method := strings.TrimSpace(req.Method)
	if method == "" {
		method = strings.TrimSpace(ctx.Query("method", ""))
	}
	if method == "" {
		return writeRPCError(ctx, http.StatusBadRequest, "RPC_METHOD_REQUIRED", "rpc method required", nil)
	}

	prototype, err := h.server.NewRequestForMethod(method)
	if err != nil {
		return writeRPCError(
			ctx,
			http.StatusNotFound,
			"RPC_METHOD_NOT_FOUND",
			fmt.Sprintf("rpc method %q not found", method),
			map[string]any{"cause": err.Error()},
		)
	}

	payload, err := decodePayload(req.rawPayload(), prototype)
	if err != nil {
		return writeRPCError(
			ctx,
			http.StatusBadRequest,
			"RPC_INVALID_PARAMS",
			"invalid rpc params",
			map[string]any{"cause": err.Error()},
		)
	}

	transportMeta := extractMeta(ctx, h.cfg.MetaExtractors)
	payload = applyTransportMeta(payload, transportMeta)

	if h.cfg.BeforeInvoke != nil {
		if err := h.cfg.BeforeInvoke(ctx, method, payload); err != nil {
			return writeRPCError(
				ctx,
				http.StatusBadRequest,
				"RPC_PREINVOKE_REJECTED",
				"rpc pre-invoke hook rejected request",
				map[string]any{"cause": err.Error()},
			)
		}
	}

	result, invokeErr := h.server.Invoke(ctx.Context(), method, payload)
	if h.cfg.AfterInvoke != nil {
		h.cfg.AfterInvoke(ctx, method, result, invokeErr)
	}
	if invokeErr != nil {
		return writeRPCError(
			ctx,
			http.StatusOK,
			"RPC_INVOKE_FAILED",
			"rpc invocation failed",
			map[string]any{
				"method": method,
				"cause":  invokeErr.Error(),
			},
		)
	}

	return ctx.JSON(http.StatusOK, result)
}

func (h mountHandler) handleEndpoints(ctx router.Context) error {
	return ctx.JSON(http.StatusOK, map[string]any{
		"endpoints": h.server.EndpointsMeta(),
	})
}

func writeRPCError(ctx router.Context, status int, code, message string, details map[string]any) error {
	out := cmdrpc.ResponseEnvelope[any]{
		Error: &cmdrpc.Error{
			Code:    code,
			Message: message,
			Details: details,
		},
	}
	return ctx.JSON(status, out)
}

func extractMeta(ctx router.Context, extractors []MetaExtractor) cmdrpc.RequestMeta {
	meta := cmdrpc.RequestMeta{}
	for _, extractor := range extractors {
		if extractor != nil {
			extractor(ctx, &meta)
		}
	}
	return normalizeMeta(meta)
}

// ExtractMetaFromHeaders copies request headers and known identity fields from headers.
func ExtractMetaFromHeaders(ctx router.Context, meta *cmdrpc.RequestMeta) {
	if ctx == nil || meta == nil {
		return
	}

	req := requestFromContext(ctx)
	if req == nil {
		return
	}

	headers := make(map[string]string, len(req.Header))
	for key, values := range req.Header {
		if len(values) == 0 {
			continue
		}
		headers[key] = values[len(values)-1]
	}
	meta.Headers = mergeStringMap(meta.Headers, headers)

	if value := firstHeaderValue(req.Header, "X-Actor-ID", "Actor-ID", "Actor"); value != "" {
		meta.ActorID = value
	}
	if value := firstHeaderValue(req.Header, "X-Tenant-ID", "X-Tenant", "Tenant"); value != "" {
		meta.Tenant = value
	}
	if value := firstHeaderValue(req.Header, "X-Request-ID", "Request-ID"); value != "" {
		meta.RequestID = value
	}
	if value := firstHeaderValue(req.Header, "X-Correlation-ID", "Correlation-ID"); value != "" {
		meta.CorrelationID = value
	}
	if values := firstHeaderList(req.Header, "X-Roles", "Roles"); len(values) > 0 {
		meta.Roles = values
	}
	if values := firstHeaderList(req.Header, "X-Permissions", "Permissions"); len(values) > 0 {
		meta.Permissions = values
	}
	if scope := firstHeaderScope(req.Header, "X-Scope", "Scope"); len(scope) > 0 {
		meta.Scope = mergeAnyMap(meta.Scope, scope)
	}
}

// ExtractMetaFromQuery copies query string and known identity fields from query values.
func ExtractMetaFromQuery(ctx router.Context, meta *cmdrpc.RequestMeta) {
	if ctx == nil || meta == nil {
		return
	}

	query := queryFromContext(ctx)
	meta.Query = mergeQueryMap(meta.Query, query)

	if value := firstQueryValue(query, "actorId", "actor_id", "actor"); value != "" {
		meta.ActorID = value
	}
	if value := firstQueryValue(query, "tenant", "tenantId", "tenant_id"); value != "" {
		meta.Tenant = value
	}
	if value := firstQueryValue(query, "requestId", "request_id"); value != "" {
		meta.RequestID = value
	}
	if value := firstQueryValue(query, "correlationId", "correlation_id"); value != "" {
		meta.CorrelationID = value
	}
	if values := firstQueryList(query, "roles", "role"); len(values) > 0 {
		meta.Roles = values
	}
	if values := firstQueryList(query, "permissions", "permission"); len(values) > 0 {
		meta.Permissions = values
	}
	if scope := firstQueryScope(query, "scope"); len(scope) > 0 {
		meta.Scope = mergeAnyMap(meta.Scope, scope)
	}
}

// ExtractMetaFromParams copies route params and known identity fields from params.
func ExtractMetaFromParams(ctx router.Context, meta *cmdrpc.RequestMeta) {
	if ctx == nil || meta == nil {
		return
	}

	params := cloneStringMap(ctx.RouteParams())
	meta.Params = mergeStringMap(meta.Params, params)

	if value := firstMapValue(params, "actorId", "actor_id", "actor"); value != "" {
		meta.ActorID = value
	}
	if value := firstMapValue(params, "tenant", "tenantId", "tenant_id"); value != "" {
		meta.Tenant = value
	}
	if value := firstMapValue(params, "requestId", "request_id"); value != "" {
		meta.RequestID = value
	}
	if value := firstMapValue(params, "correlationId", "correlation_id"); value != "" {
		meta.CorrelationID = value
	}
	if value := firstMapValue(params, "roles", "role"); value != "" {
		meta.Roles = splitAndNormalizeList(value)
	}
	if value := firstMapValue(params, "permissions", "permission"); value != "" {
		meta.Permissions = splitAndNormalizeList(value)
	}
	if value := firstMapValue(params, "scope"); value != "" {
		if scope := parseScope(value); len(scope) > 0 {
			meta.Scope = mergeAnyMap(meta.Scope, scope)
		}
	}
}

// ExtractMetaFromContext reads known values from request context.
func ExtractMetaFromContext(ctx router.Context, meta *cmdrpc.RequestMeta) {
	if ctx == nil || meta == nil {
		return
	}

	base := ctx.Context()
	if base == nil {
		return
	}

	if headers, ok := firstContextStringMap(base, "rpc.headers", "headers"); ok {
		meta.Headers = mergeStringMap(meta.Headers, headers)
	}
	if params, ok := firstContextStringMap(base, "rpc.params", "params"); ok {
		meta.Params = mergeStringMap(meta.Params, params)
	}
	if query, ok := firstContextQueryMap(base, "rpc.query", "query"); ok {
		meta.Query = mergeQueryMap(meta.Query, query)
	}

	if value, ok := firstContextString(base, "rpc.actorId", "actorId", "actor_id"); ok {
		meta.ActorID = value
	}
	if value, ok := firstContextString(base, "rpc.tenant", "tenant", "tenant_id"); ok {
		meta.Tenant = value
	}
	if value, ok := firstContextString(base, "rpc.requestId", "requestId", "request_id"); ok {
		meta.RequestID = value
	}
	if value, ok := firstContextString(base, "rpc.correlationId", "correlationId", "correlation_id"); ok {
		meta.CorrelationID = value
	}
	if values, ok := firstContextStringSlice(base, "rpc.roles", "roles"); ok {
		meta.Roles = values
	}
	if values, ok := firstContextStringSlice(base, "rpc.permissions", "permissions"); ok {
		meta.Permissions = values
	}
	if scope, ok := firstContextScope(base, "rpc.scope", "scope"); ok {
		meta.Scope = mergeAnyMap(meta.Scope, scope)
	}
}

func decodePayload(raw json.RawMessage, prototype any) (any, error) {
	if prototype == nil {
		if hasPayload(raw) {
			return nil, errors.New("method does not accept params")
		}
		return nil, nil
	}
	if !hasPayload(raw) {
		return prototype, nil
	}

	value := reflect.ValueOf(prototype)
	if !value.IsValid() {
		return nil, errors.New("invalid method request type")
	}

	if value.Kind() == reflect.Ptr {
		if err := json.Unmarshal(raw, prototype); err != nil {
			return nil, err
		}
		return prototype, nil
	}

	target := reflect.New(value.Type())
	if err := json.Unmarshal(raw, target.Interface()); err != nil {
		return nil, err
	}
	return target.Elem().Interface(), nil
}

func hasPayload(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

func applyTransportMeta(payload any, extracted cmdrpc.RequestMeta) any {
	if payload == nil || isZeroMeta(extracted) {
		return payload
	}

	value := reflect.ValueOf(payload)
	if !value.IsValid() {
		return payload
	}

	if value.Kind() != reflect.Ptr {
		ptr := reflect.New(value.Type())
		ptr.Elem().Set(value)
		payload = ptr.Interface()
		value = reflect.ValueOf(payload)
	}
	if value.IsNil() {
		return payload
	}

	elem := value.Elem()
	if elem.Kind() != reflect.Struct {
		return payload
	}

	metaField := elem.FieldByName("Meta")
	if !metaField.IsValid() || !metaField.CanSet() || metaField.Type() != requestMetaType {
		return payload
	}

	currentMeta, ok := metaField.Interface().(cmdrpc.RequestMeta)
	if !ok {
		return payload
	}

	metaField.Set(reflect.ValueOf(mergeRequestMeta(extracted, currentMeta)))
	return payload
}

func normalizeMeta(meta cmdrpc.RequestMeta) cmdrpc.RequestMeta {
	meta.ActorID = strings.TrimSpace(meta.ActorID)
	meta.Tenant = strings.TrimSpace(meta.Tenant)
	meta.RequestID = strings.TrimSpace(meta.RequestID)
	meta.CorrelationID = strings.TrimSpace(meta.CorrelationID)
	meta.Roles = dedupeStrings(meta.Roles)
	meta.Permissions = dedupeStrings(meta.Permissions)
	return meta
}

func isZeroMeta(meta cmdrpc.RequestMeta) bool {
	return strings.TrimSpace(meta.ActorID) == "" &&
		strings.TrimSpace(meta.Tenant) == "" &&
		strings.TrimSpace(meta.RequestID) == "" &&
		strings.TrimSpace(meta.CorrelationID) == "" &&
		len(meta.Roles) == 0 &&
		len(meta.Permissions) == 0 &&
		len(meta.Scope) == 0 &&
		len(meta.Headers) == 0 &&
		len(meta.Params) == 0 &&
		len(meta.Query) == 0
}

func mergeRequestMeta(base, override cmdrpc.RequestMeta) cmdrpc.RequestMeta {
	out := cloneRequestMeta(base)

	if value := strings.TrimSpace(override.ActorID); value != "" {
		out.ActorID = value
	}
	if value := strings.TrimSpace(override.Tenant); value != "" {
		out.Tenant = value
	}
	if value := strings.TrimSpace(override.RequestID); value != "" {
		out.RequestID = value
	}
	if value := strings.TrimSpace(override.CorrelationID); value != "" {
		out.CorrelationID = value
	}
	if len(override.Roles) > 0 {
		out.Roles = cloneStrings(override.Roles)
	}
	if len(override.Permissions) > 0 {
		out.Permissions = cloneStrings(override.Permissions)
	}

	out.Scope = mergeAnyMap(out.Scope, override.Scope)
	out.Headers = mergeStringMap(out.Headers, override.Headers)
	out.Params = mergeStringMap(out.Params, override.Params)
	out.Query = mergeQueryMap(out.Query, override.Query)
	return normalizeMeta(out)
}

func cloneRequestMeta(meta cmdrpc.RequestMeta) cmdrpc.RequestMeta {
	return cmdrpc.RequestMeta{
		ActorID:       strings.TrimSpace(meta.ActorID),
		Roles:         cloneStrings(meta.Roles),
		Tenant:        strings.TrimSpace(meta.Tenant),
		RequestID:     strings.TrimSpace(meta.RequestID),
		CorrelationID: strings.TrimSpace(meta.CorrelationID),
		Permissions:   cloneStrings(meta.Permissions),
		Scope:         cloneAnyMap(meta.Scope),
		Headers:       cloneStringMap(meta.Headers),
		Params:        cloneStringMap(meta.Params),
		Query:         cloneQueryMap(meta.Query),
	}
}

func requestFromContext(ctx router.Context) *http.Request {
	httpCtx, ok := router.AsHTTPContext(ctx)
	if !ok || httpCtx == nil {
		return nil
	}
	return httpCtx.Request()
}

func queryFromContext(ctx router.Context) map[string][]string {
	req := requestFromContext(ctx)
	if req != nil && req.URL != nil {
		out := make(map[string][]string, len(req.URL.Query()))
		for key, values := range req.URL.Query() {
			out[key] = cloneStrings(values)
		}
		return out
	}

	queries := ctx.Queries()
	out := make(map[string][]string, len(queries))
	for key, value := range queries {
		out[key] = []string{value}
	}
	return out
}

func firstHeaderValue(headers http.Header, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(headers.Get(key))
		if value != "" {
			return value
		}
	}
	return ""
}

func firstHeaderList(headers http.Header, keys ...string) []string {
	for _, key := range keys {
		values := headers.Values(key)
		parsed := splitAndNormalizeList(values...)
		if len(parsed) > 0 {
			return parsed
		}
	}
	return nil
}

func firstHeaderScope(headers http.Header, keys ...string) map[string]any {
	for _, key := range keys {
		for _, value := range headers.Values(key) {
			if scope := parseScope(value); len(scope) > 0 {
				return scope
			}
		}
	}
	return nil
}

func firstQueryValue(query map[string][]string, keys ...string) string {
	for _, key := range keys {
		values := query[key]
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func firstQueryList(query map[string][]string, keys ...string) []string {
	for _, key := range keys {
		values := query[key]
		if len(values) == 0 {
			continue
		}
		parsed := splitAndNormalizeList(values...)
		if len(parsed) > 0 {
			return parsed
		}
	}
	return nil
}

func firstQueryScope(query map[string][]string, keys ...string) map[string]any {
	for _, key := range keys {
		values := query[key]
		for _, value := range values {
			if scope := parseScope(value); len(scope) > 0 {
				return scope
			}
		}
	}
	return nil
}

func firstMapValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(values[key])
		if value != "" {
			return value
		}
	}
	return ""
}

func firstContextString(ctx context.Context, keys ...any) (string, bool) {
	for _, key := range keys {
		if value, ok := contextValue(ctx, key); ok {
			if out, ok := asString(value); ok {
				return out, true
			}
		}
	}
	return "", false
}

func firstContextStringSlice(ctx context.Context, keys ...any) ([]string, bool) {
	for _, key := range keys {
		if value, ok := contextValue(ctx, key); ok {
			if out, ok := asStringSlice(value); ok && len(out) > 0 {
				return out, true
			}
		}
	}
	return nil, false
}

func firstContextScope(ctx context.Context, keys ...any) (map[string]any, bool) {
	for _, key := range keys {
		if value, ok := contextValue(ctx, key); ok {
			if out := parseScope(value); len(out) > 0 {
				return out, true
			}
		}
	}
	return nil, false
}

func firstContextStringMap(ctx context.Context, keys ...any) (map[string]string, bool) {
	for _, key := range keys {
		if value, ok := contextValue(ctx, key); ok {
			if out, ok := asStringMap(value); ok && len(out) > 0 {
				return out, true
			}
		}
	}
	return nil, false
}

func firstContextQueryMap(ctx context.Context, keys ...any) (map[string][]string, bool) {
	for _, key := range keys {
		if value, ok := contextValue(ctx, key); ok {
			if out, ok := asQueryMap(value); ok && len(out) > 0 {
				return out, true
			}
		}
	}
	return nil, false
}

func contextValue(ctx context.Context, key any) (any, bool) {
	if ctx == nil || key == nil {
		return nil, false
	}
	value := ctx.Value(key)
	if value == nil {
		return nil, false
	}
	return value, true
}

func asString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		typed = strings.TrimSpace(typed)
		return typed, typed != ""
	case []byte:
		out := strings.TrimSpace(string(typed))
		return out, out != ""
	case fmt.Stringer:
		out := strings.TrimSpace(typed.String())
		return out, out != ""
	default:
		return "", false
	}
}

func asStringSlice(value any) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		out := splitAndNormalizeList(typed...)
		return out, len(out) > 0
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := asString(item); ok {
				parts = append(parts, str)
			}
		}
		out := splitAndNormalizeList(parts...)
		return out, len(out) > 0
	case string:
		out := splitAndNormalizeList(typed)
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func asStringMap(value any) (map[string]string, bool) {
	switch typed := value.(type) {
	case map[string]string:
		out := cloneStringMap(typed)
		return out, len(out) > 0
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, raw := range typed {
			if str, ok := asString(raw); ok {
				out[key] = str
			}
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func asQueryMap(value any) (map[string][]string, bool) {
	switch typed := value.(type) {
	case map[string][]string:
		out := cloneQueryMap(typed)
		return out, len(out) > 0
	case map[string]string:
		out := make(map[string][]string, len(typed))
		for key, raw := range typed {
			if str := strings.TrimSpace(raw); str != "" {
				out[key] = []string{str}
			}
		}
		return out, len(out) > 0
	case map[string]any:
		out := make(map[string][]string, len(typed))
		for key, raw := range typed {
			values, ok := asStringSlice(raw)
			if !ok {
				if str, ok := asString(raw); ok {
					values = []string{str}
				}
			}
			if len(values) > 0 {
				out[key] = values
			}
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func parseScope(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, entry := range typed {
			out[key] = entry
		}
		return out
	case string:
		raw := strings.TrimSpace(typed)
		if raw == "" {
			return nil
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(raw), &out); err == nil && len(out) > 0 {
			return out
		}
	case []byte:
		return parseScope(string(typed))
	}
	return nil
}

func splitAndNormalizeList(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return dedupeStrings(out)
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeAnyMap(base, update map[string]any) map[string]any {
	switch {
	case len(base) == 0 && len(update) == 0:
		return nil
	case len(base) == 0:
		return cloneAnyMap(update)
	}

	out := cloneAnyMap(base)
	for key, value := range update {
		out[key] = value
	}
	return out
}

func mergeStringMap(base, update map[string]string) map[string]string {
	switch {
	case len(base) == 0 && len(update) == 0:
		return nil
	case len(base) == 0:
		return cloneStringMap(update)
	}

	out := cloneStringMap(base)
	for key, value := range update {
		out[key] = value
	}
	return out
}

func mergeQueryMap(base, update map[string][]string) map[string][]string {
	switch {
	case len(base) == 0 && len(update) == 0:
		return nil
	case len(base) == 0:
		return cloneQueryMap(update)
	}

	out := cloneQueryMap(base)
	for key, values := range update {
		out[key] = cloneStrings(values)
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneQueryMap(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, values := range in {
		out[key] = cloneStrings(values)
	}
	return out
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func normalizePath(path, fallback string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = fallback
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
