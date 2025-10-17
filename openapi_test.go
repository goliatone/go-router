package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAPIRenderer_GenerateOpenAPI_NilSafety(t *testing.T) {
	tests := []struct {
		name      string
		renderer  *OpenAPIRenderer
		wantPanic bool
	}{
		{
			name: "completely nil renderer",
			renderer: &OpenAPIRenderer{
				Contact:    nil,
				License:    nil,
				Info:       nil,
				Paths:      nil,
				Components: nil,
				Tags:       nil,
			},
			wantPanic: false,
		},
		{
			name: "nil Contact only",
			renderer: &OpenAPIRenderer{
				Contact: nil,
				License: &OpenAPIInfoLicense{Name: "MIT", Url: "https://opensource.org/licenses/MIT"},
				Info:    &OpenAPIInfo{Title: "Test API", Version: "1.0.0"},
			},
			wantPanic: false,
		},
		{
			name: "nil License only",
			renderer: &OpenAPIRenderer{
				Contact: &OpenAPIFieldContact{Name: "Test", Email: "test@example.com"},
				License: nil,
				Info:    &OpenAPIInfo{Title: "Test API", Version: "1.0.0"},
			},
			wantPanic: false,
		},
		{
			name: "nil Info only",
			renderer: &OpenAPIRenderer{
				Contact: &OpenAPIFieldContact{Name: "Test", Email: "test@example.com"},
				License: &OpenAPIInfoLicense{Name: "MIT", Url: "https://opensource.org/licenses/MIT"},
				Info:    nil,
			},
			wantPanic: false,
		},
		{
			name: "all valid",
			renderer: &OpenAPIRenderer{
				Contact:    &OpenAPIFieldContact{Name: "Test", Email: "test@example.com", URL: "https://example.com"},
				License:    &OpenAPIInfoLicense{Name: "MIT", Url: "https://opensource.org/licenses/MIT"},
				Info:       &OpenAPIInfo{Title: "Test API", Version: "1.0.0", Description: "A test API"},
				Paths:      make(map[string]any),
				Components: make(map[string]any),
				Tags:       make([]any, 0),
			},
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tt.wantPanic {
						t.Errorf("GenerateOpenAPI() panicked unexpectedly: %v", r)
					}
				} else if tt.wantPanic {
					t.Error("GenerateOpenAPI() expected to panic but didn't")
				}
			}()

			result := tt.renderer.GenerateOpenAPI()

			// Verify structure exists even with nil values
			if result == nil {
				t.Error("GenerateOpenAPI() returned nil")
				return
			}

			// Check required fields exist
			if _, ok := result["openapi"]; !ok {
				t.Error("Missing openapi field")
			}
			if _, ok := result["info"]; !ok {
				t.Error("Missing info field")
			}
			if _, ok := result["servers"]; !ok {
				t.Error("Missing servers field")
			}

			// Check info structure
			info, ok := result["info"].(map[string]any)
			if !ok {
				t.Error("info field is not a map")
				return
			}

			// Verify contact and license are always present (even if empty)
			if _, ok := info["contact"]; !ok {
				t.Error("Missing contact field in info")
			}
			if _, ok := info["license"]; !ok {
				t.Error("Missing license field in info")
			}
		})
	}
}

func TestOpenAPIRenderer_AppenRouteInfo_NilSafety(t *testing.T) {
	tests := []struct {
		name      string
		renderer  *OpenAPIRenderer
		routes    []RouteDefinition
		wantPanic bool
	}{
		{
			name: "nil Paths field",
			renderer: &OpenAPIRenderer{
				Paths: nil,
			},
			routes: []RouteDefinition{
				{
					Path:   "/test",
					Method: "GET",
				},
			},
			wantPanic: false,
		},
		{
			name: "empty routes slice",
			renderer: &OpenAPIRenderer{
				Paths: make(map[string]any),
			},
			routes:    []RouteDefinition{},
			wantPanic: false,
		},
		{
			name: "nil routes slice",
			renderer: &OpenAPIRenderer{
				Paths: make(map[string]any),
			},
			routes:    nil,
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tt.wantPanic {
						t.Errorf("AppenRouteInfo() panicked unexpectedly: %v", r)
					}
				} else if tt.wantPanic {
					t.Error("AppenRouteInfo() expected to panic but didn't")
				}
			}()

			result := tt.renderer.AppenRouteInfo(tt.routes)

			// Verify method returns the renderer
			if result != tt.renderer {
				t.Error("AppenRouteInfo() should return the same renderer instance")
			}

			// Verify Paths is initialized
			if tt.renderer.Paths == nil {
				t.Error("Paths should be initialized after AppenRouteInfo")
			}
		})
	}
}

