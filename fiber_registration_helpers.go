package router

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type fiberRoutePlan interface {
	lateRouteRegistrar
	ValidateRoutes() []error
	registerDeferredRoutes()
	registerMissHandlers()
}

func finalizeFiberRoutes(base *BaseRouter, plan fiberRoutePlan, strict bool) error {
	base.registerLateRoutes(plan)
	if !base.root.beginFinalization() {
		return nil
	}
	defer base.root.finishFinalization()
	if strict {
		errs := plan.ValidateRoutes()
		for _, route := range base.root.routes {
			for _, handler := range route.Handlers {
				if handler.Handler == nil {
					errs = append(errs, fmt.Errorf("nil handler for %s %s", route.Method, route.Path))
				}
			}
		}
		if len(errs) > 0 {
			failure := &StartupValidationError{Err: errors.Join(errs...)}
			base.root.startupValidationError = failure.Error()
			return failure
		}
	}
	plan.registerDeferredRoutes()
	plan.registerMissHandlers()
	return nil
}

func handleFiberRoute(base *BaseRouter, conflict *routeConflict, policy HTTPRouterConflictPolicy, mode PathConflictMode, register func(*RouteDefinition), method HTTPMethod, fullPath string, handler HandlerFunc, allMw []namedMiddleware) (RouteInfo, bool) {
	if skipFiberRouteConflict(base, conflict, policy, mode, method, fullPath) {
		return noopRouteInfo, false
	}

	route := base.addRoute(method, fullPath, handler, "", allMw)
	register(route)

	return route, true
}

func upsertFiberRoute(base *BaseRouter, detect func(HTTPMethod, string) *routeConflict, register func(*RouteDefinition), policy HTTPRouterConflictPolicy, mode PathConflictMode, method HTTPMethod, pathStr string, handler HandlerFunc, options RouteMutationOptions, m ...MiddlewareFunc) (RouteInfo, bool, error) {
	fullPath := base.joinPath(base.prefix, pathStr)
	base.root.registrationMu.Lock()
	defer base.root.registrationMu.Unlock()
	if base.root.registrationState() != RegistrationCollecting {
		return nil, false, newRegistrationError("upsert route", method, fullPath, base.root.registrationState(), ErrRouterSealed)
	}
	for _, route := range base.root.routes {
		if route.Method == method && route.Path == fullPath {
			allMw := slices.Clone(route.middlewares)
			if options.ReplaceMiddleware {
				allMw = slices.Clone(base.middlewares)
			}
			if !options.MiddlewareOnAddOnly {
				for _, mw := range m {
					allMw = append(allMw, namedMiddleware{
						Name: fmt.Sprintf("%s %s %s", method, pathStr, funcName(mw)),
						Mw:   mw,
					})
				}
			}
			route.middlewares = slices.Clone(allMw)
			route.Handlers = chainHandlers(handler, route.Name, allMw)
			base.root.revision++
			return route, true, nil
		}
	}
	if conflict := detect(method, fullPath); conflict != nil {
		err := newRouteConflictError(method, fullPath, conflict, policy, mode)
		if base.logger != nil {
			base.logger.Warn("route conflict detected during upsert: %v", err)
		}
		if policy != HTTPRouterConflictLogAndContinue {
			return nil, false, err
		}
	}
	allMw := slices.Clone(base.middlewares)
	for _, mw := range m {
		allMw = append(allMw, namedMiddleware{
			Name: fmt.Sprintf("%s %s %s", method, pathStr, funcName(mw)),
			Mw:   mw,
		})
	}
	route := base.addRoute(method, fullPath, handler, "", allMw)
	register(route)
	base.root.revision++
	return route, false, nil
}

