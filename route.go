package router

import "fmt"

type RouteInfo interface {
	Name(string) RouteInfo
}

type RouteBuilder struct {
	router Router[any]
	routes []*Route
}

type Route struct {
	builder     *RouteBuilder
	path        string
	method      HTTPMethod
	handler     HandlerFunc
	middleware  []HandlerFunc
	name        string
	description string
}

func NewRouteBuilder(router Router[any]) *RouteBuilder {
	return &RouteBuilder{
		router: router,
		routes: make([]*Route, 0),
	}
}

func (r *RouteBuilder) NewRoute() *Route {
	route := &Route{
		builder:    r,
		middleware: make([]HandlerFunc, 0),
	}
	r.routes = append(r.routes, route)
	return route
}

// Group creates a new RouteBuilder with a prefix path
func (b *RouteBuilder) Group(prefix string) *RouteBuilder {
	return NewRouteBuilder(b.router.Group(prefix))
}

func (r *RouteBuilder) BuildAll() error {
	for _, route := range r.routes {
		if err := route.Build(); err != nil {
			return fmt.Errorf("failed to build route %s: %w", route.path, err)
		}
	}
	return nil
}

func (r *Route) Method(method HTTPMethod) *Route {
	r.method = method
	return r
}

func (r *Route) Path(path string) *Route {
	r.path = path
	return r
}

func (r *Route) Handler(handler HandlerFunc) *Route {
	r.handler = handler
	return r
}

func (r *Route) Middleware(middleware ...HandlerFunc) *Route {
	r.middleware = append(r.middleware, middleware...)
	return r
}

func (r *Route) Name(name string) *Route {
	r.name = name
	return r
}

func (r *Route) Description(description string) *Route {
	r.description = description
	return r
}

func (r *Route) Build() error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("route validation failed: %w", err)
	}

	handlers := make([]HandlerFunc, 0, len(r.middleware)+1)
	handlers = append(handlers, r.middleware...)
	handlers = append(handlers, r.handler)

	route := r.builder.router.Handle(r.method, r.path, handlers...)
	if r.name != "" {
		route = route.Name(r.name)
	}

	return nil
}

func (r *Route) validate() error {
	if r.method == "" {
		return NewValidationError("method is required", nil)
	}

	if r.path == "" {
		return NewValidationError("path is required", nil)
	}

	if r.handler == nil {
		return NewValidationError("handler is required", nil)
	}

	return nil
}

func (r *Route) GET() *Route {
	return r.Method(GET)
}

func (r *Route) POST() *Route {
	return r.Method(POST)
}

func (r *Route) PUT() *Route {
	return r.Method(PUT)
}

func (r *Route) DELETE() *Route {
	return r.Method(DELETE)
}

func (r *Route) PATCH() *Route {
	return r.Method(PATCH)
}