func TestOpenAPIRenderer_WithMetadataProviders_NilSafety(t *testing.T) {
	tests := []struct {
		name      string
		renderer  *OpenAPIRenderer
		providers []OpenApiMetaGenerator
		wantPanic bool
	}{
		{
			name:      "nil providers slice",
			renderer:  &OpenAPIRenderer{},
			providers: nil,
			wantPanic: false,
		},
		{
			name:      "empty providers slice",
			renderer:  &OpenAPIRenderer{},
			providers: []OpenApiMetaGenerator{},
			wantPanic: false,
		},
		{
			name:     "nil renderer providers field",
			renderer: &OpenAPIRenderer{providers: nil},
			providers: []OpenApiMetaGenerator{
				&mockMetaGenerator{},
			},
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tt.wantPanic {
						t.Errorf("WithMetadataProviders() panicked unexpectedly: %v", r)
					}
				} else if tt.wantPanic {
					t.Error("WithMetadataProviders() expected to panic but didn't")
				}
			}()

			result := tt.renderer.WithMetadataProviders(tt.providers...)

			// Verify method returns the renderer
			if result != tt.renderer {
				t.Error("WithMetadataProviders() should return the same renderer instance")
			}
		})
	}
}

func TestServeOpenAPI_IncludesRegisteredRoutes(t *testing.T) {
	app := NewHTTPServer().(*HTTPServer)
	r := app.Router()

	r.Get("/users", func(c Context) error {
		return c.SendString("ok")
	}).SetName("users.list")

	renderer := NewOpenAPIRenderer()
	ServeOpenAPI(r, renderer)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	app.WrappedRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode openapi document: %v", err)
	}

	paths, ok := payload["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi document missing paths: %#v", payload["paths"])
	}

	if _, ok := paths["/users"]; !ok {
		t.Fatalf("expected /users to be present in openapi paths, got %v", paths)
	}
}

func TestServeOpenAPI_IncludesLateRegisteredRoutes(t *testing.T) {
	app := NewHTTPServer().(*HTTPServer)
	r := app.Router()

	renderer := NewOpenAPIRenderer()
	ServeOpenAPI(r, renderer)

	r.Get("/posts", func(c Context) error {
		return c.SendString("ok")
	}).SetName("posts.list")

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	app.WrappedRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode openapi document: %v", err)
	}

	paths, ok := payload["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi document missing paths: %#v", payload["paths"])
	}

	if _, ok := paths["/posts"]; !ok {
		t.Fatalf("expected /posts to be present in openapi paths, got %v", paths)
	}
}

func TestServeOpenAPI_CanExcludeDocumentationRoutes(t *testing.T) {
	app := NewHTTPServer().(*HTTPServer)
	r := app.Router()

	r.Get("/widgets", func(c Context) error {
		return c.SendString("ok")
	}).SetName("widgets.list")

	renderer := NewOpenAPIRenderer()
	ServeOpenAPI(r, renderer, WithOpenAPIEndpointsInSpec(false))

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	app.WrappedRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode openapi document: %v", err)
	}

	paths, ok := payload["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi document missing paths: %#v", payload["paths"])
	}

	if _, ok := paths["/openapi.json"]; ok {
		t.Fatalf("did not expect /openapi.json in openapi paths when disabled: %v", paths)
	}

	if _, ok := paths["/widgets"]; !ok {
		t.Fatalf("expected /widgets to be present in openapi paths, got %v", paths)
	}
}

func TestServeOpenAPI_CompilesMetadataProviders(t *testing.T) {
	app := NewHTTPServer().(*HTTPServer)
	r := app.Router()

	provider := &stubMetadataProvider{}
	aggregator := NewMetadataAggregator()
	aggregator.AddProvider(provider)

	renderer := NewOpenAPIRenderer()
	renderer.WithMetadataProviders(aggregator)

	ServeOpenAPI(r, renderer)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	app.WrappedRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode openapi document: %v", err)
	}

	paths, ok := payload["paths"].(map[string]any)
	if !ok {
		t.Fatalf("openapi document missing paths: %#v", payload["paths"])
	}

	if _, ok := paths["/aggregated"]; !ok {
		t.Fatalf("expected /aggregated to be present in openapi paths, got %v", paths)
	}
}

