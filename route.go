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
	builder    *RouteBuilder[T]
	middleware []MiddlewareFunc
	handler    HandlerFunc
	definition *RouteDefinition
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
		definition: &RouteDefinition{
			Operation: Operation{
				Parameters: make([]Parameter, 0),
				Responses:  make([]Response, 0),
				Tags:       make([]string, 0),
			},
		},
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
			errs = errors.Join(errs, fmt.Errorf("failed to build route %s: %w", route.definition.Path, err))
		}
	}
	return errs
}

func (r *Route[T]) Method(method HTTPMethod) *Route[T] {
	r.definition.Method = method
	return r
}

func (r *Route[T]) Path(path string) *Route[T] {
	// TODO: Sanitize path, e.g.
	if path == "" {
		path = "/"
	}
	r.definition.Path = path
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
	r.definition.name = name
	return r
}

func (r *Route[T]) Description(description string) *Route[T] {
	r.definition.Operation.Description = description
	return r
}

func (r *Route[T]) Summary(summary string) *Route[T] {
	r.definition.Operation.Summary = summary
	return r
}

func (r *Route[T]) Tags(tags ...string) *Route[T] {
	r.definition.Operation.Tags = append(r.definition.Operation.Tags, tags...)
	return r
}

func (r *Route[T]) Responses(responses []Response) *Route[T] {
	r.definition.Operation.Responses = append(r.definition.Operation.Responses, responses...)
	return r
}

func (r *Route[T]) Parameter(name, in string, required bool, schema any) *Route[T] {
	r.definition.Operation.Parameters = append(r.definition.Operation.Parameters, Parameter{
		Name:     name,
		In:       in,
		Required: required,
		Schema:   schema,
	})
	return r
}

func (r *Route[T]) RequestBody(desc string, required bool, content map[string]any) *Route[T] {
	r.definition.Operation.RequestBody = &RequestBody{
		Description: desc,
		Required:    required,
		Content:     content,
	}
	return r
}

func (r *Route[T]) Response(code int, desc string, content map[string]any) *Route[T] {
	r.definition.Operation.Responses = append(r.definition.Operation.Responses, Response{
		Code:        code,
		Description: desc,
		Content:     content,
	})
	return r
}

func (r *Route[T]) Build() error {
	if err := r.validate(); err != nil {
		return fmt.Errorf("route validation failed: %w", err)
	}

	finalHandler := r.handler
	for i := len(r.middleware) - 1; i >= 0; i-- {
		finalHandler = r.middleware[i](finalHandler)
	}

	var handlers []NamedHandler
	for _, mw := range r.middleware {
		handlers = append(handlers, NamedHandler{
			Name:    funcName(mw),
			Handler: mw(finalHandler),
		})
	}
	handlers = append(handlers, NamedHandler{
		Name:    r.definition.name,
		Handler: finalHandler,
	})
	r.definition.Handlers = handlers

	ri := r.builder.router.Handle(r.definition.Method, r.definition.Path, finalHandler)

	ri.FromRouteDefinition(r.definition)

	return nil
}

func (r *Route[T]) validate() error {
	if r.definition.Method == "" {
		return NewValidationError("method is required", nil)
	}

	if r.definition.Path == "" {
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
