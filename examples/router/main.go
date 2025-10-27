package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/goliatone/go-errors"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/flash"
	flashmw "github.com/goliatone/go-router/middleware/flash"
	"github.com/goliatone/hashid/pkg/hashid"
	"github.com/julienschmidt/httprouter"
)

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UserStore struct {
	sync.RWMutex
	users map[string]User
}

func NewUserStore() *UserStore {
	email1 := "julie.smith@example.com"
	id1, _ := hashid.New(email1)

	email2 := "jose.bates@example.com"
	id2, _ := hashid.New(email2)

	email3 := "brad.miles@example.com"
	id3, _ := hashid.New(email3)

	return &UserStore{
		users: map[string]User{
			id1: {
				ID:        id1,
				Name:      "Julie Smith",
				Email:     email1,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			id2: {
				ID:        id2,
				Name:      "Jose Bates",
				Email:     email2,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			id3: {
				ID:        id3,
				Name:      "Brad Miles",
				Email:     email3,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}
}

func newFiberAdapter() router.Server[*fiber.App] {
	cfg := router.NewSimpleViewConfig("./views").
		WithAssets("./views", "css", "js").
		WithURLPrefix("static")

	viewEngine, err := router.InitializeViewEngine(cfg)
	if err != nil {
		log.Fatalf("failed to initialize view engine: %v", err)
	}

	app := router.NewFiberAdapter(func(a *fiber.App) *fiber.App {
		return fiber.New(
			fiber.Config{
				AppName:           "Go Router - Fiber",
				EnablePrintRoutes: true,
				PassLocalsToViews: true,
				Views:             viewEngine,
			},
		)
	})
	return app
}

func newHTTPServerAdapter() router.Server[*httprouter.Router] {
	return router.NewHTTPServer()
}

func healthRouteHandler(c router.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"success": true,
	})
}

func errorRouteHandler(c router.Context) error {
	return router.NewInternalError(
		fmt.Errorf("this is an error"), "error test",
	).WithMetadata(map[string]any{
		"version":  "v0.0.0",
		"hostname": "localhost",
	})
}

func createRoutes[T any](app router.Server[T], store *UserStore) {

	errMiddleware := router.WithErrorHandlerMiddleware(
		router.WithEnvironment("development"),
		router.WithStackTrace(true),
		router.WithErrorMapper(domainErrorMapper),
	)

	app.Router().Use(errMiddleware)

	var auth router.HandlerFunc = func(c router.Context) error {
		if pwd := c.Header(router.HeaderAuthorization); pwd == "password" {
			return c.Next()
		}
		return router.NewUnauthorizedError("unauthorized")
	}

	api := app.Router().Group("/api")
	api.Use(router.ToMiddleware(func(c router.Context) error {
		c.SetHeader(router.HeaderContentType, "application/json")
		return c.Next()
	}))

	builder := router.NewRouteBuilder(api)

	users := builder.Group("/users")
	{
		users.NewRoute().
			POST().
			Path("/").
			Summary("Create User").
			Description(`## Create User
This endpoint will create a new User, just for you
			`).
			Tags("User").
			Handler(createUser(store)).
			Name("user.create")

		users.NewRoute().
			GET().
			Path("/").
			Description("List all users").
			Summary("This is the summary").
			Tags("User").
			Responses([]router.Response{
				{
					Code:        200,
					Description: "Successful call a user",
					Content: map[string]any{
						"age": "age",
					},
				},
			}).
			Handler(listUsers(store)).
			Name("user.list")

		users.NewRoute().
			GET().
			Path("/:id").
			Summary("Get User By ID").
			Description("Get user by ID").
			Tags("User").
			Handler(getUser(store)).
			Name("user.get")

		users.NewRoute().
			PUT().
			Path("/:id").
			Summary("Update user by ID").
			Description("Update user by ID").
			Tags("User").
			Handler(updateUser(store)).
			Name("user.update")

		users.NewRoute().
			DELETE().
			Path("/:id").
			Summary("Delete user by ID").
			Description("Delete user by ID").
			Tags("User").
			Handler(deleteUser(store)).
			Name("user.delete")

		users.BuildAll()
	}

	private := api.Group("/secret")
	{
		private.Use(auth.AsMiddlware())
		private.Get("/:name", getSecret()).SetName("secrets.get")
	}

	builder.NewRoute().
		GET().
		Path("/health").
		Description("Health endpoint to get information about: health status").
		Tags("Health").
		Handler(healthRouteHandler).
		Name("health")

	builder.NewRoute().
		GET().
		Path("/errors").
		Description("Errors endpoint to get information about: errors").
		Tags("Health").
		Handler(errorRouteHandler).
		Name("errors")

	builder.BuildAll()
}

func getSecret() func(c router.Context) error {
	return func(c router.Context) error {
		c.SetHeader(router.HeaderContentType, "text/plain")
		return c.Send([]byte("secret"))
	}
}

func main() {

	app := newFiberAdapter()
	// app := newHTTPServerAdapter()
	store := NewUserStore()

	// Serve static files
	app.WrappedRouter().Static("/static", "./views")

	// Create API routes
	createRoutes(app, store)

	app.Router().PrintRoutes()

	// Front-end routes with HTML rendering
	front := app.Router().Use(router.ToMiddleware(func(c router.Context) error {
		c.SetHeader(router.HeaderContentType, "text/html")
		return c.Next()
	}))

	// Add flash middleware for front-end routes
	front.Use(flashmw.New())

	// Front-end route handlers
	createFrontEndRoutes(front, store)

	// OpenAPI documentation
	router.ServeOpenAPI(front, &router.OpenAPIRenderer{
		Title:   "My Test App",
		Version: "v0.0.1",
		Description: `## API Documentation
This playground exposes, and documents all the endpoints used by the demo application. Endpoints are documented, and provide payload examples, as well as interactive demos.

### Version
The API version: v0.0.0..
		`,
		Contact: &router.OpenAPIFieldContact{
			Email: "test@example.com",
			Name:  "Test Name",
			URL:   "https://example.com",
		},
	})

	go func() {
		if err := app.Serve(":9092"); err != nil {
			log.Panic(err)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	_ = <-c

	ctx := context.TODO()
	if err := app.Shutdown(ctx); err != nil {
		log.Panic(err)
	}

}

type DomainError struct {
	Type    string
	Code    int
	Message string
}

func (e *DomainError) Error() string {
	return e.Message
}

// Custom error mapper for domain errors
func domainErrorMapper(err error) *errors.Error {
	var domainErr *DomainError
	if errors.As(err, &domainErr) {
		return errors.Wrap(err, errors.CategoryRouting, domainErr.Message).
			WithCode(domainErr.Code).
			WithTextCode(domainErr.Type)
	}
	return nil
}

type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func createUser(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		var req CreateUserRequest
		if err := c.Bind(&req); err != nil {
			return router.NewBadRequestError("Invalid request body")
		}

		if req.Name == "" || req.Email == "" {
			return router.NewValidationError("Invalid request body", errors.ValidationErrors{
				{
					Field:   "name",
					Message: "name required",
				},
				{
					Field:   "email",
					Message: "email required",
				},
			})
		}

		id, err := hashid.New(req.Email)
		if err != nil {
			return router.NewBadRequestError("Invalid request body")
		}

		user := User{
			ID:        id,
			Name:      req.Name,
			Email:     req.Email,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		store.Lock()
		store.users[user.ID] = user
		store.Unlock()

		return c.JSON(http.StatusCreated, user)
	}
}

func listUsers(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		store.RLock()
		users := make([]User, 0, len(store.users))
		for _, user := range store.users {
			users = append(users, user)
		}
		store.RUnlock()

		return c.JSON(http.StatusOK, users)
	}
}

func getUser(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")

		store.RLock()
		user, exists := store.users[id]
		store.RUnlock()

		if !exists {
			return &DomainError{
				Type:    "USER_NOT_FOUND",
				Code:    http.StatusNotFound,
				Message: "User not found",
			}
		}

		return c.JSON(http.StatusOK, user)
	}
}

type UpdateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func updateUser(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")

		var req UpdateUserRequest
		if err := c.Bind(&req); err != nil {
			return router.NewBadRequestError("Invalid request body")
		}

		store.Lock()
		defer store.Unlock()

		user, exists := store.users[id]
		if !exists {
			return &DomainError{
				Type:    "USER_NOT_FOUND",
				Code:    http.StatusNotFound,
				Message: "User not found",
			}
		}

		if req.Name != "" {
			user.Name = req.Name
		}

		if req.Email != "" {
			user.Email = req.Email
		}

		user.UpdatedAt = time.Now()

		store.users[id] = user

		return c.JSON(http.StatusOK, user)
	}
}

func deleteUser(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")

		store.Lock()
		_, exists := store.users[id]
		if !exists {
			store.Unlock()
			return &DomainError{
				Type:    "USER_NOT_FOUND",
				Code:    http.StatusNotFound,
				Message: "User not found",
			}
		}

		delete(store.users, id)
		store.Unlock()

		return c.JSON(http.StatusNoContent, nil)
	}
}

