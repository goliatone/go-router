package router

import (
	"context"
	"fmt"
	"maps"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

type fiberContext struct {
	ctx           *fiber.Ctx
	mergeStrategy RenderMergeStrategy
	handlers      []NamedHandler
	index         int
	store         ContextStore
	logger        Logger
	cachedCtx     context.Context
	httpReq       *http.Request
	httpRes       http.ResponseWriter
	meta          *fiberRequestMeta
}

// fiberRequestMeta caches request data needed after fasthttp hijacks the connection.
type fiberRequestMeta struct {
	method      string
	path        string
	originalURL string
	ip          string
	host        string
	port        string
	headers     map[string]string
	queries     map[string]string
	params      map[string]string
	cookies     map[string]string
}

type fasthttpResponseWriter struct {
	ctx         *fasthttp.RequestCtx
	header      http.Header
	wroteHeader bool
	wroteBody   bool
}

func (w *fasthttpResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *fasthttpResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	for k, vals := range w.Header() {
		if len(vals) == 0 {
			continue
		}
		if len(vals) == 1 {
			w.ctx.Response.Header.Set(k, vals[0])
			continue
		}
		for _, v := range vals {
			w.ctx.Response.Header.Add(k, v)
		}
	}
	w.ctx.Response.SetStatusCode(status)
}

func (w *fasthttpResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.wroteBody = true
	return w.ctx.Write(p)
}

func (w *fasthttpResponseWriter) Finalize() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
}

func NewFiberContext(c *fiber.Ctx, logger Logger) Context {
	return &fiberContext{
		ctx:           c,
		mergeStrategy: defaultRenderMergeStrategy,
		index:         -1,
		store:         NewContextStore(),
		logger:        logger,
	}
}

func (c *fiberContext) setHandlers(h []NamedHandler) {
	c.handlers = h
}

func (c *fiberContext) setMergeStrategy(strategy RenderMergeStrategy) {
	if strategy != nil {
		c.mergeStrategy = strategy
	}
}

// liveCtx returns the underlying fiber.Ctx only when the fasthttp.RequestCtx
// is still attached (i.e., before websocket hijack). This prevents nil
// dereferences when fiber has upgraded the connection and cleared the
// RequestCtx.
func (c *fiberContext) liveCtx() *fiber.Ctx {
	if c == nil || c.ctx == nil {
		return nil
	}
	if c.ctx.Context() == nil {
		return nil
	}
	return c.ctx
}

func (c *fiberContext) Request() *http.Request {
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
	if err := fasthttpadaptor.ConvertRequest(ctx.Context(), req, true); err != nil {
		return nil
	}
	req = req.WithContext(c.Context())
	c.httpReq = req
	return req
}

func (c *fiberContext) Response() http.ResponseWriter {
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
	c.httpRes = &fasthttpResponseWriter{ctx: ctx.Context()}
	return c.httpRes
}

// captureRequestMeta stores request data before fasthttp hijacks the connection
// so websocket handlers can safely access metadata later.
func (c *fiberContext) captureRequestMeta() {
	if c.meta != nil {
		return
	}

	ctx := c.liveCtx()
	if ctx == nil {
		return
	}

	meta := &fiberRequestMeta{
		headers: make(map[string]string),
		queries: make(map[string]string),
		params:  make(map[string]string),
		cookies: make(map[string]string),
	}
	c.meta = meta

	meta.method = ctx.Method()
	meta.path = ctx.Path()
	meta.originalURL = ctx.OriginalURL()
	meta.ip = ctx.IP()
	meta.host = ctx.Hostname()
	meta.port = ctx.Port()

	ctx.Request().Header.VisitAll(func(key, value []byte) {
		meta.headers[string(key)] = string(value)
	})

	args := ctx.Request().URI().QueryArgs()
	args.VisitAll(func(key, value []byte) {
		meta.queries[string(key)] = string(value)
	})

	for k, v := range ctx.AllParams() {
		meta.params[k] = v
	}

	ctx.Request().Header.VisitAllCookie(func(key, value []byte) {
		meta.cookies[string(key)] = string(value)
	})
}

func (c *fiberContext) getMeta() *fiberRequestMeta {
	if c.meta == nil {
		c.captureRequestMeta()
	}
	return c.meta
}

// Context methods

func (c *fiberContext) Locals(key any, value ...any) any {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Locals(key, value...)
	}
	return nil
}

func (c *fiberContext) LocalsMerge(key any, value map[string]any) map[string]any {
	ctx := c.liveCtx()
	if ctx == nil {
		return value
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

func (c *fiberContext) Render(name string, bind any, layouts ...string) error {
	ctx := c.liveCtx()
	if ctx == nil {
		return fmt.Errorf("context unavailable")
	}

	merged, err := MergeLocalsWithViewData(ctx, c.logger, c.mergeStrategy, bind)
	if err != nil {
		return err
	}

	return ctx.Render(name, merged, layouts...)
}

// MergeLocalsWithViewData combines a render bind value with Fiber locals using the provided strategy.
func MergeLocalsWithViewData(ctx *fiber.Ctx, logger Logger, strategy RenderMergeStrategy, bind any) (map[string]any, error) {
	if strategy == nil {
		strategy = defaultRenderMergeStrategy
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
		ctx.Context().VisitUserValues(func(key []byte, val any) {
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

func (c *fiberContext) Body() []byte {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Body()
	}
	return nil
}

func (c *fiberContext) Method() string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Method()
	}
	if meta := c.getMeta(); meta != nil {
		return meta.method
	}
	return ""
}
func (c *fiberContext) Path() string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Path()
	}
	if meta := c.getMeta(); meta != nil {
		return meta.path
	}
	return ""
}

func (c *fiberContext) Param(name string, defaultValue ...string) string {
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

func (c *fiberContext) IP() string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.IP()
	}
	if meta := c.getMeta(); meta != nil {
		return meta.ip
	}
	return ""
}

