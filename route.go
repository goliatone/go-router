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
	responses   []Response
	parameters  []Parameter
	requestBody *RequestBody
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
		responses:  make([]Response, 0),
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

func (r *Route[T]) Responses(responses []Response) *Route[T] {
	r.responses = append(r.responses, responses...)
	return r
}

func (r *Route[T]) Parameter(name, in string, required bool, schema any) *Route[T] {
	r.parameters = append(r.parameters, Parameter{
		Name:     name,
		In:       in,
		Required: required,
		Schema:   schema,
	})
	return r
}

func (r *Route[T]) RequestBody(desc string, required bool, content map[string]any) *Route[T] {
	r.requestBody = &RequestBody{
		Description: desc,
		Required:    required,
		Content:     content,
	}
	return r
}

func (r *Route[T]) Response(code int, desc string, content map[string]any) *Route[T] {
	r.responses = append(r.responses, Response{
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

	ri := r.builder.router.Handle(r.method, r.path, finalHandler)

	if r.name != "" {
		ri = ri.Name(r.name)
	}

	if r.description != "" {
		ri = ri.Description(r.description)
	}

	if len(r.tags) > 0 {
		ri = ri.Tags(r.tags...)
	}

	for _, p := range r.parameters {
		ri = ri.AddParameter(p.Name, p.In, p.Required, p.Schema)
	}

	if r.requestBody != nil {
		ri = ri.SetRequestBody(r.requestBody.Description, r.requestBody.Required, r.requestBody.Content)
	}

	for _, resp := range r.responses {
		ri = ri.AddResponse(resp.Code, resp.Description, resp.Content)
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
