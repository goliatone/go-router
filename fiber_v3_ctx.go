package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

type fiberV3Context struct {
	ctx           fiber.Ctx
	mergeStrategy RenderMergeStrategy
	handlers      []NamedHandler
	index         int
	store         ContextStore
	logger        Logger
	cachedCtx     context.Context
	httpReq       *http.Request
	httpRes       http.ResponseWriter
	meta          *fiberRequestMeta
	statusCode    int
	written       bool
	bodySize      int64
	stream        bool
	localsMu      sync.RWMutex
	locals        map[any]any
}

func NewFiberV3Context(c fiber.Ctx, logger Logger) Context {
	return &fiberV3Context{
		ctx:           c,
		mergeStrategy: defaultFiberV3RenderMergeStrategy,
		index:         -1,
		store:         NewContextStore(),
		logger:        logger,
		statusCode:    http.StatusOK,
	}
}

func (c *fiberV3Context) setHandlers(h []NamedHandler) {
	c.handlers = h
}

func (c *fiberV3Context) setMergeStrategy(strategy RenderMergeStrategy) {
	if strategy != nil {
		c.mergeStrategy = strategy
	}
}

// liveCtx returns the underlying fiber.Ctx only when the fasthttp.RequestCtx
// is still attached (i.e., before websocket hijack). This prevents nil
// dereferences when fiber has upgraded the connection and cleared the
// RequestCtx.
func (c *fiberV3Context) liveCtx() fiber.Ctx {
	if c == nil || c.ctx == nil {
		return nil
	}
	if c.ctx.RequestCtx() == nil {
		return nil
	}
	return c.ctx
}

func (c *fiberV3Context) Request() *http.Request {
	if c == nil {
		return nil
	}
	if c.httpReq != nil {
		return c.httpReq
	}
	ctx := c.liveCtx()
	if ctx == nil {
		return nil
	}
	req := &http.Request{}
	if err := fasthttpadaptor.ConvertRequest(ctx.RequestCtx(), req, true); err != nil {
		return nil
	}
	req = req.WithContext(c.Context())
	c.httpReq = req
	return req
}

func (c *fiberV3Context) Response() http.ResponseWriter {
	if c == nil {
		return nil
	}
	if c.httpRes != nil {
		return c.httpRes
	}
	ctx := c.liveCtx()
	if ctx == nil {
		return nil
	}
	c.httpRes = &fasthttpResponseWriter{
		ctx:     ctx.RequestCtx(),
		onWrite: func() { c.syncNativeResponseState(true) },
	}
	return c.httpRes
}

// captureRequestMeta stores request data before fasthttp hijacks the connection
// so websocket handlers can safely access metadata later.
func (c *fiberV3Context) captureRequestMeta() {
	if c.meta != nil {
		return
	}

	ctx := c.liveCtx()
	if ctx == nil {
		return
	}

	meta := &fiberRequestMeta{
		headers:      make(map[string]string),
		queries:      make(map[string]string),
		queriesMulti: make(map[string][]string),
		params:       make(map[string]string),
		cookies:      make(map[string]string),
	}
	c.meta = meta

	meta.method = strings.Clone(ctx.Method())
	meta.path = strings.Clone(ctx.Path())
	meta.originalURL = strings.Clone(ctx.OriginalURL())
	meta.ip = strings.Clone(ctx.IP())
	meta.host = strings.Clone(ctx.Hostname())
	meta.port = strings.Clone(ctx.Port())

	for key, value := range ctx.Request().Header.All() {
		meta.headers[strings.ToLower(string(key))] = string(value)
	}

	args := ctx.Request().URI().QueryArgs()
	for key, value := range args.All() {
		keyStr := string(key)
		meta.queriesMulti[keyStr] = append(meta.queriesMulti[keyStr], string(value))
		meta.queries[keyStr] = string(value)
	}

	maps.Copy(meta.params, fiberV3Params(ctx))

	for key, value := range ctx.Request().Header.Cookies() {
		meta.cookies[string(key)] = string(value)
	}
}

func (c *fiberV3Context) getMeta() *fiberRequestMeta {
	if c.meta == nil {
		c.captureRequestMeta()
	}
	return c.meta
}

// Context methods