func (c *fiberContext) Cookie(cookie *Cookie) {
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

func (c *fiberContext) Cookies(key string, defaultValue ...string) string {
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

func (c *fiberContext) CookieParser(out any) error {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.CookieParser(out)
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberContext) Redirect(location string, status ...int) error {
	ctx := c.liveCtx()
	if ctx == nil {
		return fmt.Errorf("context unavailable")
	}
	code := http.StatusFound // default 302
	if len(status) > 0 {
		code = status[0]
	}
	c.logger.Info("redirect request", "location", location, "code", code)
	return ctx.Redirect(location, code)
}

func (c *fiberContext) RedirectToRoute(routeName string, params ViewContext, status ...int) error {
	ctx := c.liveCtx()
	if ctx == nil {
		return fmt.Errorf("context unavailable")
	}
	return ctx.RedirectToRoute(routeName, params.asFiberMap(), status...)
}

func (c *fiberContext) RedirectBack(fallback string, status ...int) error {
	ctx := c.liveCtx()
	if ctx == nil {
		return fmt.Errorf("context unavailable")
	}
	return ctx.RedirectBack(fallback, status...)
}

func (c *fiberContext) ParamsInt(name string, defaultValue int) int {
	if ctx := c.liveCtx(); ctx != nil {
		if out, err := ctx.ParamsInt(name, defaultValue); err == nil {
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

func (c *fiberContext) Query(name string, defaultValue ...string) string {
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

func (c *fiberContext) QueryInt(name string, defaultValue int) int {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.QueryInt(name, defaultValue)
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

func (c *fiberContext) Queries() map[string]string {
	if ctx := c.liveCtx(); ctx != nil {
		queries := make(map[string]string)
		args := ctx.Request().URI().QueryArgs()
		args.VisitAll(func(key, value []byte) {
			queries[string(key)] = string(value)
		})
		return queries
	}
	if meta := c.getMeta(); meta != nil && meta.queries != nil {
		out := make(map[string]string, len(meta.queries))
		for k, v := range meta.queries {
			out[k] = v
		}
		return out
	}
	return map[string]string{}
}

func (c *fiberContext) Status(code int) Context {
	if ctx := c.liveCtx(); ctx != nil {
		ctx.Status(code)
	}
	return c
}

func (c *fiberContext) SendStatus(code int) error {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.SendStatus(code)
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberContext) SendString(body string) error {
	return c.Send([]byte(body))
}

func (c *fiberContext) Send(body []byte) error {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Send(body)
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberContext) JSON(code int, v any) error {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Status(code).JSON(v)
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberContext) NoContent(code int) error {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.SendStatus(code)
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberContext) Bind(v any) error {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.BodyParser(v)
	}
	return fmt.Errorf("context unavailable")
}

func (c *fiberContext) Context() context.Context {
	if c.cachedCtx != nil {
		return c.cachedCtx
	}
	if ctx := c.liveCtx(); ctx != nil {
		if uc := ctx.UserContext(); uc != nil {
			c.cachedCtx = uc
			return uc
		}
	}
	return context.Background()
}

func (c *fiberContext) SetContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.cachedCtx = ctx
	if live := c.liveCtx(); live != nil {
		live.SetUserContext(ctx)
	}
}

func (c *fiberContext) Header(key string) string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Get(key)
	}
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.headers[key]; ok {
			return val
		}
	}
	return ""
}

func (c *fiberContext) Referer() string {
	if referrer := c.Header("Referer"); referrer != "" {
		return referrer
	}
	return c.Header("Referrer")
}

func (c *fiberContext) OriginalURL() string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.OriginalURL()
	}
	if meta := c.getMeta(); meta != nil {
		return meta.originalURL
	}
	return ""
}

func (c *fiberContext) FormFile(key string) (*multipart.FileHeader, error) {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.FormFile(key)
	}
	return nil, fmt.Errorf("context unavailable")
}

func (c *fiberContext) FormValue(key string, defaultValues ...string) string {
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.FormValue(key, defaultValues...)
	}
	if len(defaultValues) > 0 {
		return defaultValues[0]
	}
	return ""
}

func (c *fiberContext) SetHeader(key string, value string) Context {
	if ctx := c.liveCtx(); ctx != nil {
		ctx.Set(key, value)
	}
	return c
}

func (c *fiberContext) Set(key string, value any) {
	c.store.Set(key, value)
}

func (c *fiberContext) Get(key string, def any) any {
	return c.store.Get(key, def)
}

func (c *fiberContext) GetString(key string, def string) string {
	return c.store.GetString(key, def)
}

func (c *fiberContext) GetInt(key string, def int) int {
	return c.store.GetInt(key, def)
}

func (c *fiberContext) GetBool(key string, def bool) bool {
	return c.store.GetBool(key, def)
}

func (c *fiberContext) Next() error {
	c.index++
	if c.index >= len(c.handlers) {
		return nil
	}

	return c.handlers[c.index].Handler(c)
}

// RouteName returns the route name from context
func (c *fiberContext) RouteName() string {
	if name, ok := RouteNameFromContext(c.Context()); ok {
		return name
	}
	return ""
}

// RouteParams returns all route parameters as a map
func (c *fiberContext) RouteParams() map[string]string {
	if params, ok := RouteParamsFromContext(c.Context()); ok {
		return params
	}
	return make(map[string]string)
}
