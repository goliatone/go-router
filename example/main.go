package main

import (
	"context"
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

func newHTTPServer() router.Server[*httprouter.Router] {
	return router.NewHTTPServer()
}

func main() {

	app := newFiberAdapter()
	store := NewUserStore()

	app.Router().Use(func(c router.Context) error {
		c.SetHeader("Content-Type", "application/json")
		return c.Next()
	})

	users := app.Router().Group("/api/users")
	{
		users.Post("", createUser(store)).Name("user.create")
		users.Get("", listUsers(store)).Name("user.list")
		users.Get("/:id", getUser(store)).Name("user.get")
		users.Put("/:id", updateUser(store)).Name("user.update")
		users.Delete("/:id", deleteUser(store)).Name("user.delete")
	}

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

type CreateUserRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func createUser(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		var req CreateUserRequest
		if err := c.Bind(&req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		}

		if req.Name == "" || req.Email == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "name and email are required"})
		}

		id, err := hashid.New(req.Email)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
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
			return c.JSON(http.StatusNotFound, map[string]string{"error": "user not found"})
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
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		}

		store.Lock()
		defer store.Unlock()

		user, exists := store.users[id]
		if !exists {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "user not found"})
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
			return c.JSON(http.StatusNotFound, map[string]string{"error": "user not found"})
		}

		delete(store.users, id)
		store.Unlock()

		return c.JSON(http.StatusNoContent, nil)
	}
}
