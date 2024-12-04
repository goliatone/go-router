package router

import (
	"errors"
	"fmt"
)

type RouteInfo interface {
	Name(string) RouteInfo
}

type RouteBuilder[T any] struct {
	router Router[T]
	routes []*Route[T]
}

type Route[T any] struct {
	builder     *RouteBuilder[T]
	path        string
	method      HTTPMethod
	handler     HandlerFunc
	middleware  []HandlerFunc
	name        string
	description string
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
		middleware: make([]HandlerFunc, 0),
	}
	b.routes = append(b.routes, route)
	return route
}

// Group creates a new RouteBuilder with a prefix path
func (b *RouteBuilder[T]) Group(prefix string) *RouteBuilder[T] {
	return NewRouteBuilder(b.router.Group(prefix))
}

func (r *RouteBuilder[T]) BuildAll() error {
	var errs error
	for _, route := range r.routes {
		if err := route.Build(); err != nil {
			errs = errors.Join(fmt.Errorf("failed to build route %s: %w", route.path, err))
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

func (r *Route[T]) Middleware(middleware ...HandlerFunc) *Route[T] {
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

func (r *Route[T]) Build() error {
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
