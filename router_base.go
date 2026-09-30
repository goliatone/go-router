package router

import (
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type routerRoot struct {
	registrationMu      sync.Mutex
	state               RegistrationState
	revision            uint64
	routes              []*RouteDefinition
	mountedRoutes       []*RouteDefinition
	namedRoutes         map[string]*namedRouteBinding
	namedRouteConflicts map[*RouteDefinition]error
	lateRoutes          []*lateRoute
	missHandlers        map[HTTPMethod]*missHandler
	deferredRoutes      []*RouteDefinition
	deferredRegistered  bool
	matchingSemantics   RouteMatchingSemantics
}

func (root *routerRoot) registrationState() RegistrationState {
	if root.state == "" {
		return RegistrationCollecting
	}
	return root.state
}

func (root *routerRoot) beginMutation(operation string, method HTTPMethod, path string) {
	root.registrationMu.Lock()
	state := root.registrationState()
	if state != RegistrationCollecting {
		root.registrationMu.Unlock()
		panic(newRegistrationError(operation, method, path, state, ErrRouterSealed))
	}
}

func (root *routerRoot) endMutation(changed bool) {
	if changed {
		root.revision++
	}
	root.registrationMu.Unlock()
}

// beginFinalization transitions the registration plan while retaining the
// root lock. Callers must pair a successful call with finishFinalization so
// snapshots cannot observe a sealed but only partially mounted route table.
func (root *routerRoot) beginFinalization() bool {
	root.registrationMu.Lock()
	if root.registrationState() == RegistrationSealed {
		root.registrationMu.Unlock()
		return false
	}
	root.state = RegistrationFinalizing
	root.revision++
	return true
}

func (root *routerRoot) finishFinalization() {
	root.state = RegistrationSealed
	root.revision++
	root.registrationMu.Unlock()
}

func (root *routerRoot) recordMounted(route *RouteDefinition) {
	root.mountedRoutes = append(root.mountedRoutes, route)
}

type routeNameMode int

const (
	routeNameModeUnspecified routeNameMode = iota
	routeNameModeInternal
	routeNameModePublic
)

type namedRouteBinding struct {
	Name  string
	Route *RouteDefinition
	Mode  routeNameMode
}

// Common fields for both FiberRouter and HTTPRouter
type BaseRouter struct {
	mx                sync.Mutex
	prefix            string
	middlewares       []namedMiddleware
	routes            []*RouteDefinition
	logger            Logger
	namedRoutePolicy  NamedRouteCollisionPolicy
	root              *routerRoot
	views             Views
	passLocalsToViews bool
}

type namedMiddleware struct {
	Name string
	Mw   MiddlewareFunc
}

type missHandler struct {
	Method   HTTPMethod
	Handlers []NamedHandler
}

// ChainHandlers builds the final handler chain:
// 1. Start with the final route handler.
// 2. Apply route-level middlewares in reverse order.
// 3. Apply group-level and then global middlewares in reverse order.
// Result: a slice of NamedHandler forming the chain.
func chainHandlers(finalHandler HandlerFunc, routeName string, middlewares []namedMiddleware) []NamedHandler {
	// We'll build the chain from the bottom (final handler) up.
	chain := []NamedHandler{{Name: routeName, Handler: finalHandler}}

	// Apply middlewares in reverse order, each wrapping the current chain head.
	for i := len(middlewares) - 1; i >= 0; i-- {
		m := middlewares[i]
		next := chain[0].Handler
		mwHandler := m.Mw(next)
		chain = append([]NamedHandler{{Name: m.Name, Handler: mwHandler}}, chain...)
	}

	return chain
}

//	func (br *baseRouter) PrintRoutes() {
//		// Print a table similar to Fiber's output
//		fmt.Println("method  | path           | name        | handlers ")
//		fmt.Println("------  | ----           | ----        | -------- ")
//		for _, rt := range br.routes {
//			handlerNames := []string{}
//			for _, h := range rt.Handlers {
//				handlerNames = append(handlerNames, h.Name)
//			}
//			fmt.Printf("%-7s | %-14s | %-11s | %s\n",
//				rt.Method, rt.Path, rt.name, strings.Join(handlerNames, " -> "))
//		}
//	}

func (br *BaseRouter) PrintRoutes() {
	for _, rt := range br.root.routes {
		fmt.Printf("%s %s (%s)\n", rt.Method, rt.Path, rt.Name)
		if rt.Description != "" {
			fmt.Printf("  Description: %s\n", rt.Description)
		}
		if len(rt.Tags) > 0 {
			fmt.Printf("  Tags: %v\n", rt.Tags)
		}
		if len(rt.Responses) > 0 {
			fmt.Printf("  Responses: %v\n", rt.Responses)
		}
		for i, h := range rt.Handlers {
			fmt.Printf("  %02d: %s\n", i, h.Name)
		}
		fmt.Println()
	}
}

func (br *BaseRouter) addRoute(method HTTPMethod, fullPath string, finalHandler HandlerFunc, routeName string, allMw []namedMiddleware) *RouteDefinition {
	chain := chainHandlers(finalHandler, routeName, allMw)
	r :=
		&RouteDefinition{
			Method:      method,
			Path:        fullPath,
			Name:        routeName,
			Handlers:    chain,
			middlewares: append([]namedMiddleware(nil), allMw...),
		}

	if routeName != "" {
		br.applyInternalRouteName(r, routeName)
	}

	br.root.routes = append(br.root.routes, r)

	return r
}

func (br *BaseRouter) buildNamedMiddlewares(extra []MiddlewareFunc) []namedMiddleware {
	allMw := append([]namedMiddleware{}, br.middlewares...)
	for _, mw := range extra {
		allMw = append(allMw, namedMiddleware{
			Name: funcName(mw),
			Mw:   mw,
		})
	}
	return allMw
}

func (br *BaseRouter) setMissHandler(method HTTPMethod, finalHandler HandlerFunc, allMw []namedMiddleware) {
	if finalHandler == nil {
		return
	}
	if br.root.missHandlers == nil {
		br.root.missHandlers = make(map[HTTPMethod]*missHandler)
	}
	br.root.missHandlers[method] = &missHandler{
		Method:   method,
		Handlers: chainHandlers(finalHandler, "", allMw),
	}
}

func (br *BaseRouter) missHandler(method HTTPMethod) *missHandler {
	if br.root.missHandlers == nil {
		return nil
	}
	return br.root.missHandlers[method]
}

func (br *BaseRouter) addNamedRoute(routeName string, route *RouteDefinition) error {
	return br.applyPublicRouteName(route, routeName)
}

func (br *BaseRouter) applyPublicRouteName(route *RouteDefinition, routeName string) error {
	if route == nil {
		return nil
	}
	if routeName == "" {
		br.removePublicRouteBinding(route)
		route.publicName = ""
		route.Name = ""
		route.nameMode = routeNameModePublic
		br.clearNamedRouteConflict(route)
		return nil
	}

	oldName := route.effectivePublicName()
	if err := br.registerNamedRoute(route, routeName, routeNameModePublic); err != nil {
		return err
	}

	if oldName != "" && oldName != routeName {
		br.removePublicRouteBinding(route, oldName)
	}

	route.publicName = routeName
	route.Name = routeName
	route.nameMode = routeNameModePublic
	br.clearNamedRouteConflict(route)
	return nil
}

func (br *BaseRouter) setPublicRouteName(route *RouteDefinition, routeName string, after func()) error {
	path := ""
	method := HTTPMethod("")
	if route != nil {
		path = route.Path
		method = route.Method
	}
	br.root.beginMutation("name route", method, path)
	changed := false
	defer func() { br.root.endMutation(changed) }()

	if err := br.applyPublicRouteName(route, routeName); err != nil {
		return err
	}
	if after != nil {
		after()
	}
	changed = true
	return nil
}

func (br *BaseRouter) applyInternalRouteName(route *RouteDefinition, routeName string) {
	if route == nil {
		return
	}
	route.Name = routeName
	route.nameMode = routeNameModeInternal
	if route.publicName == routeName {
		route.publicName = ""
	}
}

func (br *BaseRouter) registerNamedRoute(route *RouteDefinition, routeName string, mode routeNameMode) error {
	if routeName == "" || route == nil {
		return nil
	}

	if mode != routeNameModePublic {
		return nil
	}

	br.mx.Lock()
	defer br.mx.Unlock()

	if br.root.namedRoutes == nil {
		br.root.namedRoutes = make(map[string]*namedRouteBinding)
	}

	existing := br.root.namedRoutes[routeName]
	if existing == nil || existing.Route == route {
		br.root.namedRoutes[routeName] = &namedRouteBinding{
			Name:  routeName,
			Route: route,
			Mode:  mode,
		}
		return nil
	}

	if existing.Route != nil && existing.Route.Path == route.Path {
		return nil
	}

	policy := br.namedRoutePolicy.normalize()
	conflictErr := newRouteNameConflictError(routeName, existing.Route, route, policy)
	br.recordNamedRouteConflict(route, conflictErr)

	switch policy {
	case NamedRouteCollisionPolicySkip:
		return conflictErr
	case NamedRouteCollisionPolicyError:
		if br.logger != nil {
			br.logger.Warn("named route conflict detected: %v", conflictErr)
		}
		return conflictErr
	default:
		br.root.namedRoutes[routeName] = &namedRouteBinding{
			Name:  routeName,
			Route: route,
			Mode:  mode,
		}
		br.clearNamedRouteConflict(route)
		return nil
	}
}

func (br *BaseRouter) recordNamedRouteConflict(route *RouteDefinition, err error) {
	if route == nil || err == nil {
		return
	}
	if br.root.namedRouteConflicts == nil {
		br.root.namedRouteConflicts = make(map[*RouteDefinition]error)
	}
	br.root.namedRouteConflicts[route] = err
}

func (br *BaseRouter) clearNamedRouteConflict(route *RouteDefinition) {
	if route == nil || br.root.namedRouteConflicts == nil {
		return
	}
	delete(br.root.namedRouteConflicts, route)
}

func (br *BaseRouter) removePublicRouteBinding(route *RouteDefinition, names ...string) {
	if route == nil || br.root.namedRoutes == nil {
		return
	}

	targetName := route.effectivePublicName()
	if len(names) > 0 && names[0] != "" {
		targetName = names[0]
	}
	if targetName == "" {
		return
	}

	br.mx.Lock()
	defer br.mx.Unlock()

	binding := br.root.namedRoutes[targetName]
	if binding == nil || binding.Route != route {
		return
	}

	for _, candidate := range br.root.routes {
		if candidate == nil || candidate == route {
			continue
		}
		if candidate.effectivePublicName() != targetName {
			continue
		}
		br.root.namedRoutes[targetName] = &namedRouteBinding{
			Name:  targetName,
			Route: candidate,
			Mode:  routeNameModePublic,
		}
		return
	}

	delete(br.root.namedRoutes, targetName)
}

func (br *BaseRouter) namedRouteConflicts() []error {
	if len(br.root.namedRouteConflicts) == 0 {
		return nil
	}

	errs := make([]error, 0, len(br.root.namedRouteConflicts))
	seen := make(map[*RouteDefinition]struct{}, len(br.root.namedRouteConflicts))
	for _, route := range br.root.routes {
		if err, ok := br.root.namedRouteConflicts[route]; ok {
			errs = append(errs, err)
			seen[route] = struct{}{}
		}
	}
	for route, err := range br.root.namedRouteConflicts {
		if _, ok := seen[route]; ok {
			continue
		}
		errs = append(errs, err)
	}
	return errs
}

type lateRoute struct {
	method      HTTPMethod
	path        string
	handler     HandlerFunc
	name        string
	mode        routeNameMode
	middlewares []namedMiddleware
}

func (br *BaseRouter) addLateRoute(method HTTPMethod, pathStr string, handler HandlerFunc, routeName string, m ...MiddlewareFunc) {
	br.addLateRouteWithMode(method, pathStr, handler, routeName, routeNameModePublic, m...)
}

func (br *BaseRouter) addInternalLateRoute(method HTTPMethod, pathStr string, handler HandlerFunc, routeName string, m ...MiddlewareFunc) {
	br.addLateRouteWithMode(method, pathStr, handler, routeName, routeNameModeInternal, m...)
}

func (br *BaseRouter) addLateRouteWithMode(method HTTPMethod, pathStr string, handler HandlerFunc, routeName string, mode routeNameMode, m ...MiddlewareFunc) {
	br.root.beginMutation("register late route", method, pathStr)
	defer br.root.endMutation(true)

	d := &lateRoute{
		method:  method,
		path:    pathStr,
		handler: handler,
		name:    routeName,
		mode:    mode,
		// Late routes are mounted by the root router, so capture the declaring
		// router's chain now, as immediate registration does. Otherwise group
		// middleware such as auth guards would be skipped.
		middlewares: br.buildNamedMiddlewares(m),
	}

	br.root.lateRoutes = append(br.root.lateRoutes, d)
}

// lateRouteRegistrar mounts a late route at its full path using exactly the
// middleware chain captured when the route was declared.
type lateRouteRegistrar interface {
	handleLateRoute(method HTTPMethod, fullPath string, handler HandlerFunc, middlewares []namedMiddleware) RouteInfo
}

func (br *BaseRouter) registerLateRoutes(reg lateRouteRegistrar) {
	for _, route := range br.root.lateRoutes {
		ri := reg.handleLateRoute(route.method, route.path, route.handler, route.middlewares)
		if route.name != "" {
			if route.mode == routeNameModeInternal {
				if def, ok := ri.(*RouteDefinition); ok {
					br.applyInternalRouteName(def, route.name)
				}
				continue
			}
			ri.SetName(route.name)
		}
	}
	if len(br.root.lateRoutes) > 0 {
		br.root.lateRoutes = br.root.lateRoutes[:0]
	}
}

func (br *BaseRouter) WithLogger(logger Logger) *BaseRouter {
	br.logger = logger
	return br
}

func (br *BaseRouter) Routes() []RouteDefinition {
	br.root.registrationMu.Lock()
	defer br.root.registrationMu.Unlock()
	return cloneRouteDefinitions(br.root.routes)
}

func (br *BaseRouter) RegistrationSnapshot() RegistrationSnapshot {
	br.root.registrationMu.Lock()
	defer br.root.registrationMu.Unlock()
	return RegistrationSnapshot{
		State:             br.root.registrationState(),
		Revision:          br.root.revision,
		MatchingSemantics: br.root.matchingSemantics,
		DeclaredRoutes:    cloneRouteDefinitions(br.root.routes),
		MountedRoutes:     cloneRouteDefinitions(br.root.mountedRoutes),
	}
}

func (br *BaseRouter) RouteMatchingSemantics() RouteMatchingSemantics {
	if br == nil || br.root == nil {
		return RouteMatchingSemantics{}
	}
	br.root.registrationMu.Lock()
	defer br.root.registrationMu.Unlock()
	return br.root.matchingSemantics
}

func (br *BaseRouter) TrailingSlashDistinct() bool {
	return br.RouteMatchingSemantics().TrailingSlashDistinct
}

func (br *BaseRouter) RoutingCapabilities() RoutingCapabilities {
	return RoutingCapabilities{
		RouteNamePolicy: true,
		OwnershipChecks: true,
		Manifest:        true,
	}
}

func cloneRouteDefinitions(routes []*RouteDefinition) []RouteDefinition {
	defs := make([]RouteDefinition, len(routes))
	for i, rt := range routes {
		defs[i] = *rt
		defs[i].Handlers = append([]NamedHandler(nil), rt.Handlers...)
		defs[i].middlewares = append([]namedMiddleware(nil), rt.middlewares...)
	}
	return defs
}

func (br *BaseRouter) GetRoute(name string) *RouteDefinition {
	if br.root.namedRoutes == nil {
		return nil
	}
	binding := br.root.namedRoutes[name]
	if binding == nil || binding.Mode != routeNameModePublic {
		return nil
	}
	return binding.Route
}

func (br *BaseRouter) RouteNameFromPath(method string, pathPattern string) (string, bool) {
	for _, route := range br.root.routes {
		if route.Method == HTTPMethod(method) && route.Path == pathPattern {
			if route.Name != "" {
				return route.Name, true
			}
		}
	}
	return "", false
}

func (br *BaseRouter) joinPath(prefix, path string) string {
	// Trim excess slashes
	prefix = strings.TrimRight(prefix, "/")
	path = strings.TrimLeft(path, "/")

	// Handle special cases where both are empty
	if prefix == "" && path == "" {
		return "/"
	}

	// Ensure proper concatenation
	if prefix == "" {
		return "/" + path
	}
	if path == "" {
		return prefix
	}

	return prefix + "/" + path
}

// Static file handler implementation
//
//nolint:gocyclo,nestif,funlen // Static serving supports filesystem selection, browsing, indexes, headers, and response hooks in one handler.
func (r *BaseRouter) makeStaticHandler(prefix, root string, config ...Static) (string, HandlerFunc) {
	baseCfg := Static{
		Root:  root,
		Index: "index.html",
	}
	cfg := mergeStaticConfig(baseCfg, config...)

	prefix = path.Clean("/" + prefix)

	if root != "" && len(config) > 0 && config[0].Root != "" && config[0].Root != root {
		r.logger.Warn("static configuration overrides positional root %q with config root %q for prefix %q", root, config[0].Root, prefix)
	}

	if suspect := detectConsecutiveDuplicateSegment(cfg.Root); suspect != "" {
		r.logger.Warn("static configuration root %q contains duplicated segment %q for prefix %q", cfg.Root, suspect, prefix)
	}

	fileSystem, fsErr := r.prepareStaticFilesystem(prefix, cfg)
	if fsErr != nil {
		r.logger.Error("static configuration for prefix %q is invalid: %v", prefix, fsErr)
		return prefix, staticConfigErrorHandler(prefix, fsErr, r.logger)
	}

	handler := func(c Context) error {
		r.logger.Info("Public static handler")
		// Get path relative to prefix. Fiber matches routes case-insensitively,
		// so "/STATIC/x" can reach a "/static" mount; serving it would slip past
		// guards that compare the prefix exactly. Answer 404 rather than calling
		// c.Next(), which would end the chain with an empty 200.
		reqPath := c.Path()
		if prefix != "/" {
			if reqPath != prefix && !strings.HasPrefix(reqPath, prefix+"/") {
				return c.Status(404).SendString("Not Found")
			}
		} else if !strings.HasPrefix(reqPath, "/") {
			return c.Status(404).SendString("Not Found")
		}

		// Strip the prefix and the one separator after it. Only a single slash is
		// removed so the empty segment in "/static//x" (or "//x" on a root mount)
		// reaches the canonical check below.
		filePath := strings.TrimPrefix(reqPath, prefix)
		if prefix != "/" {
			filePath = strings.TrimPrefix(filePath, "/")
		}

		// Guards and route matching saw reqPath as received, so serving a cleaned
		// variant would resolve a different file than the one they inspected
		// (e.g. "/static/./admin/x" slipping past a guard on "/static/admin").
		// Non-canonical paths get a 404 rather than a redirect to the cleaned path.
		if !isCanonicalStaticPath(filePath) {
			r.logger.Info("[WARN] public rejected non-canonical path %q", reqPath)
			return c.Status(404).SendString("Not Found")
		}

		// Only a name taken from the request needs its spelling verified below;
		// the configured index name is trusted.
		fromRequest := filePath != ""
		if filePath == "" && cfg.Browse {
			filePath = "."
		} else if filePath == "" {
			filePath = cfg.Index
		}
		// The request path is already canonical; Clean only drops a trailing
		// slash and normalizes the configured index name.
		filePath = path.Clean(filePath)
		if filePath == "/" {
			filePath = "."
		}

		// Check if file exists and get info
		f, err := fileSystem.Open(filePath)
		if err != nil {
			if isStaticNotFound(err) {
				r.logger.Info("[WARN] public did not find path")
				return c.Status(404).SendString("Not Found")
			}
			r.logger.Error("public failed to open filepath: %s", err)
			return c.Status(500).SendString("Internal Server Error")
		}
		defer func() {
			if f == nil {
				return
			}
			if closeErr := f.Close(); closeErr != nil {
				r.logger.Error("public failed to close file: %s", closeErr)
			}
		}()

		// Same reasoning as the canonical check: a case-insensitive filesystem
		// also opens "ADMIN/x" for an "admin/x" entry, a spelling that a guard
		// comparing exact names never matched. Serve stored spellings only.
		if fromRequest {
			exact, verifyErr := hasExactStaticName(fileSystem, filePath)
			if verifyErr != nil && !isStaticNotFound(verifyErr) {
				r.logger.Error("public failed to verify file name: %s", verifyErr)
				return c.Status(500).SendString("Internal Server Error")
			}
			if !exact {
				r.logger.Info("[WARN] public rejected path %q that differs from the stored name", reqPath)
				return c.Status(404).SendString("Not Found")
			}
		}

		stat, err := f.Stat()
		if err != nil {
			r.logger.Error("public failed to stat file: %s", err)
			return c.Status(500).SendString("Internal Server Error")
		}

		// Handle directory
		if stat.IsDir() {
			if cfg.Browse {
				return renderDirectoryListing(c, reqPath, filePath, fileSystem)
			}
			if !cfg.Browse {
				// Try to serve index file
				indexPath := path.Join(filePath, cfg.Index)
				indexFile, openErr := fileSystem.Open(indexPath)
				if openErr == nil {
					if closeErr := f.Close(); closeErr != nil {
						if indexCloseErr := indexFile.Close(); indexCloseErr != nil {
							r.logger.Error("public failed to close index file: %s", indexCloseErr)
						}
						f = nil
						return closeErr
					}
					f = indexFile
					filePath = indexPath
				} else {
					r.logger.Info("[WARN] public did not find dir in fs")
					return c.Status(404).SendString("Not Found")
				}
			}
		}

		// Set headers
		if cfg.MaxAge > 0 {
			c.SetHeader("Cache-Control", fmt.Sprintf("public, max-age=%d", cfg.MaxAge))
		}

		// Set content type based on extension
		ext := path.Ext(filePath)
		mimeType := mime.TypeByExtension(ext)
		if mimeType != "" {
			c.SetHeader("Content-Type", mimeType)
		}

		if cfg.Download {
			c.SetHeader("Content-Disposition", "attachment; filename="+path.Base(filePath))
		}

		// Read and send file
		content, err := io.ReadAll(f)
		if err != nil {
			r.logger.Error("public failed to read file: %s", err)
			return c.Status(500).SendString("Internal Server Error")
		}

		// TODO: We might want to modify ModifyResponse to also take in the content
		if cfg.ModifyResponse != nil {
			if err := cfg.ModifyResponse(c); err != nil {
				return err
			}
		}

		return c.Send(content)
	}

	return prefix, handler
}

//nolint:nestif // Filesystem preparation preserves legacy behavior across direct, rooted, and composite filesystems.
func (r *BaseRouter) prepareStaticFilesystem(prefix string, cfg Static) (fs.FS, error) {
	if cfg.FS != nil {
		root := normalizeFSRoot(cfg.Root)
		fsToUse := cfg.FS

		// Avoid validating embedded/composite filesystems via fs.Stat(".", ...)
		// because many fs.FS implementations don't expose a "." entry but do
		// correctly serve files via Open(name). When a non-dot root is provided,
		// validate it against the original filesystem instead.
		if root != "." {
			if _, err := fs.Stat(cfg.FS, root); err != nil {
				return nil, fmt.Errorf("filesystem root validation failed for %q: %w", root, err)
			}
			sub, err := fs.Sub(cfg.FS, root)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve filesystem root %q: %w", root, err)
			}
			fsToUse = sub
		}
		return fsToUse, nil
	}

	localRoot := cfg.Root
	if localRoot == "" {
		localRoot = "."
	}

	cleaned := filepath.Clean(localRoot)
	info, err := os.Stat(cleaned)
	if err != nil {
		r.logger.Warn("static local root %q for prefix %q not accessible during startup: %v", cleaned, prefix, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("static root %q must be a directory", cleaned)
	}

	return os.DirFS(cleaned), nil
}

func mergeStaticConfig(base Static, overrides ...Static) Static {
	if base.Index == "" {
		base.Index = "index.html"
	}

	if base.Root == "" {
		base.Root = "."
	}

	if len(overrides) == 0 {
		return base
	}

	o := overrides[0]
	if o.FS != nil {
		base.FS = o.FS
	}

	if o.Root != "" {
		base.Root = o.Root
	}

	if o.Index != "" {
		base.Index = o.Index
	}

	base.Browse = o.Browse
	base.MaxAge = o.MaxAge
	base.Download = o.Download
	base.Compress = o.Compress

	if o.ModifyResponse != nil {
		base.ModifyResponse = o.ModifyResponse
	}

	if base.Root == "" {
		base.Root = "."
	}
	if base.Index == "" {
		base.Index = "index.html"
	}

	return base
}

func normalizeFSRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return "."
	}
	root = strings.TrimPrefix(root, "./")
	root = strings.TrimPrefix(root, "/")
	root = path.Clean(root)
	if root == "." || root == "" {
		return "."
	}
	return root
}

