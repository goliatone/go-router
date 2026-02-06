package router

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

type RouteBuilder[T any] struct {
	router   Router[T]
	routes   []*Route[T]
	parent   *RouteBuilder[T] // Parent builder reference
	children []*RouteBuilder[T]
	prefix   string // Path prefix for this group
	meta     []RouteDefinition
}

type Route[T any] struct {
	builder    *RouteBuilder[T]
	middleware []MiddlewareFunc
	handler    HandlerFunc
	definition *RouteDefinition
}

func NewRouteBuilder[T any](router Router[T]) *RouteBuilder[T] {
	prefix := ""
	if pr, ok := router.(PrefixedRouter); ok {
		prefix = pr.GetPrefix()
	}
	return &RouteBuilder[T]{
		router:   router,
		routes:   make([]*Route[T], 0),
		children: make([]*RouteBuilder[T], 0),
		prefix:   prefix, // Set the initial prefix from router
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
	var meta []RouteDefinition

	for _, route := range allRoutes {
		if err := route.Build(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("failed to build route %s: %w", route.definition.Path, err))
			continue
		}

		routeMeta := RouteDefinition{
			Method:      route.definition.Method,
			Path:        getFullPath(route),
			Name:        route.definition.Name,
			Summary:     route.definition.Summary,
			Description: route.definition.Description,
			Tags:        route.definition.Tags,
			Parameters:  route.definition.Parameters,
			RequestBody: route.definition.RequestBody,
			Responses:   route.definition.Responses,
			Handlers:    route.definition.Handlers,
		}

		meta = append(meta, routeMeta)
	}

	b.meta = meta

	return errs
}

func (b *RouteBuilder[T]) GetMetadata() []RouteDefinition {
	return b.meta
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
	r.definition.Name = name
	return r
}

func (r *Route[T]) Description(description string) *Route[T] {
	r.definition.Description = description
	return r
}

func (r *Route[T]) Summary(summary string) *Route[T] {
	r.definition.Summary = summary
	return r
}

func (r *Route[T]) Tags(tags ...string) *Route[T] {
	r.definition.Tags = append(r.definition.Tags, tags...)
	return r
}

func (r *Route[T]) Responses(responses []Response) *Route[T] {
	r.definition.Responses = append(r.definition.Responses, responses...)
	return r
}

func (r *Route[T]) Parameter(name, in string, required bool, schema map[string]any) *Route[T] {
	r.definition.Parameters = append(r.definition.Parameters, Parameter{
		Name:     name,
		In:       in,
		Required: required,
		Schema:   schema,
	})
	return r
}

func (r *Route[T]) RequestBody(desc string, required bool, content map[string]any) *Route[T] {
	r.definition.RequestBody = &RequestBody{
		Description: desc,
		Required:    required,
		Content:     content,
	}
	return r
}

func (r *Route[T]) Response(code int, desc string, content map[string]any) *Route[T] {
	r.definition.Responses = append(r.definition.Responses, Response{
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

	ri := r.builder.router.Handle(r.definition.Method, r.definition.Path, r.handler, r.middleware...)
	if routeDef, ok := ri.(*RouteDefinition); ok {
		r.definition.Handlers = routeDef.Handlers
	}

	if r.definition.Name != "" {
		ri.SetName(r.definition.Name)
	}

	if r.definition.Description != "" {
		ri.SetDescription(r.definition.Description)
	}

	if r.definition.Summary != "" {
		ri.SetSummary(r.definition.Summary)
	}

	if len(r.definition.Tags) > 0 {
		ri.AddTags(r.definition.Tags...)
	}

	for _, p := range r.definition.Parameters {
		ri.AddParameter(p.Name, p.In, p.Required, p.Schema)
	}

	if r.definition.RequestBody != nil {
		ri.SetRequestBody(
			r.definition.RequestBody.Description,
			r.definition.RequestBody.Required,
			r.definition.RequestBody.Content,
		)
	}

	for _, resp := range r.definition.Responses {
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

// Get the full path by walking up the builder hierarchy
func getFullPath[T any](route *Route[T]) string {
	fullPath := route.definition.Path
	currentBuilder := route.builder

	// Walk up the builder hierarchy
	var prefixes []string
	for currentBuilder != nil {
		if currentBuilder.prefix != "" {
			prefixes = append([]string{currentBuilder.prefix}, prefixes...)
		}
		currentBuilder = currentBuilder.parent
	}

	// Join all prefixes with the route path
	if len(prefixes) > 0 {
		prefix := path.Join(prefixes...)
		if fullPath == "" || fullPath == "/" {
			fullPath = prefix
		} else {
			fullPath = path.Join(prefix, fullPath)
		}
	}

	// Clean the path
	if !strings.HasPrefix(fullPath, "/") {
		fullPath = "/" + fullPath
	}
	return fullPath
}