type stubMetadataProvider struct{}

func (s *stubMetadataProvider) GetMetadata() ResourceMetadata {
	return ResourceMetadata{
		Name:       "aggregated",
		PluralName: "aggregated",
		Routes: []RouteDefinition{
			{
				Method:  GET,
				Path:    "/aggregated",
				Name:    "aggregated.index",
				Summary: "Aggregated endpoint",
			},
		},
		Tags: []string{"aggregated"},
		Schema: SchemaMetadata{
			Properties: map[string]PropertyInfo{},
		},
	}
}

func TestOpenAPIRenderer_AppendServer_NilSafety(t *testing.T) {
	tests := []struct {
		name        string
		renderer    *OpenAPIRenderer
		url         string
		description string
		wantPanic   bool
	}{
		{
			name:        "nil Servers slice",
			renderer:    &OpenAPIRenderer{Servers: nil},
			url:         "https://api.example.com",
			description: "Production server",
			wantPanic:   false,
		},
		{
			name:        "empty strings",
			renderer:    &OpenAPIRenderer{},
			url:         "",
			description: "",
			wantPanic:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tt.wantPanic {
						t.Errorf("AppendServer() panicked unexpectedly: %v", r)
					}
				} else if tt.wantPanic {
					t.Error("AppendServer() expected to panic but didn't")
				}
			}()

			result := tt.renderer.AppendServer(tt.url, tt.description)

			// Verify method returns the renderer
			if result != tt.renderer {
				t.Error("AppendServer() should return the same renderer instance")
			}

			// Verify server was added
			if len(tt.renderer.Servers) == 0 {
				t.Error("Server should have been added")
			}
		})
	}
}

func TestNewOpenAPIRenderer_InitializesNonNilFields(t *testing.T) {
	renderer := NewOpenAPIRenderer()

	if renderer == nil {
		t.Fatal("NewOpenAPIRenderer() returned nil")
	}

	if renderer.Info == nil {
		t.Error("Info should be initialized")
	}

	if renderer.Contact == nil {
		t.Error("Contact should be initialized")
	}

	if renderer.License == nil {
		t.Error("License should be initialized")
	}
}

// mockMetaGenerator is a test helper that implements OpenApiMetaGenerator
type mockMetaGenerator struct{}

func (m *mockMetaGenerator) GenerateOpenAPI() map[string]any {
	return map[string]any{
		"paths": map[string]any{
			"/mock": map[string]any{
				"get": map[string]any{
					"summary": "Mock endpoint",
				},
			},
		},
	}
}

