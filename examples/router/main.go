package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"reflect"
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

type Company struct {
	ID   string `json:"id" crud:"label:name"`
	Name string `json:"name"`
}

type Profile struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name" crud:"label:display_name"`
}

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	CompanyID string    `json:"company_id" bun:"company_id"`
	Company   *Company  `json:"company,omitempty" bun:"rel:belongs-to,join:company_id=id"`
	ProfileID string    `json:"profile_id" bun:"profile_id"`
	Profile   *Profile  `json:"profile,omitempty" bun:"rel:belongs-to,join:profile_id=id" crud:"endpoint:/api/profiles,labelField:display_name,valueField:id,mode:search,searchParam:q"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UserStore struct {
	sync.RWMutex
	users            map[string]User
	companies        map[string]*Company
	profiles         map[string]*Profile
	defaultCompanyID string
}

func NewUserStore() *UserStore {
	email1 := "julie.smith@example.com"
	id1, _ := hashid.New(email1)

	email2 := "jose.bates@example.com"
	id2, _ := hashid.New(email2)

	email3 := "brad.miles@example.com"
	id3, _ := hashid.New(email3)

	companies := map[string]*Company{
		"acme": {
			ID:   "acme",
			Name: "Acme Incorporated",
		},
		"globex": {
			ID:   "globex",
			Name: "Globex Corporation",
		},
	}

	profileJulie := &Profile{ID: id1 + "-profile", DisplayName: "Julie S."}
	profileJose := &Profile{ID: id2 + "-profile", DisplayName: "Jose B."}
	profileBrad := &Profile{ID: id3 + "-profile", DisplayName: "Brad M."}

	profiles := map[string]*Profile{
		profileJulie.ID: profileJulie,
		profileJose.ID:  profileJose,
		profileBrad.ID:  profileBrad,
	}

	now := time.Now()

	users := map[string]User{
		id1: {
			ID:        id1,
			Name:      "Julie Smith",
			Email:     email1,
			CompanyID: "acme",
			Company:   companies["acme"],
			ProfileID: profileJulie.ID,
			Profile:   profileJulie,
			CreatedAt: now,
			UpdatedAt: now,
		},
		id2: {
			ID:        id2,
			Name:      "Jose Bates",
			Email:     email2,
			CompanyID: "globex",
			Company:   companies["globex"],
			ProfileID: profileJose.ID,
			Profile:   profileJose,
			CreatedAt: now,
			UpdatedAt: now,
		},
		id3: {
			ID:        id3,
			Name:      "Brad Miles",
			Email:     email3,
			CompanyID: "acme",
			Company:   companies["acme"],
			ProfileID: profileBrad.ID,
			Profile:   profileBrad,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	return &UserStore{
		users:            users,
		companies:        companies,
		profiles:         profiles,
		defaultCompanyID: "acme",
	}
}

func (s *UserStore) defaultCompany() *Company {
	if s == nil {
		return nil
	}
	if s.defaultCompanyID != "" {
		if company, ok := s.companies[s.defaultCompanyID]; ok {
			return company
		}
	}
	for _, company := range s.companies {
		if company != nil {
			return company
		}
	}
	return nil
}

type metadataProviderFunc func() router.ResourceMetadata

func (fn metadataProviderFunc) GetMetadata() router.ResourceMetadata {
	return fn()
}

func newMetadataAggregator() *router.MetadataAggregator {
	userMD := router.GetResourceMetadata(reflect.TypeOf(User{}))
	companyMD := router.GetResourceMetadata(reflect.TypeOf(Company{}))
	profileMD := router.GetResourceMetadata(reflect.TypeOf(Profile{}))

	aggregator := router.NewMetadataAggregator().
		WithRelationProvider(router.NewDefaultRelationProvider()).
		WithUISchemaOptions(router.UISchemaOptions{
			EndpointDefaults: func(resource *router.ResourceMetadata, relationName string, rel *router.RelationshipInfo) *router.EndpointHint {
				if rel.Endpoint != nil {
					return rel.Endpoint
				}

				switch relationName {
				case "company":
					return &router.EndpointHint{
						URL:        "/api/companies",
						Method:     "GET",
						LabelField: "name",
						ValueField: "id",
						Params: map[string]string{
							"select": "id,name",
							"order":  "name asc",
						},
					}
				case "profile":
					return &router.EndpointHint{
						URL:         "/api/profiles",
						Method:      "GET",
						LabelField:  "display_name",
						ValueField:  "id",
						Mode:        "search",
						SearchParam: "q",
					}
				}

				return nil
			},
			EndpointOverrides: map[string]map[string]*router.EndpointHint{
				userMD.Name: {
					"profile": {
						URL:         "/api/profiles/search",
						Method:      "GET",
						LabelField:  "display_name",
						ValueField:  "id",
						Mode:        "search",
						SearchParam: "query",
					},
				},
			},
		})

	aggregator.AddProviders(
		metadataProviderFunc(func() router.ResourceMetadata { return *userMD }),
		metadataProviderFunc(func() router.ResourceMetadata { return *companyMD }),
		metadataProviderFunc(func() router.ResourceMetadata { return *profileMD }),
	)

	return aggregator
}

func newFiberAdapter() router.Server[*fiber.App] {
	viewsDir := exampleViewsDir()

	cfg := router.NewSimpleViewConfig(viewsDir).
		WithAssets(viewsDir, "css", "js").
		WithURLPrefix("static").
		WithReload(true)

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

	companies := builder.Group("/companies")
	{
		companies.NewRoute().
			GET().
			Path("/").
			Summary("List companies").
			Description("Return all companies available to the demo").
			Tags("Company").
			Handler(listCompanies(store)).
			Name("company.list")

		companies.BuildAll()
	}

	profiles := builder.Group("/profiles")
	{
		profiles.NewRoute().
			GET().
			Path("/").
			Summary("List profiles").
			Description("Return user profiles including display names").
			Tags("Profile").
			Handler(listProfiles(store)).
			Name("profile.list")

		profiles.BuildAll()
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
	app.WrappedRouter().Static("/static", exampleViewsDir())

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
	openAPIRenderer := (&router.OpenAPIRenderer{
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
	}).WithMetadataProviders(newMetadataAggregator())

	router.ServeOpenAPI(front, openAPIRenderer)

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

func exampleViewsDir() string {
	return router.AbsFromCaller("views")
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

		now := time.Now()

		profileID, _ := hashid.New(fmt.Sprintf("%s:%d", req.Email, now.UnixNano()))
		profile := &Profile{
			ID:          profileID,
			DisplayName: req.Name,
		}

		store.Lock()
		if store.profiles == nil {
			store.profiles = make(map[string]*Profile)
		}
		store.profiles[profileID] = profile

		company := store.defaultCompany()

		user := User{
			ID:        id,
			Name:      req.Name,
			Email:     req.Email,
			CompanyID: "",
			Company:   nil,
			ProfileID: profileID,
			Profile:   profile,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if company != nil {
			user.CompanyID = company.ID
			user.Company = company
		}

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
			if profile, ok := store.profiles[user.ProfileID]; ok {
				user.Profile = profile
			}
			if company, ok := store.companies[user.CompanyID]; ok {
				user.Company = company
			}
			users = append(users, user)
		}
		store.RUnlock()

		return c.JSON(http.StatusOK, users)
	}
}

func listCompanies(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		store.RLock()
		companies := make([]Company, 0, len(store.companies))
		for _, company := range store.companies {
			if company != nil {
				companies = append(companies, *company)
			}
		}
		store.RUnlock()

		return c.JSON(http.StatusOK, companies)
	}
}

func listProfiles(store *UserStore) router.HandlerFunc {
	return func(c router.Context) error {
		store.RLock()
		profiles := make([]Profile, 0, len(store.profiles))
		for _, profile := range store.profiles {
			if profile != nil {
				profiles = append(profiles, *profile)
			}
		}
		store.RUnlock()

		return c.JSON(http.StatusOK, profiles)
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

		if profile, ok := store.profiles[user.ProfileID]; ok {
			user.Profile = profile
		}
		if company, ok := store.companies[user.CompanyID]; ok {
			user.Company = company
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
			if profile, ok := store.profiles[user.ProfileID]; ok {
				user.Profile = profile
			}
			if company, ok := store.companies[user.CompanyID]; ok {
				user.Company = company
			}
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

		if profile, ok := store.profiles[user.ProfileID]; ok {
			user.Profile = profile
		}
		if company, ok := store.companies[user.CompanyID]; ok {
			user.Company = company
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

		if profile, ok := store.profiles[user.ProfileID]; ok {
			user.Profile = profile
		}
		if company, ok := store.companies[user.CompanyID]; ok {
			user.Company = company
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

		now := time.Now()
		profileID, _ := hashid.New(fmt.Sprintf("%s:%d", email, now.UnixNano()))
		profile := &Profile{ID: profileID, DisplayName: name}

		store.Lock()
		if store.profiles == nil {
			store.profiles = make(map[string]*Profile)
		}
		store.profiles[profileID] = profile

		company := store.defaultCompany()

		user := User{
			ID:        id,
			Name:      name,
			Email:     email,
			CompanyID: "",
			Company:   nil,
			ProfileID: profileID,
			Profile:   profile,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if company != nil {
			user.CompanyID = company.ID
			user.Company = company
		}

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
