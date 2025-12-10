package router

import (
	"context"
	"fmt"
	"maps"
	"mime/multipart"
	"net/http"
	"slices"
	"strconv"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

type FiberAdapter struct {
	mu            sync.Mutex
	app           *fiber.App
	initialized   bool
	router        *FiberRouter
	mergeStrategy RenderMergeStrategy
	opts          []func(*fiber.App) *fiber.App
}

type FiberAdapterConfig struct {
	MergeStrategy RenderMergeStrategy
}

func (cfg FiberAdapterConfig) withDefaults() FiberAdapterConfig {
	if cfg.MergeStrategy == nil {
		cfg.MergeStrategy = defaultRenderMergeStrategy
	}
	return cfg
}

// RenderMergeStrategy resolves collisions between view bind data and Fiber locals.
// The bool return controls whether the resolved value should be written.
type RenderMergeStrategy func(key string, viewVal, localVal any, logger Logger) (resolved any, set bool)

func defaultRenderMergeStrategy(key string, viewVal, localVal any, logger Logger) (any, bool) {
	if logger != nil {
		logger.Warn("render locals overwritten by view context", "key", key, "local_value", localVal, "view_value", viewVal)
	}
	return viewVal, true
}

func newFiberInstance() *fiber.App {
	return fiber.New(fiber.Config{
		UnescapePath:      true,
		EnablePrintRoutes: true,
		StrictRouting:     false,
		PassLocalsToViews: true,
		ErrorHandler:      DefaultFiberErrorHandler(DefaultFiberErrorHandlerConfig()),
	})
}

// TODO: We should make this asynchronous so that we can gather options in our
// app before we call fiber.New (since at that point it calls init and we are done)
func NewFiberAdapter(opts ...func(*fiber.App) *fiber.App) Server[*fiber.App] {
	return NewFiberAdapterWithConfig(FiberAdapterConfig{}, opts...)
}

// NewFiberAdapterWithConfig allows callers to override adapter-level settings, including render merge strategy.
func NewFiberAdapterWithConfig(cfg FiberAdapterConfig, opts ...func(*fiber.App) *fiber.App) Server[*fiber.App] {
	cfg = cfg.withDefaults()
	app := newFiberInstance()

	if len(opts) == 0 {
		opts = append(opts, DefaultFiberOptions)
	}

	for _, opt := range opts {
		app = opt(app)
	}

	return &FiberAdapter{
		app:           app,
		opts:          opts,
		mergeStrategy: cfg.MergeStrategy,
	}
}

func DefaultFiberOptions(app *fiber.App) *fiber.App {
	// app.Use(recover.New())
	app.Use(logger.New())
	return app
}

func (a *FiberAdapter) Router() Router[*fiber.App] {
	if a.router == nil {
		a.router = &FiberRouter{
			app:           a.app,
			mergeStrategy: a.mergeStrategy,
			BaseRouter: BaseRouter{
				logger: &defaultLogger{},
				root:   &routerRoot{},
			},
		}
	}
	return a.router
}

func (a *FiberAdapter) WrapHandler(h HandlerFunc) any {
	// Wrap a HandlerFunc into a fiber handler
	return func(c *fiber.Ctx) error {
		router := a.Router()
		ctx := NewFiberContext(c, router.(*FiberRouter).logger)
		if fc, ok := ctx.(*fiberContext); ok {
			fc.setMergeStrategy(a.mergeStrategy)
		}

		// Get path pattern from Fiber and inject route context
		if c.Route() != nil {
			pathPattern := c.Route().Path

			// Look up route name from go-router's namedRoutes
			if routeName, ok := a.router.RouteNameFromPath(c.Method(), pathPattern); ok {
				goCtx := ctx.Context()
				goCtx = WithRouteName(goCtx, routeName)
				goCtx = WithRouteParams(goCtx, c.AllParams())
				ctx.SetContext(goCtx)
			}
		}

		// c.Next() will work if c.handlers is set up at request time.
		return h(ctx)
	}
}

func (r *FiberRouter) Static(prefix, root string, config ...Static) Router[*fiber.App] {
	path, handler := r.makeStaticHandler(prefix, root, config...)
	// r.addRoute(GET, path+"/*", handler, "static.get", nil)
	// r.addRoute(HEAD, path+"/*", handler, "static.head", nil)
	r.addLateRoute(GET, path+"/*", handler, "static.get", func(hf HandlerFunc) HandlerFunc {
		return func(ctx Context) error {
			r.logger.Info("static.get Next")
			return ctx.Next()
		}
	})
	r.addLateRoute(HEAD, path+"/*", handler, "static.head", func(hf HandlerFunc) HandlerFunc {
		return func(ctx Context) error {
			r.logger.Info("static.head Next")
			return ctx.Next()
		}
	})
	return r
}

func (r *FiberRouter) GetPrefix() string {
	return r.prefix
}

func (a *FiberAdapter) Init() {

	if a.initialized {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.initialized {
		return
	}

	for _, route := range a.router.lateRoutes {
		a.router.Handle(
			route.method,
			route.path,
			route.handler,
			route.mw...,
		).SetName(route.name)
	}

	a.router.lateRoutes = make([]*lateRoute, 0)
	a.initialized = true
}

func (a *FiberAdapter) Serve(address string) error {
	a.Init()
	return a.app.Listen(address)
}

func (a *FiberAdapter) Shutdown(ctx context.Context) error {
	return a.app.ShutdownWithContext(ctx)
}

func (a *FiberAdapter) WrappedRouter() *fiber.App {
	return a.app
}

type FiberRouter struct {
	BaseRouter
	app           *fiber.App
	mergeStrategy RenderMergeStrategy
}

func (r *FiberRouter) Group(prefix string) Router[*fiber.App] {
	fullPrefix := r.joinPath(r.prefix, prefix)
	r.logger.Debug("creating route group", "prefix", fullPrefix)

	return &FiberRouter{
		app:           r.app,
		mergeStrategy: r.mergeStrategy,
		BaseRouter: BaseRouter{
			prefix: r.joinPath(r.prefix, prefix),
			// middlewares: append([]namedMiddleware{}, r.middlewares...),
			middlewares: slices.Clone(r.middlewares),
			logger:      r.logger,
			routes:      r.routes,
			root:        r.root,
		},
	}
}

// TODO: make the same as group but singletong r.routers[prefix] = Group(prefix)
// return r.routers[prefix]
func (r *FiberRouter) Mount(prefix string) Router[*fiber.App] {
	return &FiberRouter{
		app:           r.app,
		mergeStrategy: r.mergeStrategy,
		BaseRouter: BaseRouter{
			prefix: r.joinPath(r.prefix, prefix),
			// middlewares: append([]namedMiddleware{}, r.middlewares...),
			middlewares: slices.Clone(r.middlewares),
			logger:      r.logger,
			routes:      r.routes,
			root:        r.root,
		},
	}
}

func (r *FiberRouter) WithGroup(path string, cb func(r Router[*fiber.App])) Router[*fiber.App] {
	g := r.Group(path)
	cb(g)
	return r
}

func (r *FiberRouter) Use(m ...MiddlewareFunc) Router[*fiber.App] {
	for _, mw := range m {
		r.middlewares = append(r.middlewares, namedMiddleware{
			Name: funcName(mw),
			Mw:   mw,
		})
	}
	return r
}

func (r *FiberRouter) Handle(method HTTPMethod, pathStr string, handler HandlerFunc, m ...MiddlewareFunc) RouteInfo {
	fullPath := r.joinPath(r.prefix, pathStr)

	// Check for duplicates
	for _, existingRoute := range r.root.routes {
		if existingRoute.Method == method && existingRoute.Path == fullPath {
			r.logger.Warn("Duplicate route detected", "method", method, "path", fullPath)
		}
	}

	allMw := slices.Clone(r.middlewares)
	for _, mw := range m {
		allMw = append(allMw, namedMiddleware{
			Name: fmt.Sprintf("%s %s %s", method, pathStr, funcName(mw)),
			Mw:   mw,
		})
	}

	route := r.addRoute(method, fullPath, handler, "", allMw)

	r.logger.Info("registering route", "method", method, "path", pathStr, "name", route.Name)

	r.app.Add(string(method), fullPath, func(c *fiber.Ctx) error {
		ctx := NewFiberContext(c, r.logger)
		if fc, ok := ctx.(*fiberContext); ok {
			fc.setMergeStrategy(r.mergeStrategy)
			fc.setHandlers(route.Handlers)
			fc.index = -1 // reset index to ensure proper chain execution

			// Inject route context
			goCtx := fc.Context()

			// Always inject route name (empty string for unnamed routes)
			goCtx = WithRouteName(goCtx, route.Name)

			// Always inject route parameters from Fiber
			goCtx = WithRouteParams(goCtx, c.AllParams())
			fc.SetContext(goCtx)

			return fc.Next() // execute the our chain completely before returning
		}
		return fmt.Errorf("context cast failed")
	})

	// If we later call route.SetName then we run this callback
	// to propagate
	route.onSetName = func(name string) {
		r.app.Name(name)
		r.addNamedRoute(name, route)
	}

	return route
}

func (r *FiberRouter) Get(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(GET, path, handler, mw...)
}

func (r *FiberRouter) Post(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(POST, path, handler, mw...)
}

func (r *FiberRouter) Put(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(PUT, path, handler, mw...)
}

func (r *FiberRouter) Delete(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(DELETE, path, handler, mw...)
}

func (r *FiberRouter) Patch(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(PATCH, path, handler, mw...)
}

func (r *FiberRouter) Head(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(HEAD, path, handler, mw...)
}

func (r *FiberRouter) WebSocket(path string, config WebSocketConfig, handler func(WebSocketContext) error) RouteInfo {
	fullPath := r.joinPath(r.prefix, path)

	r.logger.Info("registering websocket route", "path", path, "fullPath", fullPath)

	// Use FiberWebSocketHandler internally
	fiberHandler := FiberWebSocketHandler(config, handler)

	// Add to Fiber's routing
	r.app.Get(fullPath, fiberHandler)

	// Create route info for consistency
	route := r.addRoute(GET, fullPath, nil, "websocket", nil)
	route.onSetName = func(name string) {
		r.app.Name(name)
		r.addNamedRoute(name, route)
	}

	return route
}

func (r *FiberRouter) PrintRoutes() {
	r.BaseRouter.PrintRoutes()
}

func (r *FiberRouter) WithLogger(logger Logger) Router[*fiber.App] {
	r.BaseRouter.WithLogger(logger)
	return r
}

type fiberContext struct {
	ctx           *fiber.Ctx
	mergeStrategy RenderMergeStrategy
	handlers      []NamedHandler
	index         int
	store         ContextStore
	logger        Logger
	cachedCtx     context.Context
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
	if meta := c.getMeta(); meta != nil {
		return meta.method
	}
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Method()
	}
	return ""
}
func (c *fiberContext) Path() string {
	if meta := c.getMeta(); meta != nil {
		return meta.path
	}
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Path()
	}
	return ""
}

