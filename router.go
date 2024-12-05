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

type Logger interface {
	Debug(format string, args ...any)
	Info(format string, args ...any)
	Error(format string, args ...any)
}

type RequestContext interface {
	Method() string
	Path() string
	Param(name string) string
	Query(name string) string
	Queries() map[string]string
}

type ResponseWriter interface {
	Status(code int) ResponseWriter
	Send(body []byte) error
	JSON(code int, v any) error
	// NoContent for status codes that shouldn't have response bodies (204, 205, 304).
	NoContent(code int) error
	Header(string) string
	SetHeader(string, string) ResponseWriter
}

// Context represents a generic HTTP context
type Context interface {
	RequestContext
	ResponseWriter

	// Body parsing
	Bind(any) error

	// Context methods
	Context() context.Context
	SetContext(context.Context)

	Next() error
}

type Server[T any] interface {
	Router() Router[T]
	WrapHandler(HandlerFunc) any
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