// registerFiberStaticRoutes preserves GET/HEAD registration order on both native backends.
func registerFiberStaticRoutes(base *BaseRouter, handle func(HTTPMethod, string, HandlerFunc, ...MiddlewareFunc) RouteInfo, prefix, root string, config ...Static) {
	fullPrefix := base.joinPath(base.prefix, prefix)
	path, handler := base.makeStaticHandler(fullPrefix, root, config...)
	wildcard := base.joinPath(path, "*")
	for _, method := range []HTTPMethod{GET, HEAD} {
		paths := []string{wildcard}
		if path != "/" {
			paths = append([]string{path}, paths...)
		}
		for _, routePath := range paths {
			if route, ok := handle(method, routePath, handler).(*RouteDefinition); ok {
				base.applyInternalRouteName(route, "static."+strings.ToLower(string(method)))
			}
		}
	}
}

func replaceFiberRoute(base *BaseRouter, method HTTPMethod, pathStr string, handler HandlerFunc, options RouteMutationOptions, m ...MiddlewareFunc) (RouteInfo, error) {
	fullPath := base.joinPath(base.prefix, pathStr)
	base.root.registrationMu.Lock()
	defer base.root.registrationMu.Unlock()
	if base.root.registrationState() != RegistrationCollecting {
		return nil, newRegistrationError("replace route", method, fullPath, base.root.registrationState(), ErrRouterSealed)
	}

	for _, route := range base.root.routes {
		if route.Method != method || route.Path != fullPath {
			continue
		}
		allMw := slices.Clone(route.middlewares)
		if options.ReplaceMiddleware {
			allMw = slices.Clone(base.middlewares)
		}
		for _, mw := range m {
			allMw = append(allMw, namedMiddleware{
				Name: fmt.Sprintf("%s %s %s", method, pathStr, funcName(mw)),
				Mw:   mw,
			})
		}
		route.middlewares = slices.Clone(allMw)
		route.Handlers = chainHandlers(handler, route.Name, allMw)
		base.root.revision++
		return route, nil
	}
	return nil, newRouteNotFoundError(method, fullPath)
}

func declareFiberRoute(base *BaseRouter, detect func(HTTPMethod, string) *routeConflict, register func(*RouteDefinition), policy HTTPRouterConflictPolicy, mode PathConflictMode, method HTTPMethod, pathStr string, handler HandlerFunc, m ...MiddlewareFunc) RouteInfo {
	fullPath := base.joinPath(base.prefix, pathStr)
	base.root.beginMutation("register route", method, fullPath)
	changed := false
	defer func() { base.root.endMutation(changed) }()
	if skipFiberRouteConflict(base, detect(method, fullPath), policy, mode, method, fullPath) {
		return noopRouteInfo
	}
	allMw := slices.Clone(base.middlewares)
	for _, mw := range m {
		allMw = append(allMw, namedMiddleware{Name: fmt.Sprintf("%s %s %s", method, pathStr, funcName(mw)), Mw: mw})
	}
	route := base.addRoute(method, fullPath, handler, "", allMw)
	register(route)
	changed = true
	return route
}

func skipFiberRouteConflict(base *BaseRouter, conflict *routeConflict, policy HTTPRouterConflictPolicy, mode PathConflictMode, method HTTPMethod, fullPath string) bool {
	if conflict != nil {
		err := newRouteConflictError(method, fullPath, conflict, policy, mode)
		switch policy {
		case HTTPRouterConflictLogAndSkip:
			if base.logger != nil {
				base.logger.Warn("route conflict skipped: %v", err)
			}
			return true
		case HTTPRouterConflictLogAndContinue:
			if base.logger != nil {
				base.logger.Warn("route conflict detected: %v", err)
			}
		case HTTPRouterConflictPanic:
			panic(err)
		}
	}
	return false
}

func fiberGroupBase(base *BaseRouter, prefix string) BaseRouter {
	fullPrefix := base.joinPath(base.prefix, prefix)
	base.logger.Debug("creating route group", "prefix", fullPrefix)
	return BaseRouter{prefix: fullPrefix, middlewares: slices.Clone(base.middlewares), logger: base.logger, namedRoutePolicy: base.namedRoutePolicy, routes: base.routes, root: base.root}
}