// isCanonicalStaticPath reports whether rel, a request path with the static
// prefix and its separator removed, is already canonical: no ".", ".." or
// empty segments, ignoring a single trailing slash. fs.ValidPath also rejects
// invalid UTF-8.
func isCanonicalStaticPath(rel string) bool {
	if rel == "" {
		return true
	}
	rel = strings.TrimSuffix(rel, "/")
	return rel != "." && fs.ValidPath(rel)
}

// isStaticNotFound reports whether an Open error means the request does not
// name a servable file. os.DirFS returns fs.ErrInvalid for names it cannot
// represent (such as NUL bytes) and ENOTDIR when a path continues past a
// regular file; embedded filesystems report both as fs.ErrNotExist.
func isStaticNotFound(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) || errors.Is(err, syscall.ENOTDIR)
}

// hasExactStaticName reports whether every segment of name, a canonical path
// that fsys opened, is spelled exactly like the directory entry it resolved
// to. A segment is compared against its directory listing only when a
// different-case spelling of it resolves too, so case-sensitive filesystems
// pay at most one failed Stat per segment instead of a directory read.
// Segments without letters are not probed, so aliases that do not involve
// case (a Windows 8.3 short name of an all-digit directory, say) still pass.
func hasExactStaticName(fsys fs.FS, name string) (bool, error) {
	dir := "."
	for seg := range strings.SplitSeq(name, "/") {
		if resolvesOtherCase(fsys, dir, seg) {
			entries, err := fs.ReadDir(fsys, dir)
			if err != nil {
				return false, err
			}
			if !slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == seg }) {
				return false, nil
			}
		}
		dir = path.Join(dir, seg)
	}
	return true, nil
}

