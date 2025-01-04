package router

import (
	"errors"
	"fmt"
)

type RouteBuilder[T any] struct {
	router   Router[T]
	routes   []*Route[T]
	parent   *RouteBuilder[T] // Parent builder reference
	children []*RouteBuilder[T]
	prefix   string // Path prefix for this group
}

type Route[T any] struct {
	builder    *RouteBuilder[T]
	middleware []MiddlewareFunc
	handler    HandlerFunc
	definition *RouteDefinition
}

func NewRouteBuilder[T any](router Router[T]) *RouteBuilder[T] {
	return &RouteBuilder[T]{
		router:   router,
		routes:   make([]*Route[T], 0),
		children: make([]*RouteBuilder[T], 0),
	}
}

// NewRoute starts the configuration of a new route
func (b *RouteBuilder[T]) NewRoute() *Route[T] {
	route := &Route[T]{
		builder:    b,
		middleware: make([]MiddlewareFunc, 0),
		definition: NewRouteDefinition(),
	}
	b.routes = append(b.routes, route)
	return route
}

// Group creates a new RouteBuilder with a prefix path
func (b *RouteBuilder[T]) Group(prefix string) *RouteBuilder[T] {
	child := &RouteBuilder[T]{
		router:   b.router.Group(prefix),
		routes:   make([]*Route[T], 0),
		parent:   b,
		children: make([]*RouteBuilder[T], 0),
		prefix:   prefix,
	}
	b.children = append(b.children, child)
	return child
}

func (b *RouteBuilder[T]) BuildAll() error {
	if b.parent != nil {
		return b.parent.BuildAll()
	}

	allRoutes := b.getAllRoutes()
	if len(allRoutes) == 0 {
		return errors.New("no routes to build")
	}

	var errs error
	for _, route := range allRoutes {
		if err := route.Build(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("failed to build route %s: %w", route.definition.Path, err))
		}
	}

	return errs
}

func (b *RouteBuilder[T]) getAllRoutes() []*Route[T] {
	var allRoutes []*Route[T]

	allRoutes = append(allRoutes, b.routes...)

	for _, child := range b.children {
		allRoutes = append(allRoutes, child.getAllRoutes()...)
	}

	return allRoutes
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
	fmt.Println("== adding route: " + name)
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

	if r.definition.name != "" {
		ri.Name(r.definition.name)
	}

	if r.definition.Operation.Description != "" {
		ri.Description(r.definition.Operation.Description)
	}

	if r.definition.Operation.Summary != "" {
		ri.Summary(r.definition.Operation.Summary)
	}

	if len(r.definition.Operation.Tags) > 0 {
		ri.Tags(r.definition.Operation.Tags...)
	}

	for _, p := range r.definition.Operation.Parameters {
		ri.AddParameter(p.Name, p.In, p.Required, p.Schema)
	}

	if r.definition.Operation.RequestBody != nil {
		ri.SetRequestBody(
			r.definition.Operation.RequestBody.Description,
			r.definition.Operation.RequestBody.Required,
			r.definition.Operation.RequestBody.Content,
		)
	}

	for _, resp := range r.definition.Operation.Responses {
		ri.AddResponse(resp.Code, resp.Description, resp.Content)
	}

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
