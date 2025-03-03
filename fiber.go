package router

import (
	"context"
	"fmt"
	"path"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

type FiberAdapter struct {
	// BaseAdapter
	app         *fiber.App
	initialized bool
	router      *FiberRouter
	opts        []func(*fiber.App) *fiber.App
}

func newFiberInstance() *fiber.App {
	return fiber.New(fiber.Config{
		UnescapePath:      true,
		EnablePrintRoutes: true,
		StrictRouting:     false,
		PassLocalsToViews: true,
	})
}

// TODO: We should make this asynchronous so that we can gather options in our
// app before we call fiber.New (since at that point it calls init and we are done)
func NewFiberAdapter(opts ...func(*fiber.App) *fiber.App) Server[*fiber.App] {
	app := newFiberInstance()

	if len(opts) == 0 {
		opts = append(opts, DefaultFiberOptions)
	}

	for _, opt := range opts {
		app = opt(app)
	}

	return &FiberAdapter{
		app:  app,
		opts: opts,
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
			app: a.app,
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
		ctx := NewFiberContext(c)
		// c.Next() will work if c.handlers is set up at request time.
		return h(ctx)
	}
}

func (r *FiberRouter) Static(prefix, root string, config ...Static) Router[*fiber.App] {
	path, handler := r.makeStaticHandler(prefix, root, config...)
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

	for _, route := range a.router.lateRoutes {
		a.router.Handle(route.method, route.path, route.handler, route.mw...)
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
	app *fiber.App
}

func (r *FiberRouter) Group(prefix string) Router[*fiber.App] {
	return &FiberRouter{
		app: r.app,
		BaseRouter: BaseRouter{
			prefix:      path.Join(r.prefix, prefix),
			middlewares: append([]namedMiddleware{}, r.middlewares...),
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
	allMw := append([]namedMiddleware{}, r.middlewares...)
	for _, mw := range m {
		allMw = append(allMw, namedMiddleware{
			Name: funcName(mw),
			Mw:   mw,
		})
	}

	route := r.addRoute(method, fullPath, handler, "", allMw)

	r.app.Add(string(method), fullPath, func(c *fiber.Ctx) error {
		ctx := NewFiberContext(c)
		if fc, ok := ctx.(*fiberContext); ok {
			fc.setHandlers(route.Handlers)
		}
		return ctx.Next()
	})

	// If we later call route.SetName then we run this callback
	// to propagate
	route.onSetName = func(name string) {
		r.app.Name(name)
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

func (r *FiberRouter) PrintRoutes() {
	r.BaseRouter.PrintRoutes()
}

type fiberContext struct {
	ctx      *fiber.Ctx
	handlers []NamedHandler
	index    int
	store    ContextStore
}

func NewFiberContext(c *fiber.Ctx) Context {
	return &fiberContext{ctx: c, index: -1, store: NewContextStore()}
}

func (c *fiberContext) setHandlers(h []NamedHandler) {
	c.handlers = h
}

// Context methods

func (c *fiberContext) Locals(key any, value ...any) any {
	return c.ctx.Locals(key, value...)
}

func (c *fiberContext) Render(name string, bind any, layouts ...string) error {
	data, err := SerializeAsContext(bind)
	if err != nil {
		return fmt.Errorf("render: error serializing vars: %w", err)
	}

	if c.ctx.App().Config().PassLocalsToViews {
		c.ctx.Context().VisitUserValues(func(key []byte, val any) {
			if _, ok := data[string(key)]; !ok {
				data[string(key)] = val
			}
		})
	}

	return c.ctx.Render(name, data, layouts...)
}

func (c *fiberContext) Body() []byte { return c.ctx.Body() }

func (c *fiberContext) Method() string { return c.ctx.Method() }
func (c *fiberContext) Path() string   { return c.ctx.Path() }

func (c *fiberContext) Param(name string, defaultValue ...string) string {
	return c.ctx.Params(name, defaultValue...)
}

func (c *fiberContext) Cookie(cookie *Cookie) {
	c.ctx.Cookie(&fiber.Cookie{
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
	return c.ctx.Cookies(key, defaultValue...)
}

func (c *fiberContext) CookieParser(out any) error {
	return c.ctx.CookieParser(out)
}

func (c *fiberContext) Redirect(location string, status ...int) error {
	return c.ctx.Redirect(location, status...)
}

func (c *fiberContext) RedirectToRoute(routeName string, params ViewContext, status ...int) error {
	return c.ctx.RedirectToRoute(routeName, params.asFiberMap(), status...)
}

func (c *fiberContext) RedirectBack(fallback string, status ...int) error {
	return c.ctx.RedirectBack(fallback, status...)
}

func (c *fiberContext) ParamsInt(name string, defaultValue int) int {
	if out, err := c.ctx.ParamsInt(name, defaultValue); err == nil {
		return out
	}
	return defaultValue
}

func (c *fiberContext) Query(name, defaultValue string) string {
	return c.ctx.Query(name, defaultValue)
}

func (c *fiberContext) QueryInt(name string, defaultValue int) int {
	return c.ctx.QueryInt(name, defaultValue)
}

func (c *fiberContext) Queries() map[string]string {
	queries := make(map[string]string)
	args := c.ctx.Request().URI().QueryArgs()
	args.VisitAll(func(key, value []byte) {
		queries[string(key)] = string(value)
	})
	return queries
}

func (c *fiberContext) Status(code int) Context {
	c.ctx.Status(code)
	return c
}

func (c *fiberContext) SendString(body string) error {
	return c.Send([]byte(body))
}

func (c *fiberContext) Send(body []byte) error {
	return c.ctx.Send(body)
}

func (c *fiberContext) JSON(code int, v any) error {
	return c.ctx.Status(code).JSON(v)
}

func (c *fiberContext) NoContent(code int) error {
	return c.ctx.SendStatus(code)
}

func (c *fiberContext) Bind(v any) error {
	return c.ctx.BodyParser(v)
}

func (c *fiberContext) Context() context.Context {
	return c.ctx.UserContext()
}

func (c *fiberContext) SetContext(ctx context.Context) {
	c.ctx.SetUserContext(ctx)
}

func (c *fiberContext) Header(key string) string {
	return c.ctx.Get(key)
}

func (c *fiberContext) Referer() string {
	if referrer := c.Header("Referer"); referrer != "" {
		return referrer
	}
	return c.Header("Referrer")
}

func (c *fiberContext) OriginalURL() string {
	return c.ctx.OriginalURL()
}

func (c *fiberContext) SetHeader(key string, value string) Context {
	c.ctx.Set(key, value)
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
	if c.index < len(c.handlers) {
		return c.handlers[c.index].Handler(c)
	}
	return nil
}
