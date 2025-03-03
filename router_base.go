package router

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"strings"
)

type routerRoot struct {
	routes      []*RouteDefinition
	namedRoutes map[string]*RouteDefinition
}

// Common fields for both FiberRouter and HTTPRouter
type BaseRouter struct {
	prefix            string
	middlewares       []namedMiddleware
	routes            []*RouteDefinition
	lateRoutes        []*lateRoute
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
	if routeName != "" {
		if br.root.namedRoutes == nil {
			br.root.namedRoutes = make(map[string]*RouteDefinition)
		}
		br.root.namedRoutes[routeName] = r
	}

	return r
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

	br.lateRoutes = append(br.lateRoutes, d)
}

func (br *BaseRouter) registerLateRoutes() {
	// for _, route := range br.lateRoutes {
	// 	br.Handle(route.method, route.path, route.handler, route.name, route.allMw)
	// }

	// br.lateRoutes = make([]*lateRoute, 0)
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
	cfg := Static{
		Root:  root,
		Index: "index.html",
	}
	if len(config) > 0 {
		cfg = config[0]
	}
	if cfg.Root == "" {
		cfg.Root = "."
	}

	// Normalize prefix and root
	prefix = path.Clean("/" + prefix)

	// Create filesystem
	var fileSystem fs.FS
	if cfg.FS != nil {
		fileSystem = cfg.FS
	} else {
		fileSystem = os.DirFS(cfg.Root)
	}

	handler := func(c Context) error {
		r.logger.Info("Public static handler")
		// Get path relative to prefix
		reqPath := c.Path()
		if !strings.HasPrefix(reqPath, prefix) {
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

		if cfg.ModifyResponse != nil {
			if err := cfg.ModifyResponse(c); err != nil {
				return err
			}
		}

		return c.Send(content)
	}

	return prefix, handler
}
