package router

import (
	"context"
	paths "path"
	"sort"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// FiberAdapter implements Server for Fiber framework
type FiberAdapter struct {
	app          *fiber.App
	router       *FiberRouter
	errorHandler ErrorHandler
}

func NewFiberAdapter(opts ...func(*fiber.App) *fiber.App) Server[*fiber.App] {
	app := fiber.New()

	if len(opts) == 0 {
		opts = append(opts, DefaultFiberOptions)
	}

	for _, opt := range opts {
		app = opt(app)
	}

	return &FiberAdapter{app: app}
}

func DefaultFiberOptions(app *fiber.App) *fiber.App {
	app.Use(recover.New())
	app.Use(logger.New())
	return app
}

func (a *FiberAdapter) Router() Router[*fiber.App] {
	if a.router == nil {
		a.router = &FiberRouter{
			router:       a.app,
			errorHandler: a.errorHandler,
		}
	}
	return a.router
}

func (a *FiberAdapter) WrapHandler(h HandlerFunc) any {
	return func(c *fiber.Ctx) error {
		return h(NewFiberContext(c))
	}
}

func (a *FiberAdapter) Serve(address string) error {
	return a.app.Listen(address)
}

func (a *FiberAdapter) Shutdown(ctx context.Context) error {
	return a.app.ShutdownWithContext(ctx)
}

func (a *FiberAdapter) WrappedRouter() *fiber.App {
	return a.app
}

func (a *FiberAdapter) SetErrorHandler(handler ErrorHandler) {
	a.errorHandler = handler
	if a.router != nil {
		a.router.errorHandler = handler
	}
}

type FiberRouter struct {
	router      *fiber.App
	prefix      string
	middlewares []struct {
		priority int
		handler  MiddlewareFunc
	}
	errorHandler ErrorHandler
}

func (r *FiberRouter) Handle(method HTTPMethod, path string, handlers ...HandlerFunc) RouteInfo {
	fullPath := paths.Join(r.prefix, path)
	handler := r.buildChain(handlers...)

	r.router.Add(string(method), fullPath, func(c *fiber.Ctx) error {
		ctx := NewFiberContext(c)
		if err := handler(ctx); err != nil && r.errorHandler != nil {
			return r.errorHandler(ctx, err)
		}
		return nil
	})

	return &FiberRouteInfo{router: r.router}
}

func (r *FiberRouter) buildChain(handlers ...HandlerFunc) HandlerFunc {
	sort.Slice(r.middlewares, func(i, j int) bool {
		return r.middlewares[i].priority > r.middlewares[j].priority
	})

	var final HandlerFunc
	if len(handlers) > 0 {
		final = handlers[0]
	} else {
		final = func(c Context) error { return nil }
	}

	for i := len(r.middlewares) - 1; i >= 0; i-- {
		final = r.middlewares[i].handler(final)
	}

	return final
}

func (r *FiberRouter) Group(prefix string) Router[*fiber.App] {
	return &FiberRouter{
		router:       r.router,
		prefix:       paths.Join(r.prefix, prefix),
		middlewares:  r.middlewares,
		errorHandler: r.errorHandler,
	}
}

func (r *FiberRouter) Use(middleware ...MiddlewareFunc) Router[*fiber.App] {
	return r.UseWithPriority(0, middleware...)
}

func (r *FiberRouter) UseWithPriority(priority int, middleware ...MiddlewareFunc) Router[*fiber.App] {
	for _, m := range middleware {
		r.middlewares = append(r.middlewares, struct {
			priority int
			handler  MiddlewareFunc
		}{priority, m})
	}
	return r
}

func (r *FiberRouter) Get(path string, handler HandlerFunc) RouteInfo {
	return r.Handle(GET, path, handler)
}

func (r *FiberRouter) Post(path string, handler HandlerFunc) RouteInfo {
	return r.Handle(POST, path, handler)
}

func (r *FiberRouter) Put(path string, handler HandlerFunc) RouteInfo {
	return r.Handle(PUT, path, handler)
}

func (r *FiberRouter) Delete(path string, handler HandlerFunc) RouteInfo {
	return r.Handle(DELETE, path, handler)
}

func (r *FiberRouter) Patch(path string, handler HandlerFunc) RouteInfo {
	return r.Handle(PATCH, path, handler)
}

type fiberContext struct {
	ctx     *fiber.Ctx
	store   ContextStore
	aborted bool
}

func NewFiberContext(c *fiber.Ctx) Context {
	return &fiberContext{
		ctx:   c,
		store: NewContextStore(),
	}
}

func (c *fiberContext) Method() string           { return c.ctx.Method() }
func (c *fiberContext) Path() string             { return c.ctx.Path() }
func (c *fiberContext) Param(name string) string { return c.ctx.Params(name) }
func (c *fiberContext) Query(name string) string { return c.ctx.Query(name) }

func (c *fiberContext) Queries() map[string]string {
	queries := make(map[string]string)
	c.ctx.QueryParser(&queries)
	return queries
}

func (c *fiberContext) Status(code int) ResponseWriter {
	c.ctx.Status(code)
	return c
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

func (c *fiberContext) SetHeader(key string, value string) ResponseWriter {
	c.ctx.Set(key, value)
	return c
}

func (c *fiberContext) Next() error {
	return c.ctx.Next()
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

func (c *fiberContext) IsAborted() bool {
	return c.aborted
}

func (c *fiberContext) Abort() {
	c.aborted = true
}

type FiberRouteInfo struct {
	router *fiber.App
}

func (r *FiberRouteInfo) Name(name string) RouteInfo {
	r.router.Name(name)
	return r
}
