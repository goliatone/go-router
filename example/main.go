package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/goliatone/go-router"
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
	app := router.NewFiberAdapter(func(a *fiber.App) *fiber.App {
		return fiber.New(
			fiber.Config{
				AppName:           "Go Router - Fiber",
				EnablePrintRoutes: true,
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

	app.Router().Use(router.ToMiddleware(func(c router.Context) error {
		c.SetHeader(router.HeaderContentType, "application/json")
		return c.Next()
	}))

	var auth router.HandlerFunc = func(c router.Context) error {
		if pwd := c.Header(router.HeaderAuthorization); pwd == "password" {
			return c.Next()
		}
		return router.NewUnauthorizedError("unauthorized")
	}

	users := app.Router().Group("/api/users")
	{
		users.Post("", createUser(store)).Name("user.create")
		users.Get("", listUsers(store)).Name("user.list")
		users.Get("/:id", getUser(store)).Name("user.get")
		users.Put("/:id", updateUser(store)).Name("user.update")
		users.Delete("/:id", deleteUser(store)).Name("user.delete")
	}

	private := app.Router().Group("/api/secret")
	{
		private.Use(auth.AsMiddlware())
		private.Get("/:name", getSecret()).Name("secrets.get")
	}

	builder := router.NewRouteBuilder(app.Router())
	builder.NewRoute().
		GET().
		Path("/health").
		Description("Health endpoint to get information about: health status").
		Tags("health", "http").
		Handler(healthRouteHandler).
		Name("health")

	builder.NewRoute().
		GET().
		Path("/errors").
		Description("Errors endpoint to get information about: errors").
		Tags("health", "http").
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
	createRoutes(app, store)

	app.Router().PrintRoutes()

	router.ServeOpenAPI(app.Router(), &router.OpenAPIRenderer{
		Title:       "My Test App",
		Version:     "v0.0.1",
		Description: "This is my description, but nothing more",
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
func domainErrorMapper(err error) *router.RouterError {
	var domainErr *DomainError
	if errors.As(err, &domainErr) {
		return &router.RouterError{
			Type:     router.ErrorType(domainErr.Type),
			Code:     domainErr.Code,
			Message:  domainErr.Message,
			Metadata: map[string]any{},
		}
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
			return router.NewValidationError("Invalid request body", []router.ValidationError{
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
		id := c.Param("id")

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
		id := c.Param("id")

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
		id := c.Param("id")

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
