package router

import (
	"sync"

	"github.com/gofiber/fiber/v2"
)

var once sync.Once
var sharedSubApp *fiber.App

// fiberMiddlewareKey is the Locals key for the MiddlewareFromFiber dispatch in
// flight. Being unexported, it cannot collide with user Locals, and views only
// receive string-keyed Locals.
type fiberMiddlewareKey struct{}

// fiberMiddlewareState carries one MiddlewareFromFiber dispatch through
// sharedSubApp. It lives in the request's Locals, so concurrent requests on the
// shared sub-app never see each other's state.
type fiberMiddlewareState struct {
	userFiberMw func(*fiber.Ctx) error
	next        HandlerFunc
	routerCtx   Context
	err         error
}

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
	//    Its error is kept for MiddlewareFromFiber to return, so the
	//    sub-app's own error handler never renders it.
	sharedSubApp.Use(func(ctx *fiber.Ctx) error {
		state, ok := ctx.Locals(fiberMiddlewareKey{}).(*fiberMiddlewareState)
		if !ok {
			return nil
		}
		state.err = state.userFiberMw(ctx)
		return nil
	})

	// 2) Second subApp middleware: call the router’s next(context) if
	//    the user’s middleware chain called c.Next(). Otherwise, we never get here.
	//    This is the last route, so the chain runs at most once per dispatch.
	sharedSubApp.Use(func(ctx *fiber.Ctx) error {
		state, ok := ctx.Locals(fiberMiddlewareKey{}).(*fiberMiddlewareState)
		if !ok {
			return nil
		}
		return state.next(state.routerCtx)
	})
}

// MiddlewareFromFiber adapts a user-provided Fiber middleware to
// your router's chain, preserving c.Next() semantics by dispatching
// each request through a shared sub-Fiber app.
//
// The rest of the router chain runs once, when the Fiber middleware calls
// c.Next(). If it does not, the chain stops there and whatever the middleware
// sent is the response, as in plain Fiber.
//
// The Fiber middleware's error is returned to the router chain, so earlier
// middleware and the app's error handler see it. A middleware that ends with
// `return c.Next()` passes on errors from the rest of the chain; one that
// handles an error itself and returns nil stops it there.
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

			// pass data using locals; restore the outer value afterwards so a
			// nested MiddlewareFromFiber does not leave its state behind.
			state := &fiberMiddlewareState{
				userFiberMw: userFiberMw,
				next:        next,
				routerCtx:   c,
			}
			prev := realFiberCtx.Locals(fiberMiddlewareKey{})
			realFiberCtx.Locals(fiberMiddlewareKey{}, state)
			defer realFiberCtx.Locals(fiberMiddlewareKey{}, prev)

			// Dispatch this request to the shared sub‑app.
			sharedSubApp.Handler()(reqCtx)

			return state.err
		}
	}
}
