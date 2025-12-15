package router

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

type routerRoot struct {
	routes      []*RouteDefinition
	namedRoutes map[string]*RouteDefinition
	lateRoutes  []*lateRoute
}

// Common fields for both FiberRouter and HTTPRouter
type BaseRouter struct {
	mx                sync.Mutex
	prefix            string
	middlewares       []namedMiddleware
	routes            []*RouteDefinition
	logger            Logger
	root              *routerRoot
	views             Views
	passLocalsToViews bool
}

type namedMiddleware struct {
	Name string
	Mw   MiddlewareFunc
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
			Method:   method,
			Path:     fullPath,
			Name:     routeName,
			Handlers: chain,
		}

	br.root.routes = append(br.root.routes, r)

	// If the route has a name, also store it in the map
	br.addNamedRoute(routeName, r)

	return r
}

func (br *BaseRouter) addNamedRoute(routeName string, route *RouteDefinition) {
	if routeName == "" {
		return
	}
	br.mx.Lock()
	defer br.mx.Unlock()

	if br.root.namedRoutes == nil {
		br.root.namedRoutes = make(map[string]*RouteDefinition)
	}

	if route.Name != routeName {
		route.Name = routeName
	}

	br.root.namedRoutes[route.Name] = route
}

type lateRoute struct {
	method  HTTPMethod
	path    string
	handler HandlerFunc
	name    string
	mw      []MiddlewareFunc
}

func (br *BaseRouter) addLateRoute(method HTTPMethod, pathStr string, handler HandlerFunc, routeName string, m ...MiddlewareFunc) {
	// method HTTPMethod, pathStr string, handler HandlerFunc, m ...MiddlewareFunc

	d := &lateRoute{
		method:  method,
		path:    pathStr,
		handler: handler,
		name:    routeName,
		mw:      m,
	}

	br.root.lateRoutes = append(br.root.lateRoutes, d)
}

type lateRouteRegistrar interface {
	Handle(method HTTPMethod, path string, handler HandlerFunc, middlewares ...MiddlewareFunc) RouteInfo
}

func (br *BaseRouter) registerLateRoutes(reg lateRouteRegistrar) {
	for _, route := range br.root.lateRoutes {
		ri := reg.Handle(route.method, route.path, route.handler, route.mw...)
		if route.name != "" {
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
	defs := make([]RouteDefinition, len(br.root.routes))
	for i, rt := range br.root.routes {
		defs[i] = *rt
	}
	return defs
}

func (br *BaseRouter) GetRoute(name string) *RouteDefinition {
	if br.root.namedRoutes == nil {
		return nil
	}
	return br.root.namedRoutes[name]
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
		// Get path relative to prefix
		reqPath := c.Path()
		if prefix != "/" {
			if reqPath != prefix && !strings.HasPrefix(reqPath, prefix+"/") {
				return c.Next()
			}
		} else if !strings.HasPrefix(reqPath, "/") {
			return c.Next()
		}

		// Strip prefix and clean path
		filePath := strings.TrimPrefix(reqPath, prefix)
		filePath = strings.TrimPrefix(filePath, "/") // Remove leading slash for fs.FS

		if filePath == "" {
			filePath = cfg.Index
		}

		// Check if file exists and get info
		f, err := fileSystem.Open(filePath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				r.logger.Info("[WARN] public did not find path")
				return c.Status(404).SendString("Not Found")
			}
			r.logger.Error("public failed to open filepath: %s", err)
			return c.Status(500).SendString(err.Error())
		}
		defer f.Close()

		stat, err := f.Stat()
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}

		// Handle directory
		if stat.IsDir() {
			if !cfg.Browse {
				// Try to serve index file
				indexPath := path.Join(filePath, cfg.Index)
				if f, err := fileSystem.Open(indexPath); err == nil {
					stat, _ = f.Stat()
					filePath = indexPath
					f.Close()
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
			return c.Status(500).SendString(err.Error())
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
