package router

import (
	"context"
)

// HTTPMethod represents HTTP request methods
type HTTPMethod string

type HandlerFunc func(Context) error

const (
	GET    HTTPMethod = "GET"
	POST   HTTPMethod = "POST"
	PUT    HTTPMethod = "PUT"
	DELETE HTTPMethod = "DELETE"
	PATCH  HTTPMethod = "PATCH"
)

// Context represents a generic HTTP context
type Context interface {
	// Request data
	Method() string
	Path() string
	Param(name string) string
	Query(name string) string
	Queries() map[string]string

	// Response methods
	Status(code int) Context
	Send(body []byte) error
	JSON(code int, v interface{}) error
	NoContent(code int) error

	// Body parsing
	Bind(interface{}) error

	// Context methods
	Context() context.Context
	SetContext(context.Context)

	Header(string) string
	SetHeader(string, string)

	Next() error
}

type Server[T any] interface {
	Router() Router[T]
	WrapHandler(HandlerFunc) interface{}
	WrappedRouter() T
	Serve(address string) error
	Shutdown(ctx context.Context) error
}

// Router represents a generic router interface
type Router[T any] interface {
	Handle(method HTTPMethod, path string, handler ...HandlerFunc) RouteInfo
	Group(prefix string) Router[T]
	Use(args ...any) Router[T]
	Get(path string, handler HandlerFunc) RouteInfo
	Post(path string, handler HandlerFunc) RouteInfo
	Put(path string, handler HandlerFunc) RouteInfo
	Delete(path string, handler HandlerFunc) RouteInfo
	Patch(path string, handler HandlerFunc) RouteInfo
}

type RouteInfo interface {
	Name(string) RouteInfo
}
