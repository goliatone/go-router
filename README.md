# go-router

A lightweight, generic HTTP router interface for Go that enables framework agnostic HTTP handling with built in adapters. This package provides an abstraction for routing, making it easy to switch between different HTTP router implementations.

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

## Middleware

`go-router` includes several built in middleware components that provide common functionality for HTTP request processing.

### Request ID Middleware

Generates and manages unique request identifiers for tracing and logging purposes.

```go
import "github.com/goliatone/go-router/middleware/requestid"

// Use default configuration
app.Use(requestid.New())

// Custom configuration
app.Use(requestid.New(requestid.Config{
    Header:     "X-Custom-Request-ID",
    Generator:  func() string { return "custom-" + uuid.NewString() },
    ContextKey: "req_id",
}))
```

**Features:**
- Generates UUID based request IDs by default
- Configurable header name (default: `X-Request-ID`)
- Custom ID generator function support
- Stores ID in context for handler access
- Skippable with custom skip function

### Route Context Middleware

Injects route information into the request context for template rendering and handler access.

```go
import "github.com/goliatone/go-router/middleware/routecontext"

// Using default configuration (ExportAsMap: true)
app.Use(routecontext.New())

// Custom configuration with consolidated map export
app.Use(routecontext.New(routecontext.Config{
    ExportAsMap:         true,  // Default: exports as single map
    TemplateContextKey:  "route_info",
    CurrentRouteNameKey: "route_name",
    CurrentParamsKey:    "params",
    CurrentQueryKey:     "query",
}))

// Configuration with individual key exports
app.Use(routecontext.New(routecontext.Config{
    ExportAsMap:         false, // Exports each key individually
    CurrentRouteNameKey: "route_name",
    CurrentParamsKey:    "params",
    CurrentQueryKey:     "query",
}))
```

**Features:**
- Extracts current route name, parameters, and query strings
- **Two export modes** controlled by `ExportAsMap` field:
  - **Map mode** (`ExportAsMap: true`, default): Stores data in a consolidated map under `TemplateContextKey`
  - **Individual mode** (`ExportAsMap: false`): Stores each key separately in context
- Default storage under `"template_context"` key in map mode
- Perfect for template rendering with route aware content
- Flexible access patterns for different use cases

**Template Usage (Map Mode - Default):**
```html
<!-- Access route information via consolidated map -->
{{ template_context.current_route_name }}
{{ template_context.current_params.user_id }}
{{ template_context.current_query.page }}
```

**Template Usage (Individual Mode):**
```html
<!-- Access route information via individual keys -->
{{ route_name }}
{{ params.user_id }}
{{ query.page }}
```

**Handler Access (Map Mode):**
```go
app.Get("/users/:id", func(c router.Context) error {
    routeData := c.Locals("template_context").(map[string]any)
    routeName := routeData["current_route_name"].(string)
    params := routeData["current_params"].(map[string]string)
    return c.JSON(200, fiber.Map{"route": routeName, "params": params})
})
```

**Handler Access (Individual Mode):**
```go
app.Get("/users/:id", func(c router.Context) error {
    routeName := c.Locals("route_name").(string)
    params := c.Locals("params").(map[string]string)
    query := c.Locals("query").(map[string]string)
    return c.JSON(200, fiber.Map{"route": routeName, "params": params, "query": query})
})
```

### Flash Middleware

Provides flash message functionality for displaying temporary messages across redirects using cookie based storage.

```go
import "github.com/goliatone/go-router/middleware/flash"

// Use default configuration
app.Use(flash.New())

// Custom configuration
app.Use(flash.New(flash.Config{
    ContextKey: "flash_data",
    Flash:      customFlashInstance,
}))
```

**Features:**
- Cookie based flash message storage that survives redirects
- Automatic injection of flash data into request context
- Integration with existing flash utility for setting messages
- Configurable context key for accessing flash data
- Skippable with custom skip function

**Handler Usage:**
```go
// Access flash data in handlers
app.Get("/", func(c router.Context) error {
    flashData := c.Locals("flash").(router.ViewContext)
    return c.Render("index", flashData)
})
```