func TestEither_Function(t *testing.T) {
	tests := []struct {
		name     string
		inputs   []string
		expected string
	}{
		{
			name:     "all empty strings",
			inputs:   []string{"", "", ""},
			expected: "",
		},
		{
			name:     "first non-empty",
			inputs:   []string{"first", "second", "third"},
			expected: "first",
		},
		{
			name:     "middle non-empty",
			inputs:   []string{"", "second", "third"},
			expected: "second",
		},
		{
			name:     "last non-empty",
			inputs:   []string{"", "", "third"},
			expected: "third",
		},
		{
			name:     "empty slice",
			inputs:   []string{},
			expected: "",
		},
		{
			name:     "nil slice",
			inputs:   nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := either(tt.inputs...)
			if result != tt.expected {
				t.Errorf("either() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestJoinPaths_Function(t *testing.T) {
	tests := []struct {
		name     string
		parts    []string
		expected string
	}{
		{
			name:     "empty parts",
			parts:    []string{},
			expected: "/",
		},
		{
			name:     "nil parts",
			parts:    nil,
			expected: "/",
		},
		{
			name:     "single part",
			parts:    []string{"api"},
			expected: "/api",
		},
		{
			name:     "multiple parts",
			parts:    []string{"api", "v1", "users"},
			expected: "/api/v1/users",
		},
		{
			name:     "parts with slashes",
			parts:    []string{"/api/", "/v1/", "/users/"},
			expected: "/api/v1/users",
		},
		{
			name:     "empty strings in parts",
			parts:    []string{"api", "", "users"},
			expected: "/api/users",
		},
		{
			name:     "only empty and slash strings",
			parts:    []string{"", "/", ""},
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := joinPaths(tt.parts...)
			if result != tt.expected {
				t.Errorf("joinPaths() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNewOpenAPIRenderer_StructMerging(t *testing.T) {
	tests := []struct {
		name      string
		overrides []OpenAPIRenderer
		verify    func(*testing.T, *OpenAPIRenderer)
	}{
		{
			name:      "no overrides - default initialization",
			overrides: []OpenAPIRenderer{},
			verify: func(t *testing.T, renderer *OpenAPIRenderer) {
				if renderer.Info == nil {
					t.Error("Info should be initialized")
				}
				if renderer.Contact == nil {
					t.Error("Contact should be initialized")
				}
				if renderer.License == nil {
					t.Error("License should be initialized")
				}
				if renderer.Paths == nil {
					t.Error("Paths should be initialized")
				}
				if renderer.Components == nil {
					t.Error("Components should be initialized")
				}
			},
		},
		{
			name: "single override - basic fields",
			overrides: []OpenAPIRenderer{
				{
					Title:       "Test API",
					Version:     "1.0.0",
					Description: "A test API",
				},
			},
			verify: func(t *testing.T, renderer *OpenAPIRenderer) {
				if renderer.Title != "Test API" {
					t.Errorf("Title = %v, want %v", renderer.Title, "Test API")
				}
				if renderer.Version != "1.0.0" {
					t.Errorf("Version = %v, want %v", renderer.Version, "1.0.0")
				}
				if renderer.Description != "A test API" {
					t.Errorf("Description = %v, want %v", renderer.Description, "A test API")
				}
			},
		},
		{
			name: "multiple overrides - later wins",
			overrides: []OpenAPIRenderer{
				{Title: "First API", Version: "1.0.0"},
				{Title: "Second API", Description: "Updated description"},
			},
			verify: func(t *testing.T, renderer *OpenAPIRenderer) {
				if renderer.Title != "Second API" {
					t.Errorf("Title = %v, want %v", renderer.Title, "Second API")
				}
				if renderer.Version != "1.0.0" {
					t.Errorf("Version = %v, want %v", renderer.Version, "1.0.0")
				}
				if renderer.Description != "Updated description" {
					t.Errorf("Description = %v, want %v", renderer.Description, "Updated description")
				}
			},
		},
		{
			name: "contact and license overrides",
			overrides: []OpenAPIRenderer{
				{
					Contact: &OpenAPIFieldContact{
						Name:  "John Doe",
						Email: "john@example.com",
						URL:   "https://johndoe.com",
					},
					License: &OpenAPIInfoLicense{
						Name: "MIT",
						Url:  "https://opensource.org/licenses/MIT",
					},
				},
			},
			verify: func(t *testing.T, renderer *OpenAPIRenderer) {
				if renderer.Contact.Name != "John Doe" {
					t.Errorf("Contact.Name = %v, want %v", renderer.Contact.Name, "John Doe")
				}
				if renderer.Contact.Email != "john@example.com" {
					t.Errorf("Contact.Email = %v, want %v", renderer.Contact.Email, "john@example.com")
				}
				if renderer.License.Name != "MIT" {
					t.Errorf("License.Name = %v, want %v", renderer.License.Name, "MIT")
				}
			},
		},
		{
			name: "info struct override",
			overrides: []OpenAPIRenderer{
				{
					Info: &OpenAPIInfo{
						Title:       "Info API",
						Version:     "2.0.0",
						Description: "From Info struct",
					},
				},
			},
			verify: func(t *testing.T, renderer *OpenAPIRenderer) {
				if renderer.Info.Title != "Info API" {
					t.Errorf("Info.Title = %v, want %v", renderer.Info.Title, "Info API")
				}
				if renderer.Info.Version != "2.0.0" {
					t.Errorf("Info.Version = %v, want %v", renderer.Info.Version, "2.0.0")
				}
			},
		},
		{
			name: "slice appending",
			overrides: []OpenAPIRenderer{
				{
					Servers: []OpenAPIServer{
						{Url: "https://api1.example.com", Description: "Server 1"},
					},
					Tags: []any{"tag1", "tag2"},
				},
				{
					Servers: []OpenAPIServer{
						{Url: "https://api2.example.com", Description: "Server 2"},
					},
					Tags: []any{"tag3"},
				},
			},
			verify: func(t *testing.T, renderer *OpenAPIRenderer) {
				if len(renderer.Servers) != 2 {
					t.Errorf("Servers length = %v, want %v", len(renderer.Servers), 2)
				}
				if len(renderer.Tags) != 3 {
					t.Errorf("Tags length = %v, want %v", len(renderer.Tags), 3)
				}
				if renderer.Servers[0].Url != "https://api1.example.com" {
					t.Errorf("First server URL = %v, want %v", renderer.Servers[0].Url, "https://api1.example.com")
				}
				if renderer.Servers[1].Url != "https://api2.example.com" {
					t.Errorf("Second server URL = %v, want %v", renderer.Servers[1].Url, "https://api2.example.com")
				}
			},
		},
		{
			name: "map merging",
			overrides: []OpenAPIRenderer{
				{
					Paths: map[string]any{
						"/users": map[string]any{"get": "get users"},
						"/posts": map[string]any{"get": "get posts"},
					},
					Components: map[string]any{
						"User": map[string]any{"type": "object"},
					},
				},
				{
					Paths: map[string]any{
						"/users":    map[string]any{"post": "create user"}, // Should override
						"/comments": map[string]any{"get": "get comments"}, // Should add
					},
					Components: map[string]any{
						"Post": map[string]any{"type": "object"}, // Should add
					},
				},
			},
			verify: func(t *testing.T, renderer *OpenAPIRenderer) {
				if len(renderer.Paths) != 3 {
					t.Errorf("Paths length = %v, want %v", len(renderer.Paths), 3)
				}
				if len(renderer.Components) != 2 {
					t.Errorf("Components length = %v, want %v", len(renderer.Components), 2)
				}

				// Check that /users was overridden
				usersPath, exists := renderer.Paths["/users"]
				if !exists {
					t.Error("/users path should exist")
				} else if usersMap, ok := usersPath.(map[string]any); ok {
					if usersMap["post"] != "create user" {
						t.Error("/users POST should be overridden")
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := NewOpenAPIRenderer(tt.overrides...)
			tt.verify(t, renderer)
		})
	}
}

func TestMergeHelperFunctions(t *testing.T) {
	t.Run("empty contact handling", func(t *testing.T) {
		// Test that empty contact fields are not overridden by empty values
		renderer := NewOpenAPIRenderer(
			OpenAPIRenderer{
				Contact: &OpenAPIFieldContact{Name: "John"},
			},
			OpenAPIRenderer{
				Info: &OpenAPIInfo{
					Contact: OpenAPIFieldContact{}, // Empty contact should not override
				},
			},
		)

		if renderer.Contact.Name != "John" {
			t.Error("Empty contact in Info should not override existing contact")
		}
	})

	t.Run("empty license handling", func(t *testing.T) {
		// Test that empty license fields are not overridden by empty values
		renderer := NewOpenAPIRenderer(
			OpenAPIRenderer{
				License: &OpenAPIInfoLicense{Name: "MIT"},
			},
			OpenAPIRenderer{
				Info: &OpenAPIInfo{
					License: OpenAPIInfoLicense{}, // Empty license should not override
				},
			},
		)

		if renderer.License.Name != "MIT" {
			t.Error("Empty license in Info should not override existing license")
		}
	})
}

func TestPartialOverrides(t *testing.T) {
	t.Run("partial contact merge", func(t *testing.T) {
		renderer := NewOpenAPIRenderer(
			OpenAPIRenderer{
				Contact: &OpenAPIFieldContact{
					Name:  "Initial Name",
					Email: "initial@example.com",
				},
			},
			OpenAPIRenderer{
				Contact: &OpenAPIFieldContact{
					Email: "updated@example.com",
					URL:   "https://example.com",
				},
			},
		)

		if renderer.Contact.Name != "Initial Name" {
			t.Errorf("Contact.Name should be preserved = %v", renderer.Contact.Name)
		}
		if renderer.Contact.Email != "updated@example.com" {
			t.Errorf("Contact.Email should be updated = %v", renderer.Contact.Email)
		}
		if renderer.Contact.URL != "https://example.com" {
			t.Errorf("Contact.URL should be added = %v", renderer.Contact.URL)
		}
	})

	t.Run("empty string doesn't override", func(t *testing.T) {
		renderer := NewOpenAPIRenderer(
			OpenAPIRenderer{Title: "Original Title"},
			OpenAPIRenderer{Title: ""}, // Empty string shouldn't override
		)

		if renderer.Title != "Original Title" {
			t.Errorf("Title should be preserved = %v", renderer.Title)
		}
	})
}
