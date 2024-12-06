package router

import (
	"context"
	"path"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

type FiberAdapter struct {
	app    *fiber.App
	router *FiberRouter
}

func NewFiberAdapter(opts ...func(*fiber.App) *fiber.App) Server[*fiber.App] {
	app := fiber.New(fiber.Config{
		UnescapePath:      true,
		EnablePrintRoutes: true,
		StrictRouting:     false,
	})

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
			app: a.app,
			baseRouter: baseRouter{
				logger: &defaultLogger{},
				root:   &routerRoot{},
			},
		}
	}
	return a.router
}

func (a *FiberAdapter) WrapHandler(h HandlerFunc) interface{} {
	// Wrap a HandlerFunc into a fiber handler
	return func(c *fiber.Ctx) error {
		ctx := NewFiberContext(c)
		// c.Next() will work if c.handlers is set up at request time.
		return h(ctx)
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

type FiberRouter struct {
	app *fiber.App
	baseRouter
}

func (r *FiberRouter) Group(prefix string) Router[*fiber.App] {
	return &FiberRouter{
		app: r.app,
		baseRouter: baseRouter{
			prefix:      path.Join(r.prefix, prefix),
			middlewares: append([]namedMiddleware{}, r.middlewares...),
			logger:      r.logger,
			routes:      r.routes,
			root:        r.root,
		},
	}
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
	fullPath := r.prefix + pathStr
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

func (r *FiberRouter) PrintRoutes() {
	r.baseRouter.PrintRoutes()
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

func (c *fiberContext) JSON(code int, v interface{}) error {
	return c.ctx.Status(code).JSON(v)
}

func (c *fiberContext) NoContent(code int) error {
	return c.ctx.SendStatus(code)
}

func (c *fiberContext) Bind(v interface{}) error {
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