**Template Usage:**
```html
{% if flash.error %}
<div class="p-4 mb-4 text-sm text-red-800 rounded-lg bg-red-50 border border-red-400" role="alert">
    <span class="font-medium">Authentication Failed!</span>
    {% if flash.error_message %}
    <p>{{ flash.error_message }}</p>
    {% else %}
    Please check your credentials and try again.
    {% endif %}
</div>
{% endif %}
```

## View Engine

### View Engine Initialization

`go-router` includes a powerful and flexible view engine initializer that works seamlessly with frameworks that support `fiber.Views` (like Fiber itself). It's built on `pongo2`, offering a Django like template syntax, and handles both live reloading for development and high performance embedded assets for production.

The core function is `router.InitializeViewEngine(config)`. It takes a configuration object that implements the `ViewConfigProvider` interface.

#### Key Features

*   **Embedded & Live Modes**: Use `go:embed` for single binary production builds, or load templates directly from the OS for rapid development.
*   **Composite Filesystems**: In embedded mode, you can layer multiple filesystems. This is perfect for theme overriding, where a theme's templates can transparently override a base set of templates.
*   **Automatic Asset Handling**: Template functions `css(glob)` and `js(glob)` automatically find your versioned assets (e.g., `main-*.css`) and generate the correct HTML tags.
*   **Intelligent Pathing**: The engine automatically handles subdirectories in embedded filesystems, so your paths are always clean and relative to your declared asset/template roots.

#### Configuration (`ViewConfigProvider` Interface)

Your configuration struct must provide getters for the following options:

| Option | Type | Description |
| :--- | :--- | :--- |
| `GetEmbed()` | `bool` | **(Required)** `true` to use embedded `fs.FS` filesystems, `false` to load from the operating system. |
| `GetDebug()` | `bool` | Enables debug logging from the template engine. |
| `GetReload()` | `bool` | If `true`, templates are reloaded on every request. Ideal for development. |
| `GetExt()` | `string` | The file extension for your templates (e.g., `.html`, `.p2`). |
| `GetTemplateFunctions()` | `map[string]any` | A map of custom functions to register with the template engine. |
| **Embedded Mode Options** | | |
| `GetTemplatesFS()` | `[]fs.FS` | **(Required)** A slice of `fs.FS` filesystems containing your templates. Order matters for overrides (first found wins). |
| `GetAssetsFS()` | `fs.FS` | **(Required)** The `fs.FS` filesystem containing your static assets (CSS, JS). |
| `GetDirFS()` | `string` | The path *inside* `GetTemplatesFS` to the root of your templates (e.g., "templates"). |
| `GetAssetsDir()` | `string` | The path *inside* `GetAssetsFS` to the root of your assets (e.g., "public"). |
| `GetDevDir()` | `string` | An absolute OS path to a directory of override templates. These take highest priority, perfect for local development without changing embedded files. |
| **Live (Non-Embedded) Mode Options** | | |
| `GetDirOS()` | `string` | **(Required)** The absolute or relative OS path to your templates directory. |
| `GetAssetsDir()` | `string` | **(Required)** The absolute or relative OS path to your assets directory. |
| **Asset URL Generation** | | |
| `GetCSSPath()` | `string` | The subdirectory within your `AssetsDir` where CSS files are located (e.g., "css"). |
| `GetJSPath()` | `string` | The subdirectory within your `AssetsDir` where JS files are located (e.g., "js"). |
| `GetURLPrefix()` | `string` | An optional prefix to add to all generated asset URLs. Useful for serving assets from a namespaced path like `/static`. |

---

#### Example 1: Embedded Mode for Production

This setup is ideal for creating a self contained, single binary application.

**Directory Structure:**
```
.
├── assets/
│   ├── css/main-a1b2c3d4.css
│   └── js/app-e5f6g7h8.js
├── templates/
│   ├── layouts/base.html
│   └── index.html
└── main.go
```

