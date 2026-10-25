package router

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
)

type FiberV3Adapter struct {
	mu            sync.Mutex
	app           *fiber.App
	initialized   bool
	router        *FiberV3Router
	mergeStrategy RenderMergeStrategy
	strictRoutes  bool
	opts          []func(*fiber.App) *fiber.App
	listenConfig  fiber.ListenConfig
}

// FiberV3AdapterConfig combines shared route settings with native listen settings.
// ListenConfig applies to Serve. Callers starting WrappedRouter directly pass
// their listen settings to the native app themselves.
type FiberV3AdapterConfig struct {
	FiberAdapterConfig
	ListenConfig fiber.ListenConfig
}

func newFiberV3Instance() *fiber.App {
	return fiber.New(fiber.Config{
		UnescapePath:      true,
		StrictRouting:     false,
		PassLocalsToViews: true,
		ErrorHandler:      DefaultFiberV3ErrorHandler(DefaultFiberErrorHandlerConfig()),
	})
}

// NewFiberV3Adapter creates an explicit v3 server using the shared router contracts.
func NewFiberV3Adapter(opts ...func(*fiber.App) *fiber.App) Server[*fiber.App] {
	return NewFiberV3AdapterWithConfig(FiberV3AdapterConfig{}, opts...)
}

// NewFiberV3AdapterWithConfig allows callers to override adapter-level settings, including render merge strategy.
func NewFiberV3AdapterWithConfig(cfg FiberV3AdapterConfig, opts ...func(*fiber.App) *fiber.App) Server[*fiber.App] {
	if cfg.MergeStrategy == nil {
		cfg.MergeStrategy = defaultFiberV3RenderMergeStrategy
	}
	cfg.FiberAdapterConfig = cfg.withDefaults()
	app := newFiberV3Instance()

	if len(opts) == 0 {
		opts = append(opts, DefaultFiberV3Options)
	}

	for _, opt := range opts {
		app = opt(app)
	}

	conflictPolicy := HTTPRouterConflictLogAndContinue
	if cfg.ConflictPolicy != nil {
		conflictPolicy = *cfg.ConflictPolicy
	}

	return &FiberV3Adapter{
		app:           app,
		listenConfig:  cfg.ListenConfig,
		opts:          opts,
		mergeStrategy: cfg.MergeStrategy,
		strictRoutes:  cfg.StrictRoutes,
		router: &FiberV3Router{
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

func DefaultFiberV3Options(app *fiber.App) *fiber.App {
	// app.Use(recover.New())
	app.Use(logger.New())
	return app
}

func (a *FiberV3Adapter) Router() Router[*fiber.App] {
	if a.router == nil {
		conflictPolicy := HTTPRouterConflictLogAndContinue
		a.router = &FiberV3Router{
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

func (a *FiberV3Adapter) WrapHandler(h HandlerFunc) any {
	// Wrap a HandlerFunc into a fiber handler
	return func(c fiber.Ctx) error {
		a.Router()
		ctx := NewFiberV3Context(c, a.router.logger)
		if fc, ok := ctx.(*fiberV3Context); ok {
			fc.setMergeStrategy(a.mergeStrategy)
		}

		// Get path pattern from Fiber and inject route context
		if c.Route() != nil {
			pathPattern := c.Route().Path

			// Look up route name from go-router's namedRoutes
			if routeName, ok := a.router.RouteNameFromPath(c.Method(), pathPattern); ok {
				goCtx := ctx.Context()
				goCtx = WithRouteName(goCtx, routeName)
				goCtx = WithRouteParams(goCtx, fiberV3Params(c))
				ctx.SetContext(goCtx)
			}
		}

		// c.Next() will work if c.handlers is set up at request time.
		return h(ctx)
	}
}

func (r *FiberV3Router) Static(prefix, root string, config ...Static) Router[*fiber.App] {
	registerFiberStaticRoutes(&r.BaseRouter, r.handleFull, prefix, root, config...)
	return r
}

// handleFull registers a route using an already-prefixed path (i.e. full/absolute path).
// This is needed for helpers like Static() which produce full paths (including group prefix).
func (r *FiberV3Router) handleFull(method HTTPMethod, fullPath string, handler HandlerFunc, m ...MiddlewareFunc) RouteInfo {
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
func (r *FiberV3Router) handleLateRoute(method HTTPMethod, fullPath string, handler HandlerFunc, middlewares []namedMiddleware) RouteInfo {
	r.root.beginMutation("register route", method, fullPath)
	changed := false
	defer func() { r.root.endMutation(changed) }()

	route, mounted := r.handleLocked(method, fullPath, handler, middlewares)
	changed = mounted
	return route
}

// handleLocked registers fullPath with an already resolved middleware chain.
// Callers must hold the root registration lock.
func (r *FiberV3Router) handleLocked(method HTTPMethod, fullPath string, handler HandlerFunc, allMw []namedMiddleware) (RouteInfo, bool) {
	return handleFiberRoute(&r.BaseRouter, r.detectRouteConflict(method, fullPath), r.conflictPolicy, r.pathConflictMode, r.routeRegistration, method, fullPath, handler, allMw)
}

func (r *FiberV3Router) routeRegistration(route *RouteDefinition) {
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

	r.app.Add([]string{string(route.Method)}, route.Path, func(c fiber.Ctx) error {
		ctx := NewFiberV3Context(c, r.logger)
		if fc, ok := ctx.(*fiberV3Context); ok {
			fc.setMergeStrategy(r.mergeStrategy)
			fc.setHandlers(route.Handlers)
			fc.index = -1 // reset index to ensure proper chain execution

			// Inject route context
			goCtx := fc.Context()
			goCtx = WithRouteName(goCtx, route.Name)
			goCtx = WithRouteParams(goCtx, fiberV3Params(c))
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

func (r *FiberV3Router) registerDeferredRoutes() {
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

func (r *FiberV3Router) registerMissHandlers() {
	if len(r.root.missHandlers) == 0 {
		return
	}

	r.app.Use(func(c fiber.Ctx) error {
		def := r.missHandler(HTTPMethod(c.Method()))
		if def == nil {
			return c.Status(fiber.StatusNotFound).SendString("Not Found")
		}

		ctx := NewFiberV3Context(c, r.logger)
		fc, ok := ctx.(*fiberV3Context)
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

func (r *FiberV3Router) detectRouteConflict(method HTTPMethod, fullPath string) *routeConflict {
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

func (r *FiberV3Router) GetPrefix() string {
	return r.prefix
}

func (a *FiberV3Adapter) Init() {
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

func (a *FiberV3Adapter) Serve(address string) error {
	a.Init()
	return a.app.Listen(address, a.listenConfig)
}

func (a *FiberV3Adapter) Shutdown(ctx context.Context) error {
	return a.app.ShutdownWithContext(ctx)
}

func (a *FiberV3Adapter) WrappedRouter() *fiber.App {
	// Ensure go-router late routes (e.g., Static) are registered even if the
	// returned Fiber app is started with app.Listen directly.
	a.Init()
	return a.app
}

type FiberV3Router struct {
	BaseRouter
	app                      *fiber.App
	mergeStrategy            RenderMergeStrategy
	conflictPolicy           HTTPRouterConflictPolicy
	pathConflictMode         PathConflictMode
	enforceCatchAllConflicts bool
	enforceRouteLints        bool
	orderRoutesBySpecificity bool
}

func (r *FiberV3Router) Group(prefix string) Router[*fiber.App] {
	return &FiberV3Router{
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
func (r *FiberV3Router) Mount(prefix string) Router[*fiber.App] {
	return &FiberV3Router{
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

func (r *FiberV3Router) WithGroup(path string, cb func(r Router[*fiber.App])) Router[*fiber.App] {
	g := r.Group(path)
	cb(g)
	return r
}

func (r *FiberV3Router) Use(m ...MiddlewareFunc) Router[*fiber.App] {
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

func (r *FiberV3Router) HandleMiss(method HTTPMethod, handler HandlerFunc, m ...MiddlewareFunc) {
	if handler == nil {
		return
	}
	r.root.beginMutation("register miss handler", method, r.prefix)
	defer r.root.endMutation(true)
	r.setMissHandler(method, handler, r.buildNamedMiddlewares(m))
}

func (r *FiberV3Router) Handle(method HTTPMethod, pathStr string, handler HandlerFunc, m ...MiddlewareFunc) RouteInfo {
	return declareFiberRoute(&r.BaseRouter, r.detectRouteConflict, r.routeRegistration, r.conflictPolicy, r.pathConflictMode, method, pathStr, handler, m...)
}

func (r *FiberV3Router) TryReplace(method HTTPMethod, pathStr string, handler HandlerFunc, m ...MiddlewareFunc) (RouteInfo, error) {
	return r.TryReplaceWithOptions(method, pathStr, handler, RouteMutationOptions{}, m...)
}

func (r *FiberV3Router) TryReplaceWithOptions(method HTTPMethod, pathStr string, handler HandlerFunc, options RouteMutationOptions, m ...MiddlewareFunc) (RouteInfo, error) {
	return replaceFiberRoute(&r.BaseRouter, method, pathStr, handler, options, m...)
}

func (r *FiberV3Router) TryUpsert(method HTTPMethod, pathStr string, handler HandlerFunc, m ...MiddlewareFunc) (RouteInfo, bool, error) {
	return r.TryUpsertWithOptions(method, pathStr, handler, RouteMutationOptions{}, m...)
}

func (r *FiberV3Router) TryUpsertWithOptions(method HTTPMethod, pathStr string, handler HandlerFunc, options RouteMutationOptions, m ...MiddlewareFunc) (RouteInfo, bool, error) {
	return upsertFiberRoute(&r.BaseRouter, r.detectRouteConflict, r.routeRegistration, r.conflictPolicy, r.pathConflictMode, method, pathStr, handler, options, m...)
}

func (r *FiberV3Router) Get(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(GET, path, handler, mw...)
}

func (r *FiberV3Router) Post(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(POST, path, handler, mw...)
}

func (r *FiberV3Router) Put(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(PUT, path, handler, mw...)
}

func (r *FiberV3Router) Delete(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(DELETE, path, handler, mw...)
}

func (r *FiberV3Router) Patch(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(PATCH, path, handler, mw...)
}

func (r *FiberV3Router) Head(path string, handler HandlerFunc, mw ...MiddlewareFunc) RouteInfo {
	return r.Handle(HEAD, path, handler, mw...)
}

func (r *FiberV3Router) WebSocket(path string, config WebSocketConfig, handler func(WebSocketContext) error) RouteInfo {
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
	fiberHandler := fiberV3WebSocketHandler(config, handler)
	routeHandler := func(ctx Context) error {
		fc, ok := ctx.(*fiberV3Context)
		if !ok || fc == nil || fc.ctx == nil {
			return fmt.Errorf("expected fiberV3Context, got %T", ctx)
		}
		return fiberHandler(fc)
	}
	route := r.addRoute(GET, fullPath, routeHandler, "websocket", slices.Clone(r.middlewares))
	r.routeRegistration(route)
	changed = true

	return route
}

func (r *FiberV3Router) PrintRoutes() {
	r.BaseRouter.PrintRoutes()
}

func (r *FiberV3Router) WithLogger(logger Logger) Router[*fiber.App] {
	r.BaseRouter.WithLogger(logger)
	return r
}

func (r *FiberV3Router) ValidateRoutes() []error {
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
