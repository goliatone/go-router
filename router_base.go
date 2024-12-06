package router

import (
	"fmt"
)

type routerRoot struct {
	routes []*registeredRoute
}

// Common fields for both FiberRouter and HTTPRouter
type baseRouter struct {
	prefix      string
	middlewares []namedMiddleware
	routes      []*registeredRoute
	logger      Logger
	root        *routerRoot
}

type namedMiddleware struct {
	Name string
	Mw   MiddlewareFunc
}

type registeredRoute struct {
	method    HTTPMethod
	path      string
	name      string
	desc      string
	tags      []string
	responses map[int]string
	handlers  []NamedHandler
}

func (r *registeredRoute) Description(d string) RouteInfo {
	r.desc = d
	return r
}

func (r *registeredRoute) Tags(t ...string) RouteInfo {
	r.tags = append(r.tags, t...)
	return r
}

func (r *registeredRoute) Responses(resp map[int]string) RouteInfo {
	if r.responses == nil {
		r.responses = make(map[int]string)
	}
	for k, v := range resp {
		r.responses[k] = v
	}
	return r
}

func (r *registeredRoute) Method() string {
	return string(r.method)
}

func (r *registeredRoute) Path() string {
	return r.path
}

func (r *registeredRoute) Name(n string) RouteInfo {
	r.name = n
	return r
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

func (br *baseRouter) PrintRoutes() {
	for _, rt := range br.root.routes {
		fmt.Printf("%s %s (%s)\n", rt.method, rt.path, rt.name)
		if rt.desc != "" {
			fmt.Printf("  Description: %s\n", rt.desc)
		}
		if len(rt.tags) > 0 {
			fmt.Printf("  Tags: %v\n", rt.tags)
		}
		if len(rt.responses) > 0 {
			fmt.Printf("  Responses: %v\n", rt.responses)
		}
		for i, h := range rt.handlers {
			fmt.Printf("  %02d: %s\n", i, h.Name)
		}
		fmt.Println()
	}
}

func (br *baseRouter) addRoute(method HTTPMethod, fullPath string, finalHandler HandlerFunc, routeName string, allMw []namedMiddleware) *registeredRoute {
	chain := chainHandlers(finalHandler, routeName, allMw)
	r := &registeredRoute{
		method:    method,
		path:      fullPath,
		name:      routeName,
		handlers:  chain,
		tags:      []string{},
		responses: make(map[int]string),
	}
	br.root.routes = append(br.root.routes, r)
	return r
}
