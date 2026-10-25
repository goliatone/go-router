package router

import (
	"context"
	"fmt"
	"slices"
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
	strictRoutes  bool
	opts          []func(*fiber.App) *fiber.App
}

type FiberAdapterConfig struct {
	MergeStrategy            RenderMergeStrategy
	ConflictPolicy           *HTTPRouterConflictPolicy
	NamedRoutePolicy         NamedRouteCollisionPolicy
	PathConflictMode         PathConflictMode
	EnforceCatchAllConflicts bool
	EnforceRouteLints        bool
	StrictRoutes             bool
	OrderRoutesBySpecificity bool
	// PreserveRegistrationOrder disables the default specificity ordering.
	// Use only when an application intentionally relies on route shadowing.
	PreserveRegistrationOrder bool
}

func (cfg FiberAdapterConfig) withDefaults() FiberAdapterConfig {
	if cfg.MergeStrategy == nil {
		cfg.MergeStrategy = defaultRenderMergeStrategy
	}
	if cfg.ConflictPolicy == nil {
		policy := HTTPRouterConflictLogAndContinue
		cfg.ConflictPolicy = &policy
	}
	cfg.NamedRoutePolicy = cfg.NamedRoutePolicy.normalize()
	cfg.PathConflictMode = cfg.PathConflictMode.normalize()
	// Prefer-static mode requires deterministic specificity ordering.
	if cfg.PathConflictMode == PathConflictModePreferStatic {
		cfg.OrderRoutesBySpecificity = true
	}
	if !cfg.PreserveRegistrationOrder {
		cfg.OrderRoutesBySpecificity = true
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

	conflictPolicy := HTTPRouterConflictLogAndContinue
	if cfg.ConflictPolicy != nil {
		conflictPolicy = *cfg.ConflictPolicy
	}

	return &FiberAdapter{
		app:           app,
		opts:          opts,
		mergeStrategy: cfg.MergeStrategy,
		strictRoutes:  cfg.StrictRoutes,
		router: &FiberRouter{
			app:                      app,
			mergeStrategy:            cfg.MergeStrategy,
			conflictPolicy:           conflictPolicy,
			pathConflictMode:         cfg.PathConflictMode,
			enforceCatchAllConflicts: cfg.EnforceCatchAllConflicts,
			enforceRouteLints:        cfg.EnforceRouteLints,
			orderRoutesBySpecificity: cfg.OrderRoutesBySpecificity,
			BaseRouter: BaseRouter{
				logger:           &defaultLogger{},
				namedRoutePolicy: cfg.NamedRoutePolicy,
				root: &routerRoot{matchingSemantics: RouteMatchingSemantics{
					TrailingSlashDistinct: app.Config().StrictRouting,
				}},
			},
		},
	}
}

func DefaultFiberOptions(app *fiber.App) *fiber.App {
	// app.Use(recover.New())
	app.Use(logger.New())
	return app
}

func (a *FiberAdapter) Router() Router[*fiber.App] {
	if a.router == nil {
		conflictPolicy := HTTPRouterConflictLogAndContinue
		a.router = &FiberRouter{
			app:                      a.app,
			mergeStrategy:            a.mergeStrategy,
			conflictPolicy:           conflictPolicy,
			pathConflictMode:         PathConflictModeStrict,
			enforceCatchAllConflicts: false,
			enforceRouteLints:        false,
			BaseRouter: BaseRouter{
				logger:           &defaultLogger{},
				namedRoutePolicy: NamedRouteCollisionPolicyReplace,
				root: &routerRoot{matchingSemantics: RouteMatchingSemantics{
					TrailingSlashDistinct: a.app.Config().StrictRouting,
				}},
			},
		}
	}
	return a.router
}

func (a *FiberAdapter) WrapHandler(h HandlerFunc) any {
	// Wrap a HandlerFunc into a fiber handler
	return func(c *fiber.Ctx) error {
		a.Router()
		ctx := NewFiberContext(c, a.router.logger)
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
	registerFiberStaticRoutes(&r.BaseRouter, r.handleFull, prefix, root, config...)
	return r
}

// handleFull registers a route using an already-prefixed path (i.e. full/absolute path).
// This is needed for helpers like Static() which produce full paths (including group prefix).
func (r *FiberRouter) handleFull(method HTTPMethod, fullPath string, handler HandlerFunc, m ...MiddlewareFunc) RouteInfo {
	r.root.beginMutation("register route", method, fullPath)
	changed := false
	defer func() { r.root.endMutation(changed) }()

	allMw := slices.Clone(r.middlewares)
	for _, mw := range m {
		allMw = append(allMw, namedMiddleware{
			Name: fmt.Sprintf("%s %s %s", method, fullPath, funcName(mw)),
			Mw:   mw,
		})
	}

	route, mounted := r.handleLocked(method, fullPath, handler, allMw)
	changed = mounted
	return route
}

// handleLateRoute registers a late route at its already-prefixed path with the
// middleware chain captured by the router that declared it.
func (r *FiberRouter) handleLateRoute(method HTTPMethod, fullPath string, handler HandlerFunc, middlewares []namedMiddleware) RouteInfo {
	r.root.beginMutation("register route", method, fullPath)
	changed := false
	defer func() { r.root.endMutation(changed) }()

	route, mounted := r.handleLocked(method, fullPath, handler, middlewares)
	changed = mounted
	return route
}

// handleLocked registers fullPath with an already resolved middleware chain.
// Callers must hold the root registration lock.
func (r *FiberRouter) handleLocked(method HTTPMethod, fullPath string, handler HandlerFunc, allMw []namedMiddleware) (RouteInfo, bool) {
	return handleFiberRoute(&r.BaseRouter, r.detectRouteConflict(method, fullPath), r.conflictPolicy, r.pathConflictMode, r.routeRegistration, method, fullPath, handler, allMw)
}

func (r *FiberRouter) routeRegistration(route *RouteDefinition) {
	route.onSetName = func(route *RouteDefinition, name string) error {
		return r.setPublicRouteName(route, name, func() {
			// In the explicit registration-order compatibility mode the route is
			// already Fiber's most recently mounted route when callers chain
			// Get(...).SetName(...). Deferred mode applies the name at mount time.
			if !r.orderRoutesBySpecificity && name != "" {
				r.app.Name(name)
			}
		})
	}

	if r.orderRoutesBySpecificity && !r.root.deferredRegistered {
		r.root.deferredRoutes = append(r.root.deferredRoutes, route)
		return
	}

	r.logger.Info("registering route", "method", route.Method, "path", route.Path, "name", route.Name)

	r.app.Add(string(route.Method), route.Path, func(c *fiber.Ctx) error {
		ctx := NewFiberContext(c, r.logger)
		if fc, ok := ctx.(*fiberContext); ok {
			fc.setMergeStrategy(r.mergeStrategy)
			fc.setHandlers(route.Handlers)
			fc.index = -1 // reset index to ensure proper chain execution

			// Inject route context
			goCtx := fc.Context()
			goCtx = WithRouteName(goCtx, route.Name)
			goCtx = WithRouteParams(goCtx, c.AllParams())
			fc.SetContext(goCtx)

			return fc.Next()
		}
		return fmt.Errorf("context cast failed")
	})
	if route.Name != "" && route.nameMode == routeNameModePublic {
		r.app.Name(route.Name)
	}
	r.root.recordMounted(route)
}

func (r *FiberRouter) registerDeferredRoutes() {
	if !r.orderRoutesBySpecificity || r.root.deferredRegistered {
		return
	}

	r.root.deferredRegistered = true
	if len(r.root.deferredRoutes) == 0 {
		return
	}

	sortRoutesBySpecificity(r.root.deferredRoutes)
	for _, route := range r.root.deferredRoutes {
		r.routeRegistration(route)
	}
	r.root.deferredRoutes = r.root.deferredRoutes[:0]
}

func (r *FiberRouter) registerMissHandlers() {
	if len(r.root.missHandlers) == 0 {
		return
	}

	r.app.Use(func(c *fiber.Ctx) error {
		def := r.missHandler(HTTPMethod(c.Method()))
		if def == nil {
			return c.Status(fiber.StatusNotFound).SendString("Not Found")
		}

		ctx := NewFiberContext(c, r.logger)
		fc, ok := ctx.(*fiberContext)
		if !ok {
			return fmt.Errorf("context cast failed")
		}

		fc.setMergeStrategy(r.mergeStrategy)
		fc.setHandlers(def.Handlers)
		fc.index = -1

		goCtx := fc.Context()
		goCtx = WithRouteName(goCtx, "")
		goCtx = WithRouteParams(goCtx, map[string]string{})
		fc.SetContext(goCtx)

		return fc.Next()
	})
}

func (r *FiberRouter) detectRouteConflict(method HTTPMethod, fullPath string) *routeConflict {
	for _, route := range r.root.routes {
		if route.Method != method {
			continue
		}
		if route.Path == fullPath {
			return &routeConflict{
				existing: route,
				reason:   "duplicate route",
				index:    -1,
			}
		}
		if conflict := detectPathConflict(route.Path, fullPath, r.pathConflictMode, r.enforceCatchAllConflicts); conflict != nil {
			conflict.existing = route
			return conflict
		}
	}
	return nil
}

func (r *FiberRouter) GetPrefix() string {
	return r.prefix
}

func (a *FiberAdapter) Init() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.initialized {
		return
	}

	// Ensure router is initialized even when callers only use WrappedRouter().
	if a.router == nil {
		a.Router()
	}

	finalizeFiberRoutes(&a.router.BaseRouter, a.router, a.strictRoutes)
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
	// Ensure go-router late routes (e.g., Static) are registered even if the
	// returned Fiber app is started with app.Listen directly.
	a.Init()
	return a.app
}

type FiberRouter struct {
	BaseRouter
	app                      *fiber.App
	mergeStrategy            RenderMergeStrategy
	conflictPolicy           HTTPRouterConflictPolicy
	pathConflictMode         PathConflictMode
	enforceCatchAllConflicts bool
	enforceRouteLints        bool
	orderRoutesBySpecificity bool
}

func (r *FiberRouter) Group(prefix string) Router[*fiber.App] {
	return &FiberRouter{
		app:                      r.app,
		mergeStrategy:            r.mergeStrategy,
		conflictPolicy:           r.conflictPolicy,
		pathConflictMode:         r.pathConflictMode,
		enforceCatchAllConflicts: r.enforceCatchAllConflicts,
		enforceRouteLints:        r.enforceRouteLints,
		orderRoutesBySpecificity: r.orderRoutesBySpecificity,
		BaseRouter:               fiberGroupBase(&r.BaseRouter, prefix),
	}
}

// TODO: make the same as group but singletong r.routers[prefix] = Group(prefix)
// return r.routers[prefix]
func (r *FiberRouter) Mount(prefix string) Router[*fiber.App] {
	return &FiberRouter{
		app:                      r.app,
		mergeStrategy:            r.mergeStrategy,
		conflictPolicy:           r.conflictPolicy,
		pathConflictMode:         r.pathConflictMode,
		enforceCatchAllConflicts: r.enforceCatchAllConflicts,
		enforceRouteLints:        r.enforceRouteLints,
		orderRoutesBySpecificity: r.orderRoutesBySpecificity,
		BaseRouter: BaseRouter{
			prefix: r.joinPath(r.prefix, prefix),
			// middlewares: append([]namedMiddleware{}, r.middlewares...),
			middlewares:      slices.Clone(r.middlewares),
			logger:           r.logger,
			namedRoutePolicy: r.namedRoutePolicy,
			routes:           r.routes,
			root:             r.root,
		},
	}
}

func (r *FiberRouter) WithGroup(path string, cb func(r Router[*fiber.App])) Router[*fiber.App] {
	g := r.Group(path)
	cb(g)
	return r
}

func (r *FiberRouter) Use(m ...MiddlewareFunc) Router[*fiber.App] {
	r.root.beginMutation("register middleware", "", r.prefix)
	changed := false
	defer func() { r.root.endMutation(changed) }()
	for _, mw := range m {
		r.middlewares = append(r.middlewares, namedMiddleware{
			Name: funcName(mw),
			Mw:   mw,
		})
	}
	changed = len(m) > 0
	return r
}

func (r *FiberRouter) HandleMiss(method HTTPMethod, handler HandlerFunc, m ...MiddlewareFunc) {
	if handler == nil {
		return
	}
	r.root.beginMutation("register miss handler", method, r.prefix)
	defer r.root.endMutation(true)
	r.setMissHandler(method, handler, r.buildNamedMiddlewares(m))
}

func (r *FiberRouter) Handle(method HTTPMethod, pathStr string, handler HandlerFunc, m ...MiddlewareFunc) RouteInfo {
	return declareFiberRoute(&r.BaseRouter, r.detectRouteConflict, r.routeRegistration, r.conflictPolicy, r.pathConflictMode, method, pathStr, handler, m...)
}

func (r *FiberRouter) TryReplace(method HTTPMethod, pathStr string, handler HandlerFunc, m ...MiddlewareFunc) (RouteInfo, error) {
	return r.TryReplaceWithOptions(method, pathStr, handler, RouteMutationOptions{}, m...)
}

func (r *FiberRouter) TryReplaceWithOptions(method HTTPMethod, pathStr string, handler HandlerFunc, options RouteMutationOptions, m ...MiddlewareFunc) (RouteInfo, error) {
	return replaceFiberRoute(&r.BaseRouter, method, pathStr, handler, options, m...)
}

func (r *FiberRouter) TryUpsert(method HTTPMethod, pathStr string, handler HandlerFunc, m ...MiddlewareFunc) (RouteInfo, bool, error) {
	return r.TryUpsertWithOptions(method, pathStr, handler, RouteMutationOptions{}, m...)
}

func (r *FiberRouter) TryUpsertWithOptions(method HTTPMethod, pathStr string, handler HandlerFunc, options RouteMutationOptions, m ...MiddlewareFunc) (RouteInfo, bool, error) {
	return upsertFiberRoute(&r.BaseRouter, r.detectRouteConflict, r.routeRegistration, r.conflictPolicy, r.pathConflictMode, method, pathStr, handler, options, m...)
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
	r.root.beginMutation("register websocket route", GET, fullPath)
	changed := false
	defer func() { r.root.endMutation(changed) }()
	if conflict := r.detectRouteConflict(GET, fullPath); conflict != nil {
		err := newRouteConflictError(GET, fullPath, conflict, r.conflictPolicy, r.pathConflictMode)
		switch r.conflictPolicy {
		case HTTPRouterConflictLogAndSkip:
			if r.logger != nil {
				r.logger.Warn("route conflict skipped: %v", err)
			}
			return noopRouteInfo
		case HTTPRouterConflictLogAndContinue:
			if r.logger != nil {
				r.logger.Warn("route conflict detected: %v", err)
			}
		case HTTPRouterConflictPanic:
			panic(err)
		}
	}

	r.logger.Info("registering websocket route", "path", path, "fullPath", fullPath)

	// Keep WebSockets in the same deferred physical plan as ordinary GET
	// routes. Registering directly on Fiber here would let an early wildcard
	// bypass specificity ordering and shadow later HTTP handlers.
	fiberHandler := FiberWebSocketHandler(config, handler)
	routeHandler := func(ctx Context) error {
		fc, ok := ctx.(*fiberContext)
		if !ok || fc == nil || fc.ctx == nil {
			return fmt.Errorf("expected fiberContext, got %T", ctx)
		}
		return fiberHandler(fc.ctx)
	}
	route := r.addRoute(GET, fullPath, routeHandler, "websocket", slices.Clone(r.middlewares))
	r.routeRegistration(route)
	changed = true

	return route
}

func (r *FiberRouter) PrintRoutes() {
	r.BaseRouter.PrintRoutes()
}

func (r *FiberRouter) WithLogger(logger Logger) Router[*fiber.App] {
	r.BaseRouter.WithLogger(logger)
	return r
}

func (r *FiberRouter) ValidateRoutes() []error {
	routes := collectRoutesForValidation(&r.BaseRouter)
	errs := ValidateRouteDefinitionsWithOptions(routes, RouteValidationOptions{
		PathConflictMode:         r.pathConflictMode,
		EnforceCatchAllConflicts: r.enforceCatchAllConflicts,
		EnforceRouteLints:        r.enforceRouteLints,
		NamedRoutePolicy:         r.namedRoutePolicy,
	})
	if r.namedRoutePolicy.normalize() == NamedRouteCollisionPolicyError {
		errs = append(errs, r.namedRouteConflicts()...)
	}
	return errs
}
