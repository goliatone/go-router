package router

import (
	"context"
	"path"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

// FiberAdapter implements Server for Fiber framework
type FiberAdapter struct {
	app    *fiber.App
	router Router[*fiber.App]
}

type FiberConfig struct {
	AppFactory func() *fiber.App
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
		a.router = &FiberRouter{router: a.app}
	}
	return a.router
}

func (a *FiberAdapter) WrapHandler(h HandlerFunc) interface{} {
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

// FiberRouter implements Router for Fiber
type FiberRouter struct {
	router *fiber.App
	prefix string
}

func (r *FiberRouter) Handle(method HTTPMethod, path string, handlers ...HandlerFunc) RouteInfo {
	fullPath := r.prefix + path

	h := make([]func(*fiber.Ctx) error, len(handlers))

	for i, handler := range handlers {
		h[i] = func(c *fiber.Ctx) error {
			return handler(NewFiberContext(c))
		}
	}

	r.router.Add(string(method), fullPath, h...)

	return &FiberRouteInfo{router: r.router}
}

func (r *FiberRouter) Group(prefix string) Router[*fiber.App] {
	return &FiberRouter{
		router: r.router,
		prefix: path.Join(r.prefix, prefix),
	}
}

func (r *FiberRouter) Use(args ...any) Router[*fiber.App] {
	var params []interface{}

	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			params = append(params, v)
		case HandlerFunc:
			params = append(params, func(c *fiber.Ctx) error {
				return v(NewFiberContext(c))
			})
		case func(Context) error:
			params = append(params, func(c *fiber.Ctx) error {
				return v(NewFiberContext(c))
			})
		}
	}

	r.router.Use(params...)
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

// Context implementations
type fiberContext struct {
	ctx *fiber.Ctx
}

func NewFiberContext(c *fiber.Ctx) Context {
	return &fiberContext{ctx: c}
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

func (c *fiberContext) Next() error {
	return c.ctx.Next()
}

// RouteInfo implementations
type FiberRouteInfo struct {
	router *fiber.App
	path   string
}

func (r *FiberRouteInfo) Name(name string) RouteInfo {
	r.router.Name(name)
	return r
}
