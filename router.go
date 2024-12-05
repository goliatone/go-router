package router

import (
	"context"
)

const HeaderAuthorization = "Authorization"

// HTTPMethod represents HTTP request methods
type HTTPMethod string

const (
	GET    HTTPMethod = "GET"
	POST   HTTPMethod = "POST"
	PUT    HTTPMethod = "PUT"
	DELETE HTTPMethod = "DELETE"
	PATCH  HTTPMethod = "PATCH"
)

type HandlerFunc func(Context) error
type MiddlewareFunc func(HandlerFunc) HandlerFunc
type ErrorHandler func(Context, error) error

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
	NoContent(code int) error
	Header(string) string
	SetHeader(string, string) ResponseWriter
}

// ContextStore is a request scoped, in-memoroy
// store to pass data between middleware/handlers
// in the same request in a fremework agnostic
// way.
// If you need persistence between requests use
// Store e.g. for authentication middleware.
type ContextStore interface {
	Set(key string, value any)
	Get(key string) any
	GetString(key string, def string) string
	GetInt(key string, def int) int
	GetBool(key string, def bool) bool
}

// Context represents a generic HTTP context
type Context interface {
	RequestContext
	ResponseWriter
	ContextStore

	// Body parsing //TODO: maybe rename to BodyParse?
	Bind(any) error

	// Context methods
	Context() context.Context
	SetContext(context.Context)
	Next() error

	IsAborted() bool
	Abort()
}

// Server represents a generic HTTP server that
// can be adapted to different HTTP routing frameworks.
// It manages the lifecycle of the HTTP server and
// provides access to the underlying router implementation.
type Server[T any] interface {
	// Router returns the router instance for registering routes and middleware
	Router() Router[T]
	// WrapHandler converts a HandlerFunc to the framework-specific handler type
	WrapHandler(HandlerFunc) any
	WrappedRouter() T
	Serve(address string) error
	Shutdown(ctx context.Context) error
	SetErrorHandler(handler ErrorHandler)
}

type RouteInfo interface {
	Name(string) RouteInfo
}

// Router represents a generic router interface
type Router[T any] interface {
	Handle(method HTTPMethod, path string, handler ...HandlerFunc) RouteInfo
	Group(prefix string) Router[T]
	Use(middleware ...MiddlewareFunc) Router[T]
	UseWithPriority(priority int, middleware ...MiddlewareFunc) Router[T]
	Get(path string, handler HandlerFunc) RouteInfo
	Post(path string, handler HandlerFunc) RouteInfo
	Put(path string, handler HandlerFunc) RouteInfo
	Delete(path string, handler HandlerFunc) RouteInfo
	Patch(path string, handler HandlerFunc) RouteInfo
}

// WrapHandler function to wrap handlers that return error
func WrapHandler(handler func(Context) error) HandlerFunc {
	return HandlerFunc(handler)
}

// ToMiddleware function to wrap handlers and run them as a middleware
func ToMiddleware(h HandlerFunc) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			if err := h(c); err != nil {
				return err
			}
			return next(c)
		}
	}
}
