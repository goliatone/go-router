package router

import (
	"errors"
	"fmt"
)

type RouteBuilder[T any] struct {
	router Router[T]
	routes []*Route[T]
}

type Route[T any] struct {
	builder     *RouteBuilder[T]
	path        string
	method      HTTPMethod
	handler     HandlerFunc
	middleware  []MiddlewareFunc
	name        string
	description string
	tags        []string
	responses   map[int]string
}

func NewRouteBuilder[T any](router Router[T]) *RouteBuilder[T] {
	return &RouteBuilder[T]{
		router: router,
		routes: make([]*Route[T], 0),
	}
}

// NewRoute starts the configuration of a new route
func (b *RouteBuilder[T]) NewRoute() *Route[T] {
	route := &Route[T]{
		builder:    b,
		middleware: make([]MiddlewareFunc, 0),
		responses:  make(map[int]string),
	}
	b.routes = append(b.routes, route)
	return route
}

// Group creates a new RouteBuilder with a prefix path
func (b *RouteBuilder[T]) Group(prefix string) *RouteBuilder[T] {
	return NewRouteBuilder(b.router.Group(prefix))
}

func (b *RouteBuilder[T]) BuildAll() error {
	var errs error
	for _, route := range b.routes {
		if err := route.Build(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("failed to build route %s: %w", route.path, err))
		}
	}
	return errs
}

func (r *Route[T]) Method(method HTTPMethod) *Route[T] {
	r.method = method
	return r
}

func (r *Route[T]) Path(path string) *Route[T] {
	r.path = path
	return r
}

func (r *Route[T]) Handler(handler HandlerFunc) *Route[T] {
	r.handler = handler
	return r
}

func (r *Route[T]) Middleware(middleware ...MiddlewareFunc) *Route[T] {
	r.middleware = append(r.middleware, middleware...)
	return r
}

func (r *Route[T]) Name(name string) *Route[T] {
	r.name = name
	return r
}

func (r *Route[T]) Description(description string) *Route[T] {
	r.description = description
	return r
}

func (r *Route[T]) Tags(tags ...string) *Route[T] {
	r.tags = append(r.tags, tags...)
	return r
}

func (r *Route[T]) Responses(responses map[int]string) *Route[T] {
	if r.responses == nil {
		r.responses = make(map[int]string)
	}
	for k, v := range responses {
		r.responses[k] = v
	}
	return r
}

func (r *Route[T]) Build() error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("route validation failed: %w", err)
	}

	// Compose middleware chain: wrap handler with each middleware in reverse order
	finalHandler := r.handler
	for i := len(r.middleware) - 1; i >= 0; i-- {
		finalHandler = r.middleware[i](finalHandler)
	}

	// Register the route and get the RouteInfo back
	ri := r.builder.router.Handle(r.method, r.path, finalHandler)

	// Apply metadata to RouteInfo
	if r.name != "" {
		ri = ri.Name(r.name)
	}
	if r.description != "" {
		ri = ri.Description(r.description)
	}
	if len(r.tags) > 0 {
		ri = ri.Tags(r.tags...)
	}
	if len(r.responses) > 0 {
		ri = ri.Responses(r.responses)
	}

	return nil
}

func (r *Route[T]) validate() error {
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

// Helper methods for common HTTP methods
func (r *Route[T]) GET() *Route[T] {
	return r.Method(GET)
}
func (r *Route[T]) POST() *Route[T] {
	return r.Method(POST)
}
func (r *Route[T]) PUT() *Route[T] {
	return r.Method(PUT)
}
func (r *Route[T]) DELETE() *Route[T] {
	return r.Method(DELETE)
}
func (r *Route[T]) PATCH() *Route[T] {
	return r.Method(PATCH)
}