// resolvesOtherCase reports whether fsys also resolves seg in dir when it is
// spelled in upper case, or in lower case if it already is all upper case.
func resolvesOtherCase(fsys fs.FS, dir, seg string) bool {
	other := strings.ToUpper(seg)
	if other == seg {
		other = strings.ToLower(seg)
	}
	if other == seg {
		return false
	}
	_, err := fs.Stat(fsys, path.Join(dir, other))
	return err == nil
}

func detectConsecutiveDuplicateSegment(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	var last string
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == last {
			return part
		}
		last = part
	}
	return ""
}

func staticConfigErrorHandler(prefix string, err error, logger Logger) HandlerFunc {
	return func(c Context) error {
		logger.Error("static handler for prefix %q is misconfigured: %v", prefix, err)
		return c.Status(500).SendString("Static file configuration error")
	}
}

func renderDirectoryListing(c Context, reqPath, dirPath string, fileSystem fs.FS) error {
	entries, err := fs.ReadDir(fileSystem, dirPath)
	if err != nil {
		return c.Status(500).SendString("Failed to read directory")
	}

	basePath := strings.TrimSuffix(reqPath, "/")
	if basePath == "" {
		basePath = "/"
	}

	var body strings.Builder
	body.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Index of ")
	body.WriteString(html.EscapeString(reqPath))
	body.WriteString("</title></head><body><h1>Index of ")
	body.WriteString(html.EscapeString(reqPath))
	body.WriteString("</h1><ul>")

	if basePath != "/" {
		parent := path.Dir(basePath)
		if parent == "." {
			parent = "/"
		}
		body.WriteString("<li><a href=\"")
		body.WriteString(html.EscapeString(parent))
		body.WriteString("\">..</a></li>")
	}

	for _, entry := range entries {
		name := entry.Name()
		label := name
		href := path.Join(basePath, name)
		if entry.IsDir() {
			label += "/"
			href += "/"
		}

		body.WriteString("<li><a href=\"")
		body.WriteString(html.EscapeString(href))
		body.WriteString("\">")
		body.WriteString(html.EscapeString(label))
		body.WriteString("</a>")
		if info, infoErr := entry.Info(); infoErr == nil && !info.IsDir() {
			body.WriteString(" (")
			body.WriteString(strconv.FormatInt(info.Size(), 10))
			body.WriteString(" bytes)")
		}
		body.WriteString("</li>")
	}

	body.WriteString("</ul></body></html>")

	c.SetHeader("Content-Type", "text/html; charset=utf-8")
	return c.SendString(body.String())
}
