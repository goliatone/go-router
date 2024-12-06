package router

import (
	"fmt"
)

type routerRoot struct {
	routes []*RouteDefinition
}

// Common fields for both FiberRouter and HTTPRouter
type baseRouter struct {
	prefix      string
	middlewares []namedMiddleware
	routes      []*RouteDefinition
	logger      Logger
	root        *routerRoot
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

func (br *baseRouter) PrintRoutes() {
	for _, rt := range br.root.routes {
		fmt.Printf("%s %s (%s)\n", rt.Method, rt.Path, rt.Name)
		if rt.Operation.Description != "" {
			fmt.Printf("  Description: %s\n", rt.Operation.Description)
		}
		if len(rt.Operation.Tags) > 0 {
			fmt.Printf("  Tags: %v\n", rt.Operation.Tags)
		}
		if len(rt.Operation.Responses) > 0 {
			fmt.Printf("  Responses: %v\n", rt.Operation.Responses)
		}
		for i, h := range rt.Handlers {
			fmt.Printf("  %02d: %s\n", i, h.Name)
		}
		fmt.Println()
	}
}

func (br *baseRouter) addRoute(method HTTPMethod, fullPath string, finalHandler HandlerFunc, routeName string, allMw []namedMiddleware) *RouteDefinition {
	chain := chainHandlers(finalHandler, routeName, allMw)
	r := &RouteDefinition{
		Method:    method,
		Path:      fullPath,
		name:      routeName,
		Handlers:  chain,
		Operation: Operation{
			// Initially empty, populated via RouteInfo methods
		},
	}
	br.root.routes = append(br.root.routes, r)
	return r
}

func (br *baseRouter) Routes() []RouteDefinition {
	defs := make([]RouteDefinition, len(br.root.routes))
	for i, rt := range br.root.routes {
		defs[i] = *rt
	}
	return defs
}