func (c *fiberV3Context) Locals(key any, value ...any) any {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Locals(key, value...)
	}
	key = detachedLocalKey(key)
	c.localsMu.Lock()
	defer c.localsMu.Unlock()
	if len(value) > 0 {
		if c.locals == nil {
			c.locals = make(map[any]any)
		}
		c.locals[key] = value[0]
	}
	return c.locals[key]
}

func (c *fiberV3Context) LocalsMerge(key any, value map[string]any) map[string]any {
	ctx := c.liveCtx()
	if ctx == nil {
		key = detachedLocalKey(key)
		c.localsMu.Lock()
		defer c.localsMu.Unlock()
		if c.locals == nil {
			c.locals = make(map[any]any)
		}
		merged := make(map[string]any)
		if existing, ok := c.locals[key].(map[string]any); ok {
			maps.Copy(merged, existing)
		}
		maps.Copy(merged, value)
		c.locals[key] = merged
		return merged
	}

	existing := ctx.Locals(key)

	if existing == nil {
		ctx.Locals(key, value)
		return value
	}

	if existingMap, ok := existing.(map[string]any); ok {
		// merge maps (new values override existing ones)
		merged := make(map[string]any)
		maps.Copy(merged, existingMap)
		maps.Copy(merged, value)
		ctx.Locals(key, merged)
		return merged
	}

	// existing value is not a map -> replace
	ctx.Locals(key, value)
	return value
}

func (c *fiberV3Context) Render(name string, bind any, layouts ...string) error {
	ctx := c.liveCtx()
	if ctx == nil {
		return fmt.Errorf("context unavailable")
	}

	merged, err := MergeFiberV3LocalsWithViewData(ctx, c.logger, c.mergeStrategy, bind)
	if err != nil {
		return err
	}

	if err := ctx.Render(name, merged, layouts...); err != nil {
		return err
	}
	c.syncFiberResponseState(true, false)
	return nil
}

func (c *fiberV3Context) RenderToWriter(w io.Writer, name string, bind any, layouts ...string) error {
	if w == nil {
		return fmt.Errorf("render: writer is nil")
	}

	ctx := c.liveCtx()
	if ctx == nil {
		return fmt.Errorf("context unavailable")
	}

	cfg := ctx.App().Config()
	if cfg.Views == nil {
		return fmt.Errorf("render: no template engine registered")
	}

	merged, err := MergeFiberV3LocalsWithViewData(ctx, c.logger, c.mergeStrategy, bind)
	if err != nil {
		return err
	}

	if len(layouts) == 0 && cfg.ViewsLayout != "" {
		layouts = []string{cfg.ViewsLayout}
	}

	return cfg.Views.Render(w, name, merged, layouts...)
}

func (c *fiberV3Context) RenderToBytes(name string, bind any, layouts ...string) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := c.RenderToWriter(buf, name, bind, layouts...); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MergeFiberV3LocalsWithViewData combines a render bind value with Fiber locals using the provided strategy.
func MergeFiberV3LocalsWithViewData(ctx fiber.Ctx, logger Logger, strategy RenderMergeStrategy, bind any) (map[string]any, error) {
	if strategy == nil {
		strategy = defaultFiberV3RenderMergeStrategy
	}

	var data map[string]any
	if bind == nil {
		data = map[string]any{}
	} else {
		serialized, err := SerializeAsContext(bind)
		if err != nil {
			return nil, fmt.Errorf("render: error serializing vars: %w", err)
		}
		data = serialized
		if data == nil {
			data = map[string]any{}
		}
	}

	if ctx != nil && ctx.App().Config().PassLocalsToViews {
		ctx.RequestCtx().VisitUserValues(func(key []byte, val any) {
			k := string(key)
			if existing, ok := data[k]; ok {
				if resolved, set := strategy(k, existing, val, logger); set {
					data[k] = resolved
				}
				return
			}
			data[k] = val
		})
	}

	return data, nil
}

func (c *fiberV3Context) Body() []byte {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Body()
	}
	return nil
}

func (c *fiberV3Context) Method() string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Method()
	}
	if meta := c.getMeta(); meta != nil {
		return meta.method
	}
	return ""
}
func (c *fiberV3Context) Path() string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Path()
	}
	if meta := c.getMeta(); meta != nil {
		return meta.path
	}
	return ""
}

func (c *fiberV3Context) Param(name string, defaultValue ...string) string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Params(name, defaultValue...)
	}
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.params[name]; ok {
			return val
		}
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (c *fiberV3Context) IP() string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.IP()
	}
	if meta := c.getMeta(); meta != nil {
		return meta.ip
	}
	return ""
}