**main.go:**
```go
package main

import (
	"embed"
	"io/fs"
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
)

//go:embed templates
var templateFS embed.FS

//go:embed assets
var assetFS embed.FS

type AppConfig struct {
    // app settings
}
func (c *AppConfig) GetEmbed() bool         { return true }
func (c *AppConfig) GetDebug() bool         { return true }
func (c *AppConfig) GetReload() bool        { return false } // a
func (c *AppConfig) GetExt() string         { return ".html" }
func (c *AppConfig) GetDirFS() string       { return "templates" } // root of templates in templateFS
func (c *AppConfig) GetAssetsDir() string   { return "assets" } // Root of assets in assetFS
func (c *AppConfig) GetCSSPath() string     { return "css" }
func (c *AppConfig) GetJSPath() string      { return "js" }
func (c *AppConfig) GetURLPrefix() string   { return "static" } // URL will be /static/css/...
func (c *AppConfig) GetDevDir() string      { return "" } // no dev overrides
func (c *AppConfig) GetTemplatesFS() []fs.FS { return []fs.FS{templateFS} }
func (c *AppConfig) GetAssetsFS() fs.FS     { return assetFS }
// live mode options are ignored when embed is true
func (c *AppConfig) GetDirOS() string { return "" }
func (c *AppConfig) GetTemplateFunctions() map[string]any { return nil }

func main() {
    config := &AppConfig{}

    viewEngine, err := router.InitializeViewEngine(config)
    if err != nil {
        log.Fatalf("Failed to init view engine: %v", err)
    }

    app := fiber.New(fiber.Config{
        Views: viewEngine,
    })

    // IMPORTANT: Serve your static files with the same prefix!
    // The router only generates URLs; it doesn't serve files.
    // fs.Sub is used to serve the `assets` directory content.
    staticFS, _ := fs.Sub(assetFS, "assets")
    app.Static("/static", filesystem.New(filesystem.Config{
        Root: http.FS(staticFS),
    }))

    app.Get("/", func(c *fiber.Ctx) error {
        return c.Render("index", fiber.Map{"Title": "Welcome"})
    })

    log.Fatal(app.Listen(":3000"))
}
```

**templates/layouts/base.html:**
```html
<!DOCTYPE html>
<html>
<head>
    <title>{{ Title }}</title>
    {{ css("main-*.css") | safe }}
</head>
<body>
    {% block content %}{% endblock %}
    {{ js("app-*.js") | safe }}
</body>
</html>
```
This will render `<link href="/static/css/main-a1b2c3d4.css">` in the final HTML.

---

#### Example 2: Live Mode for Development

This setup loads files directly from your disk, and `Reload: true` ensures you see template changes without restarting the server.

**main.go:**
```go
type DevConfig struct {
    // ...
}
func (c *DevConfig) GetEmbed() bool       { return false }
func (c *DevConfig) GetReload() bool      { return true }
func (c *DevConfig) GetDirOS() string     { return "./templates" } // path on disk
func (c *DevConfig) GetAssetsDir() string { return "./assets" }    // path on your disk
func (c *DevConfig) GetURLPrefix() string { return "" } // we serve from root URL /
// other getters
```

**Server Setup:**
```go
func main() {
    config := &DevConfig{}

    viewEngine, err := router.InitializeViewEngine(config)
    // define error handling, etc...

    app := fiber.New(fiber.Config{
        Views: viewEngine,
    })

    // serve assets directly from the filesystem path
    app.Static("/", "./assets")

    // define your routes...
    log.Fatal(app.Listen(":3000"))
}
```
This will render `<link href="/css/main-a1b2c3d4.css">` in the final HTML, which is served by `app.Static`.

### Pro-Tip: Generating Configuration