func (c *fiberContext) Param(name string, defaultValue ...string) string {
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.params[name]; ok {
			return val
		}
	}
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Params(name, defaultValue...)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (c *fiberContext) IP() string {
	if meta := c.getMeta(); meta != nil {
		return meta.ip
	}
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.IP()
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
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.cookies[key]; ok {
			return val
		}
	}
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Cookies(key, defaultValue...)
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
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.params[name]; ok {
			if out, err := strconv.Atoi(val); err == nil {
				return out
			}
		}
	}
	if ctx := c.liveCtx(); ctx != nil {
		if out, err := ctx.ParamsInt(name, defaultValue); err == nil {
			return out
		}
	}
	return defaultValue
}

func (c *fiberContext) Query(name string, defaultValue ...string) string {
	def := ""
	if len(defaultValue) > 0 {
		def = defaultValue[0]
	}
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.queries[name]; ok {
			return val
		}
	}
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Query(name, def)
	}
	return def
}

func (c *fiberContext) QueryInt(name string, defaultValue int) int {
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.queries[name]; ok {
			if out, err := strconv.Atoi(val); err == nil {
				return out
			}
		}
	}
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.QueryInt(name, defaultValue)
	}
	return defaultValue
}

func (c *fiberContext) Queries() map[string]string {
	if meta := c.getMeta(); meta != nil && meta.queries != nil {
		out := make(map[string]string, len(meta.queries))
		for k, v := range meta.queries {
			out[k] = v
		}
		return out
	}
	if ctx := c.liveCtx(); ctx != nil {
		queries := make(map[string]string)
		args := ctx.Request().URI().QueryArgs()
		args.VisitAll(func(key, value []byte) {
			queries[string(key)] = string(value)
		})
		return queries
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
	if meta := c.getMeta(); meta != nil {
		if val, ok := meta.headers[key]; ok {
			return val
		}
	}
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.Get(key)
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
	if meta := c.getMeta(); meta != nil {
		return meta.originalURL
	}
	if ctx := c.liveCtx(); ctx != nil {
		return ctx.OriginalURL()
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