func (c *fiberV3Context) Cookie(cookie *Cookie) {
	ctx := c.liveCtx()
	if ctx == nil {
		return
	}
	ctx.Cookie(&fiber.Cookie{
		Name:        cookie.Name,
		Value:       cookie.Value,
		Path:        cookie.Path,
		Domain:      cookie.Domain,
		MaxAge:      cookie.MaxAge,
		Expires:     cookie.Expires,
		Secure:      cookie.Secure,
		HTTPOnly:    cookie.HTTPOnly,
		SameSite:    cookie.SameSite,
		SessionOnly: cookie.SessionOnly,
	})
}

func (c *fiberV3Context) Cookies(key string, defaultValue ...string) string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Cookies(key, defaultValue...)
	}
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.cookies[key]; ok {
			return val
		}
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (c *fiberV3Context) CookieParser(out any) error {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Bind().WithoutAutoHandling().Cookie(out)
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberV3Context) Redirect(location string, status ...int) error {
	ctx := c.liveCtx()
	if ctx == nil {
		return fmt.Errorf("context unavailable")
	}
	code := http.StatusFound // default 302
	if len(status) > 0 {
		code = status[0]
	}
	c.logger.Info("redirect request", "location", location, "code", code)
	return ctx.Redirect().Status(code).To(location)
}

func (c *fiberV3Context) RedirectToRoute(routeName string, params ViewContext, status ...int) error {
	ctx := c.liveCtx()
	if ctx == nil {
		return fmt.Errorf("context unavailable")
	}
	code := http.StatusFound
	if len(status) > 0 {
		code = status[0]
	}
	return ctx.Redirect().Status(code).Route(routeName, fiber.RedirectConfig{Params: fiber.Map(params)})
}

func (c *fiberV3Context) RedirectBack(fallback string, status ...int) error {
	ctx := c.liveCtx()
	if ctx == nil {
		return fmt.Errorf("context unavailable")
	}
	return c.Redirect(resolveRedirectBackTarget(c, fallback), status...)
}

func (c *fiberV3Context) ParamsInt(name string, defaultValue int) int {
	if ctx := c.liveCtx(); ctx != nil {
		if out, err := strconv.Atoi(ctx.Params(name)); err == nil {
			return out
		}
	}
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.params[name]; ok {
			if out, err := strconv.Atoi(val); err == nil {
				return out
			}
		}
	}
	return defaultValue
}

func (c *fiberV3Context) Query(name string, defaultValue ...string) string {
	def := ""
	if len(defaultValue) > 0 {
		def = defaultValue[0]
	}
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Query(name, def)
	}
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.queries[name]; ok {
			return val
		}
	}
	return def
}

func (c *fiberV3Context) QueryValues(name string) []string {
	if ctx := c.liveCtx(); ctx != nil {
		args := ctx.Request().URI().QueryArgs()
		values := []string{}
		for key, value := range args.All() {
			if string(key) == name {
				values = append(values, string(value))
			}
		}
		if len(values) > 0 {
			return values
		}
	}
	if meta := c.getMeta(); meta != nil {
		if values, ok := meta.queriesMulti[name]; ok {
			out := make([]string, len(values))
			copy(out, values)
			return out
		}
		if val, ok := meta.queries[name]; ok {
			return []string{val}
		}
	}
	return []string{}
}

func (c *fiberV3Context) QueryInt(name string, defaultValue int) int {
	if ctx := c.liveCtx(); ctx != nil {
		if out, err := strconv.Atoi(ctx.Query(name)); err == nil {
			return out
		}
		return defaultValue
	}
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.queries[name]; ok {
			if out, err := strconv.Atoi(val); err == nil {
				return out
			}
		}
	}
	return defaultValue
}

func (c *fiberV3Context) Queries() map[string]string {
	if ctx := c.liveCtx(); ctx != nil {
		queries := make(map[string]string)
		args := ctx.Request().URI().QueryArgs()
		for key, value := range args.All() {
			queries[string(key)] = string(value)
		}
		return queries
	}
	if meta := c.getMeta(); meta != nil && meta.queries != nil {
		out := make(map[string]string, len(meta.queries))
		maps.Copy(out, meta.queries)
		return out
	}
	return map[string]string{}
}

