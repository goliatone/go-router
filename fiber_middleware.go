package router

import (
	"sync"

	"github.com/gofiber/fiber/v2"
)

var once sync.Once
var sharedSubApp *fiber.App

// Initialize sharedSubApp once (in init or a custom func).
func initSharedSubApp() {
	// Create one sub‑Fiber app with minimal overhead.
	sharedSubApp = fiber.New(fiber.Config{
		DisableStartupMessage: true,
		StrictRouting:         false,
		CaseSensitive:         false,
	})

	// 1) First subApp middleware: call the user’s Fiber middleware.
	//    If user’s middleware calls c.Next(), flow continues;
	//    otherwise, short circuit (like normal Fiber).
	sharedSubApp.Use(func(ctx *fiber.Ctx) error {
		// Retrieve the user’s middleware from Locals (set per-request).
		userFiberMw, _ := ctx.Locals("userFiberMw").(func(*fiber.Ctx) error)
		if userFiberMw == nil {
			// If none set, just continue.
			return ctx.Next()
		}
		if err := userFiberMw(ctx); err != nil {
			return err
		}
		return ctx.Next()
	})

	// 2) Second subApp middleware: call the router’s next(context) if
	//    the user’s middleware chain called c.Next(). Otherwise, we never get here.
	sharedSubApp.Use(func(ctx *fiber.Ctx) error {
		routeNext, _ := ctx.Locals("routeNext").(HandlerFunc)
		if routeNext == nil {
			return nil
		}
		routerCtx, _ := ctx.Locals("routerContext").(Context)
		if routerCtx == nil {
			return nil
		}
		return routeNext(routerCtx)
	})

	sharedSubApp.All("/*", func(ctx *fiber.Ctx) error {
		userFiberMw, _ := ctx.Locals("userFiberMw").(func(*fiber.Ctx) error)
		if userFiberMw != nil {
			if err := userFiberMw(ctx); err != nil {
			}
		}
		routeNext, _ := ctx.Locals("routeNext").(HandlerFunc)
		routerCtx, _ := ctx.Locals("routerContext").(Context)
		if routeNext != nil && routerCtx != nil {
			return routeNext(routerCtx)
		}
		return nil
	})
}

// MiddlewareFromFiber adapts a user-provided Fiber middleware to
// your router's chain, preserving c.Next() semantics by spinning
// up a sub-Fiber app for each request.
func MiddlewareFromFiber(userFiberMw func(*fiber.Ctx) error) MiddlewareFunc {
	once.Do(func() {
		initSharedSubApp()
	})

	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			fc, ok := c.(*fiberContext)
			if !ok {
				// not a fiber ctx, continue the chain
				return next(c)
			}

			realFiberCtx := fc.ctx
			reqCtx := realFiberCtx.Context() // *fasthttp.RequestCtx

			// pass data using locals
			realFiberCtx.Locals("userFiberMw", userFiberMw)
			realFiberCtx.Locals("routeNext", next)
			realFiberCtx.Locals("routerContext", c)

			// Dispatch this request to the shared sub‑app.
			sharedSubApp.Handler()(reqCtx)

			// always return nil, because any error or response
			// is handled inside the sub app chain. To
			// propagate errors, we could store them in Locals or
			// checking some status code after Handler() returns.
			return nil
		}
	}
}