Manually implementing the `ViewConfigProvider` interface for every project can be repetitive. You can accelerate this process by defining your configuration in a JSON or YAML file and using the **[go-generators](https://github.com/goliatone/go-generators?tab=readme-ov-file#app-config)** tool to automatically generate the required Go struct and methods.

**1. Define your configuration in `config/app.json`:**
```json
{
  "views": {
    "embed": true,
    "debug": true,
    "reload": false,
    "ext": ".html",
    "dir_fs": "views",
    "assets_dir": "public",
    "css_path": "css",
    "js_path": "js",
    "url_prefix": "static"
  }
}
```

**2. Run the generator:**
The generator will parse your JSON file, create a corresponding Go struct (`ViewsConfig`), and automatically implement all the necessary methods of the `ViewConfigProvider` interface.

```bash
go run github.com/goliatone/go-generators/cmd/config-gen --source ./config/app.json --key views --type ViewsConfig
```

**3. Use the generated config:**
Now you can simply load your configuration from the file and pass it directly to the view engine initializer.

```go
package main

import (
    "path/to/your/generated/config"
    "github.com/goliatone/go-router"
    // ...
)

func main() {
    // load config from file
    appConfig, err := config.Load()
    if err != nil {
        // handle error
    }

    // the generated appConfig.Views field already implements ViewConfigProvider
    viewEngine, err := router.InitializeViewEngine(appConfig.Views)
    if err != nil {
        log.Fatalf("Failed to init view engine: %v", err)
    }

    //... setup Fiber app
}
```

This approach keeps your configuration clean and separate from your application logic while eliminating boilerplate code.

## WebSocket Support

`go-router` provides comprehensive WebSocket support with an event driven architecture, room management, and extensive middleware capabilities. The WebSocket implementation works seamlessly with existing HTTP routers and provides both simple and advanced usage patterns.

### Quick Start

```go
// Simple WebSocket handler
app.Router().Get("/ws", router.NewWSHandler(func(ctx context.Context, client router.WSClient) error {
    // Handle messages
    client.OnMessage(func(ctx context.Context, data []byte) error {
        fmt.Printf("Received: %s\n", data)
        return client.Send([]byte("Echo: " + string(data)))
    })

    // Wait for disconnection
    <-ctx.Done()
    return nil
}))
```

### Features

- **Event-driven architecture** with connect/disconnect/message handlers
- **Room management** with join/leave and targeted broadcasting
- **Middleware system** including authentication, logging, metrics, rate limiting, and panic recovery
- **Context support** throughout the API for cancellation and request scoped data
- **JSON message handling** with structured event routing
- **Client state management** with get/set operations
- **Automatic connection lifecycle** management with ping/pong handling

For complete WebSocket documentation, examples, and advanced features, see [README_WEBSOCKET.md](README_WEBSOCKET.md).

## Error Handling Policy

This project follows a consistent error handling strategy to ensure reliability and maintainability across the WebSocket and HTTP components.

### Error Classification

#### 1. Recoverable Errors (Return to Caller)
These errors should be returned to the calling function to allow for graceful handling and recovery:

- **Validation errors**: Invalid input parameters, malformed data
- **Authentication/Authorization failures**: Token validation, permission checks
- **Configuration errors**: Missing required configuration, invalid settings
- **External service failures**: Database connection errors, API call failures
- **Resource exhaustion**: Rate limiting violations, connection limits reached

**Pattern:**
```go
func ProcessRequest(data []byte) error {
    if len(data) == 0 {
        return fmt.Errorf("empty data provided")
    }
    // ... processing logic
    return nil
}
```

#### 2. System Errors (Log Centrally)
These errors represent system level issues that should be logged centrally for monitoring and debugging:

- **Internal server errors**: Unexpected panics, nil pointer dereferences
- **Infrastructure failures**: Network timeouts, disk I/O errors
- **Background operation failures**: Cleanup operations, periodic tasks
- **WebSocket connection errors**: Unexpected disconnections, protocol violations
- **Hub/Room management errors**: Client registration failures, broadcast errors

**Pattern:**
```go
func (h *WSHub) broadcastToAll(message []byte) {
    h.clientsMu.RLock()
    defer h.clientsMu.RUnlock()

    for client := range h.clients {
        if err := client.WriteMessage(message); err != nil {
            h.logger.Error("Failed to send message to client",
                "client_id", client.ID(),
                "error", err)
            // Continue with other clients - don't return error
        }
    }
}
```

### Logger Requirements

All background components and long running operations must have access to a structured logger:

- **WSHub**: For client management and broadcasting errors
- **Room**: For room specific operation failures
- **WSClient**: For connection specific errors
- **Middleware**: For request processing errors

### Error Context

When logging errors, always include relevant context:

```go
logger.Error("Operation failed",
    "operation", "client_registration",
    "client_id", client.ID(),
    "room_id", roomID,
    "error", err)
```

### Testing Error Paths

All error handling paths should be testable:

- Use dependency injection for external services
- Provide mock implementations for testing
- Include error scenarios in unit tests
- Validate error messages and context

## API Reference

### Server Interface

```go
type Server[T any] interface {
    Router() Router[T]
    WrapHandler(HandlerFunc) any
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
    JSON(code int, v any) error
    NoContent(code int) error
    Bind(any) error
    Context() context.Context
    SetContext(context.Context)
    Header(string) string
    SetHeader(string, string)
    Next() error
}
```

## License

MIT
