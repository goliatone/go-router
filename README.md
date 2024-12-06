# go-router

A lightweight, generic HTTP router interface for Go that enables framework-agnostic HTTP handling with built-in adapters. This package provides an abstraction for routing, making it easy to switch between different HTTP router implementations.

## Installation

```bash
go get github.com/goliatone/go-router
```

## Overview

`go-router` provides a common interface for HTTP routing that can be implemented by different HTTP frameworks. Currently includes a [Fiber](https://github.com/gofiber/fiber) and [HTTPRouter](https://github.com/julienschmidt/httprouter)  with plans to support more frameworks.

## Usage

### Basic Example with Fiber

```go
package main

import (
    "github.com/goliatone/go-router"
    "github.com/gofiber/fiber/v2"
)

func main() {
    // Create new Fiber adapter
    app := router.NewFiberAdapter()

    // Add middleware
    app.Router().Use(func(c router.Context) error {
        c.SetHeader("Content-Type", "application/json")
        return c.Next()
    })

    // Add routes
    app.Router().Get("/hello", func(c router.Context) error {
        return c.JSON(200, map[string]string{"message": "Hello World!"})
    })

    // Start server
    app.Serve(":3000")
}
```

### Route Groups

```go
api := app.Router().Group("/api")
{
    api.Post("/users", createUser(store)).Name("user.create")
    api.Get("/users", listUsers(store)).Name("user.list")
    api.Get("/users/:id", getUser(store)).Name("user.get")
    api.Put("/users/:id", updateUser(store)).Name("user.update")
    api.Delete("/users/:id", deleteUser(store)).Name("user.delete")
}
```

### Builder

```go
api := app.Router().Group("/api")

builder := router.NewRouteBuilder(api)

users := builder.Group("/users")
{
    users.NewRoute().
        POST().
        Path("/").
        Description("Create a new user").
        Tags("User").
        Handler(createUser(store)).
        Name("user.create")

    users.NewRoute().
        GET().
        Path("/").
        Description("List all users").
        Tags("User").
        Handler(listUsers(store)).
        Name("user.list")

    users.NewRoute().
        GET().
        Path("/:id").
        Description("Get user by ID").
        Tags("User").
        Handler(getUser(store)).
        Name("user.get")

    users.NewRoute().
        PUT().
        Path("/:id").
        Description("Update user by ID").
        Tags("User").
        Handler(updateUser(store)).
        Name("user.update")

    users.NewRoute().
        DELETE().
        Path("/:id").
        Description("Delete user by ID").
        Tags("User").
        Handler(deleteUser(store)).
        Name("user.delete")

    users.BuildAll()
}
```

## API Reference

### Server Interface

```go
type Server[T any] interface {
    Router() Router[T]
    WrapHandler(HandlerFunc) interface{}
    WrappedRouter() T
    Serve(address string) error
    Shutdown(ctx context.Context) error
}
```

### Router Interface

```go
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
```

### Context Interface

```go
type Context interface {
    Method() string
    Path() string
    Param(name string) string
    Query(name string) string
    Queries() map[string]string
    Status(code int) Context
    Send(body []byte) error
    JSON(code int, v interface{}) error
    NoContent(code int) error
    Bind(interface{}) error
    Context() context.Context
    SetContext(context.Context)
    Header(string) string
    SetHeader(string, string)
    Next() error
}
```

## License

MIT
