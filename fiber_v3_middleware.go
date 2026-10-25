package router

import (
	"sync"

	"github.com/gofiber/fiber/v3"
)

var fiberV3MiddlewareOnce sync.Once
var sharedV3SubApp *fiber.App

// fiberV3MiddlewareKey is the Locals key for the MiddlewareFromFiberV3 dispatch in
// flight. Being unexported, it cannot collide with user Locals, and views only
// receive string-keyed Locals.
type fiberV3MiddlewareKey struct{}

// fiberV3MiddlewareState carries one MiddlewareFromFiberV3 dispatch through
// sharedV3SubApp. It lives in the request's Locals, so concurrent requests on the
// shared sub-app never see each other's state.
type fiberV3MiddlewareState struct {
	userFiberMw func(fiber.Ctx) error
	next        HandlerFunc
	routerCtx   *fiberV3Context
	err         error
	nextCalled  bool
}

// Initialize sharedV3SubApp once (in init or a custom func).
func initSharedV3SubApp() {
	// Create one sub‑Fiber app with minimal overhead.
	sharedV3SubApp = fiber.New(fiber.Config{
		StrictRouting: false,
		CaseSensitive: false,
	})

	// 1) First subApp middleware: call the user’s Fiber middleware.
	//    If user’s middleware calls c.Next(), flow continues;
	//    otherwise, short circuit (like normal Fiber).
	//    Its error is kept for MiddlewareFromFiberV3 to return, so the
	//    sub-app's own error handler never renders it.
	sharedV3SubApp.Use(func(ctx fiber.Ctx) error {
		state, ok := ctx.Locals(fiberV3MiddlewareKey{}).(*fiberV3MiddlewareState)
		if !ok {
			return nil
		}
		// V3 clears the shared standard-context local when it releases this
		// acquired Ctx. Capture it in the router wrapper before that release.
		ctx.SetContext(state.routerCtx.Context())
		defer func() { state.routerCtx.SetContext(ctx.Context()) }()
		state.err = state.userFiberMw(ctx)
		return nil
	})

	// 2) Second subApp middleware: call the router’s next(context) if
	//    the user’s middleware chain called c.Next(). Otherwise, we never get here.
	//    This is the last route, so the chain runs at most once per dispatch.
	sharedV3SubApp.Use(func(ctx fiber.Ctx) error {
		state, ok := ctx.Locals(fiberV3MiddlewareKey{}).(*fiberV3MiddlewareState)
		if !ok {
			return nil
		}
		// Native middleware may replace UserContext after the router cached it.
		// Make that current context visible before re-entering the router chain.
		state.routerCtx.SetContext(ctx.Context())
		state.routerCtx.syncNativeResponseState(false)
		state.nextCalled = true
		err := state.next(state.routerCtx)
		ctx.SetContext(state.routerCtx.Context())
		return err
	})
}

// MiddlewareFromFiberV3 adapts a user-provided Fiber middleware to
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
func MiddlewareFromFiberV3(userFiberMw fiber.Handler) MiddlewareFunc {
	fiberV3MiddlewareOnce.Do(func() {
		initSharedV3SubApp()
	})

	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			fc, ok := c.(*fiberV3Context)
			if !ok {
				// not a fiber ctx, continue the chain
				return next(c)
			}

			realFiberCtx := fc.ctx
			// Also synchronize updates made while native middleware unwinds, or
			// when it short-circuits without calling the router's next handler.
			defer func() { realFiberCtx.SetContext(fc.Context()) }()
			reqCtx := realFiberCtx.RequestCtx() // *fasthttp.RequestCtx

			// pass data using locals; restore the outer value afterwards so a
			// nested MiddlewareFromFiberV3 does not leave its state behind.
			state := &fiberV3MiddlewareState{
				userFiberMw: userFiberMw,
				next:        next,
				routerCtx:   fc,
			}
			prev := realFiberCtx.Locals(fiberV3MiddlewareKey{})
			realFiberCtx.Locals(fiberV3MiddlewareKey{}, state)
			defer realFiberCtx.Locals(fiberV3MiddlewareKey{}, prev)

			// Dispatch this request to the shared sub‑app.
			sharedV3SubApp.Handler()(reqCtx)
			// A successful short circuit owns even an empty response. For
			// pass-through middleware, retain the downstream write state and
			// inspect the final native buffer without consuming body streams.
			fc.syncNativeResponseState(!state.nextCalled && state.err == nil)

			return state.err
		}
	}
}