func (c *fiberV3Context) Status(code int) Context {
	if ctx := c.liveCtx(); ctx != nil {
		ctx.Status(code)
	}
	if code > 0 {
		c.statusCode = code
	}
	return c
}

func (c *fiberV3Context) SendStatus(code int) error {
	if ctx := c.liveCtx(); ctx != nil {
		if err := ctx.SendStatus(code); err != nil {
			return err
		}
		c.syncFiberResponseState(true, false)
		return nil
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberV3Context) SendString(body string) error {
	return c.Send([]byte(body))
}

func (c *fiberV3Context) Send(body []byte) error {
	if ctx := c.liveCtx(); ctx != nil {
		if err := ctx.Send(body); err != nil {
			return err
		}
		c.syncFiberResponseState(true, false)
		return nil
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberV3Context) SendStream(r io.Reader) error {
	if ctx := c.liveCtx(); ctx != nil {
		if err := ctx.SendStream(r); err != nil {
			return err
		}
		c.syncFiberResponseState(true, true)
		return nil
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberV3Context) JSON(code int, v any) error {
	if ctx := c.liveCtx(); ctx != nil {
		if err := ctx.Status(code).JSON(v); err != nil {
			return err
		}
		c.syncFiberResponseState(true, false)
		return nil
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberV3Context) encodeResponseJSON(v any) ([]byte, error) {
	ctx := c.liveCtx()
	if ctx == nil {
		return nil, fmt.Errorf("context unavailable")
	}
	encoder := ctx.App().Config().JSONEncoder
	if encoder == nil {
		encoder = json.Marshal
	}
	return encoder(v)
}

func (c *fiberV3Context) NoContent(code int) error {
	if ctx := c.liveCtx(); ctx != nil {
		if err := ctx.SendStatus(code); err != nil {
			return err
		}
		c.statusCode = code
		c.written = true
		c.bodySize = 0
		c.stream = false
		return nil
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberV3Context) Bind(v any) error {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Bind().WithoutAutoHandling().Body(v)
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberV3Context) Context() context.Context {
	if c.cachedCtx != nil {
		return c.cachedCtx
	}
	if ctx := c.liveCtx(); ctx != nil {
		if uc := ctx.Context(); uc != nil {
			c.cachedCtx = uc
			return uc
		}
	}
	return context.Background()
}

func (c *fiberV3Context) SetContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.cachedCtx = ctx
	if c.httpReq != nil {
		c.httpReq = c.httpReq.WithContext(ctx)
	}
	if live := c.liveCtx(); live != nil {
		live.SetContext(ctx)
	}
}

func (c *fiberV3Context) Header(key string) string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Get(key)
	}
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.headers[strings.ToLower(key)]; ok {
			return val
		}
	}
	return ""
}

func detachedLocalKey(key any) any {
	switch key := key.(type) {
	case string:
		return strings.Clone(key)
	case []byte:
		return string(key)
	default:
		return key
	}
}

// snapshotLocals owns the local map and scalar request-backed buffers. Other
// reference values remain application-owned, just as values in ContextStore do.
func (c *fiberV3Context) snapshotLocals(ctx fiber.Ctx) {
	c.locals = make(map[any]any)
	ctx.RequestCtx().VisitUserValuesAll(func(key, value any) {
		// Dispatch bookkeeping contains live router/native contexts and must
		// never escape into an asynchronous callback.
		if _, internal := key.(fiberV3MiddlewareKey); internal {
			return
		}
		switch value := value.(type) {
		case string:
			c.locals[detachedLocalKey(key)] = strings.Clone(value)
		case []byte:
			c.locals[detachedLocalKey(key)] = bytes.Clone(value)
		default:
			c.locals[detachedLocalKey(key)] = value
		}
	})
}

func (c *fiberV3Context) Referer() string {
	if referrer := c.Header("Referer"); referrer != "" {
		return referrer
	}
	return c.Header("Referrer")
}

func (c *fiberV3Context) OriginalURL() string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.OriginalURL()
	}
	if meta := c.getMeta(); meta != nil {
		return meta.originalURL
	}
	return ""
}

func (c *fiberV3Context) FormFile(key string) (*multipart.FileHeader, error) {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.FormFile(key)
	}
	return nil, fmt.Errorf("context unavailable")
}