// ==============================================
// Front-End Route Handlers (HTML Rendering)
// ==============================================

func createFrontEndRoutes[T any](front router.Router[T], store *UserStore) {
	// Home page - User list
	front.Get("/", renderUserList(store))

	// Create user form
	front.Get("/users/new", renderCreateForm())

	// User detail page
	front.Get("/users/:id", renderUserDetail(store))

	// Edit user form
	front.Get("/users/:id/edit", renderEditForm(store))

	// Form submissions
	front.Post("/users", handleCreateUser(store))
	front.Post("/users/:id", handleUpdateUser(store))
	front.Post("/users/:id/delete", handleDeleteUser(store))
}

func renderUserList(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		store.RLock()
		users := make([]User, 0, len(store.users))
		for _, user := range store.users {
			users = append(users, user)
		}
		store.RUnlock()

		return c.Render("index", map[string]any{
			"users": users,
		})
	}
}

func renderCreateForm() router.HandlerFunc {
	return func(c router.Context) error {
		return c.Render("user-form", map[string]any{
			"mode": "create",
		})
	}
}

func renderUserDetail(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")

		store.RLock()
		user, exists := store.users[id]
		store.RUnlock()

		if !exists {
			return c.Render("error", map[string]any{
				"error_code":    404,
				"error_title":   "User Not Found",
				"error_message": fmt.Sprintf("The user with ID %s does not exist.", id),
			})
		}

		return c.Render("user-detail", map[string]any{
			"user": user,
		})
	}
}