func (c *fiberV3Context) FormValue(key string, defaultValues ...string) string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.FormValue(key, defaultValues...)
	}
	if len(defaultValues) > 0 {
		return defaultValues[0]
	}
	return ""
}

func (c *fiberV3Context) SetHeader(key string, value string) Context {
	if ctx := c.liveCtx(); ctx != nil {
		ctx.Set(key, value)
	}
	return c
}

func (c *fiberV3Context) AppendResponseHeader(key string, value string) Context {
	if ctx := c.liveCtx(); ctx != nil {
		ctx.Response().Header.Add(key, value)
	}
	return c
}

func (c *fiberV3Context) StatusCode() int {
	if c == nil {
		return http.StatusOK
	}
	if c.statusCode > 0 {
		return c.statusCode
	}
	if ctx := c.liveCtx(); ctx != nil {
		if code := ctx.Response().StatusCode(); code > 0 {
			return code
		}
	}
	return http.StatusOK
}

func (c *fiberV3Context) ResponseHeaders() http.Header {
	headers := make(http.Header)
	if c == nil {
		return headers
	}
	if ctx := c.liveCtx(); ctx != nil {
		for key, value := range ctx.Response().Header.All() {
			headers.Add(string(key), string(value))
		}
	}
	return headers
}

func (c *fiberV3Context) ResponseWritten() bool {
	return c != nil && c.written
}

func (c *fiberV3Context) ResponseBodySize() int64 {
	if c == nil {
		return 0
	}
	return c.bodySize
}

func (c *fiberV3Context) ResponseIsStream() bool {
	return c != nil && c.stream
}

func (c *fiberV3Context) syncFiberResponseState(written bool, stream bool) {
	if c == nil {
		return
	}
	if ctx := c.liveCtx(); ctx != nil {
		if code := ctx.Response().StatusCode(); code > 0 {
			c.statusCode = code
		}
		if !stream {
			c.bodySize = int64(len(ctx.Response().Body()))
		}
	}
	if written {
		c.written = true
	}
	if stream {
		c.stream = true
	}
}

// syncNativeResponseState reconciles native middleware writes at the dispatch
// boundary. Body must never be called for streams: fasthttp would consume them.
func (c *fiberV3Context) syncNativeResponseState(handled bool) {
	ctx := c.liveCtx()
	if ctx == nil {
		return
	}
	response := ctx.Response()
	c.statusCode = response.StatusCode()
	c.stream = response.IsBodyStream()
	c.bodySize = 0
	if c.stream {
		c.written = true
	} else {
		body := response.Body()
		c.bodySize = int64(len(body))
		c.written = c.written || body != nil
	}
	c.written = c.written || handled
}

func (c *fiberV3Context) Set(key string, value any) {
	c.store.Set(key, value)
}

func (c *fiberV3Context) Get(key string, def any) any {
	return c.store.Get(key, def)
}

func (c *fiberV3Context) GetString(key string, def string) string {
	return c.store.GetString(key, def)
}

func (c *fiberV3Context) GetInt(key string, def int) int {
	return c.store.GetInt(key, def)
}

func (c *fiberV3Context) GetBool(key string, def bool) bool {
	return c.store.GetBool(key, def)
}

func (c *fiberV3Context) setHandlerIndex(index int) { c.index = index }

func (c *fiberV3Context) Next() error {
	c.index++
	if c.index >= len(c.handlers) {
		return nil
	}

	return c.handlers[c.index].Handler(c)
}

// RouteName returns the route name from context
func (c *fiberV3Context) RouteName() string {
	if name, ok := RouteNameFromContext(c.Context()); ok {
		return name
	}
	return ""
}

// RouteParams returns all route parameters as a map
func (c *fiberV3Context) RouteParams() map[string]string {
	if params, ok := RouteParamsFromContext(c.Context()); ok {
		return params
	}
	return make(map[string]string)
}

// Copy route values because native contexts and parameter backing storage are pooled.
func fiberV3Params(c fiber.Ctx) map[string]string {
	params := make(map[string]string)
	if c != nil && c.Route() != nil {
		for _, name := range c.Route().Params {
			params[name] = strings.Clone(c.Params(name))
		}
	}
	return params
}

func defaultFiberV3RenderMergeStrategy(key string, viewVal, localVal any, logger Logger) (any, bool) {
	if logger != nil {
		logger.Warn("render locals overwritten by view context", "key", key)
	}
	return viewVal, true
}