func renderEditForm(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")

		store.RLock()
		user, exists := store.users[id]
		store.RUnlock()

		if !exists {
			return c.Render("error", map[string]any{
				"error_code":    404,
				"error_title":   "User Not Found",
				"error_message": fmt.Sprintf("The user with ID %s does not exist.", id),
			})
		}

		return c.Render("user-form", map[string]any{
			"mode": "edit",
			"user": user,
		})
	}
}

func handleCreateUser(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		name := c.FormValue("name")
		email := c.FormValue("email")

		// Validation
		var validationErrors []map[string]string
		if name == "" {
			validationErrors = append(validationErrors, map[string]string{
				"field":   "name",
				"message": "Name is required",
			})
		}
		if email == "" {
			validationErrors = append(validationErrors, map[string]string{
				"field":   "email",
				"message": "Email is required",
			})
		}

		if len(validationErrors) > 0 {
			return c.Render("user-form", map[string]any{
				"mode":   "create",
				"errors": validationErrors,
				"user": map[string]string{
					"name":  name,
					"email": email,
				},
			})
		}

		// Create user
		id, err := hashid.New(email)
		if err != nil {
			return c.Render("user-form", map[string]any{
				"mode": "create",
				"errors": []map[string]string{
					{
						"field":   "email",
						"message": "Invalid email format",
					},
				},
				"user": map[string]string{
					"name":  name,
					"email": email,
				},
			})
		}

		user := User{
			ID:        id,
			Name:      name,
			Email:     email,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		store.Lock()
		store.users[user.ID] = user
		store.Unlock()

		// Set flash message and redirect
		return flash.Redirect(c, fmt.Sprintf("/users/%s", user.ID), router.ViewContext{
			"success":         true,
			"success_message": fmt.Sprintf("User '%s' has been created successfully", name),
		})
	}
}

func handleUpdateUser(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")
		name := c.FormValue("name")

		store.Lock()
		defer store.Unlock()

		user, exists := store.users[id]
		if !exists {
			return c.Render("error", map[string]any{
				"error_code":    404,
				"error_title":   "User Not Found",
				"error_message": fmt.Sprintf("The user with ID %s does not exist.", id),
			})
		}

		// Validation
		if name == "" {
			return c.Render("user-form", map[string]any{
				"mode": "edit",
				"user": user,
				"errors": []map[string]string{
					{
						"field":   "name",
						"message": "Name is required",
					},
				},
			})
		}

		// Update user
		user.Name = name
		user.UpdatedAt = time.Now()
		store.users[id] = user

		// Set flash message and redirect
		return flash.Redirect(c, fmt.Sprintf("/users/%s", user.ID), router.ViewContext{
			"success":         true,
			"success_message": fmt.Sprintf("User '%s' has been updated successfully", name),
		})
	}
}

func handleDeleteUser(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")

		store.Lock()
		user, exists := store.users[id]
		if !exists {
			store.Unlock()
			return flash.Redirect(c, "/", router.ViewContext{
				"error":         true,
				"error_message": "User not found",
			})
		}

		delete(store.users, id)
		store.Unlock()

		// Set flash message and redirect
		return flash.Redirect(c, "/", router.ViewContext{
			"success":         true,
			"success_message": fmt.Sprintf("User '%s' has been deleted successfully", user.Name),
		})
	}
}
