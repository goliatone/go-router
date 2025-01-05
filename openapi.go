package router

import (
	"fmt"
	"net/http"
	"strings"

	"dario.cat/mergo"
	"gopkg.in/yaml.v2"
)

// Functional options for configuring ServeOpenAPI
type openAPIConfig struct {
	docsPath    string
	openapiPath string
	title       string
}

type OpenAPIOption func(*openAPIConfig)

func WithDocsPath(path string) OpenAPIOption {
	return func(cfg *openAPIConfig) {
		cfg.docsPath = path
	}
}

func WithOpenAPIPath(path string) OpenAPIOption {
	return func(cfg *openAPIConfig) {
		cfg.openapiPath = path
	}
}
func WithTitle(title string) OpenAPIOption {
	return func(cfg *openAPIConfig) {
		cfg.title = title
	}
}

// Default paths: /meta/docs and /openapi.json
func defaultOpenAPIConfig() *openAPIConfig {
	return &openAPIConfig{
		docsPath:    "/meta/docs/",
		openapiPath: "/openapi",
		title:       "API Documentation",
	}
}

type OpenAPIFieldContact struct {
	Email string
	Name  string
	URL   string
}

type OpenAPIRenderer struct {
	Title       string
	Version     string
	Description string
	Contact     OpenAPIFieldContact
	Routes      []RouteDefinition
	Paths       map[string]any
	Tags        []any
	Components  map[string]any
	providers   []OpenApiMetaGenerator
}

func (o *OpenAPIRenderer) WithMetadataProviders(providers ...OpenApiMetaGenerator) *OpenAPIRenderer {
	o.providers = append(o.providers, providers...)
	return o
}

// AppenRouteInfo updates the renderer with route information
func (o *OpenAPIRenderer) AppenRouteInfo(routes []RouteDefinition) {
	if o.Paths == nil {
		o.Paths = make(map[string]any)
	}

	for _, rt := range o.Routes {
		o.addRouteToPath(rt)
	}

	for _, rt := range routes {
		o.addRouteToPath(rt)
	}

	o.Routes = append(o.Routes, routes...)
}

func (o *OpenAPIRenderer) GenerateOpenAPI() map[string]any {
	base := map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       o.Title,
			"version":     o.Version,
			"description": o.Description,
			"contact": map[string]any{
				"email": o.Contact.Email,
				"name":  o.Contact.Name,
				"url":   o.Contact.URL,
			},
		},
		"paths":      o.Paths,
		"components": o.Components,
		"tags":       o.Tags,
	}

	for _, provider := range o.providers {
		overlay := provider.GenerateOpenAPI()

		// paths need special handling...
		if overlayPaths, ok := overlay["paths"].(map[string]any); ok {
			if base["paths"] == nil {
				base["paths"] = make(map[string]any)
			}
			basePaths := base["paths"].(map[string]any)

			// we need to merge each path individually
			for path, pathItem := range overlayPaths {
				fullPath := joinPaths(path)
				if existing, exists := basePaths[fullPath]; exists {
					merged := make(map[string]any)
					if existingMap, ok := existing.(map[string]any); ok {
						if err := mergo.Merge(&merged, existingMap, mergo.WithOverride); err != nil {
							continue
						}
					}
					if pathItemMap, ok := pathItem.(map[string]any); ok {
						if err := mergo.Merge(&merged, pathItemMap, mergo.WithOverride); err != nil {
							continue
						}
					}
					basePaths[fullPath] = merged
				} else {
					basePaths[fullPath] = pathItem
				}
			}

			// remove to prevent double merging
			delete(overlay, "paths")
		}

		// Merge the rest
		if err := mergo.Merge(&base, overlay, mergo.WithOverride, mergo.WithAppendSlice); err != nil {
			continue
		}
	}

	return base
}

type OpenApiMetaGenerator interface {
	GenerateOpenAPI() map[string]any
}

func ServeOpenAPI[T any](router Router[T], renderer OpenApiMetaGenerator, opts ...OpenAPIOption) {
	cfg := defaultOpenAPIConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	// We will serve /openapi.yaml by default: cfg.openapiPath + ".yaml"
	yamlPath := cfg.openapiPath
	if !strings.HasSuffix(yamlPath, ".yaml") {
		yamlPath = yamlPath + ".yaml"
	}

	jsonPath := cfg.openapiPath
	if !strings.HasSuffix(jsonPath, ".json") {
		jsonPath = jsonPath + ".json"
	}

	doc := renderer.GenerateOpenAPI()

	router.Get(jsonPath, func(c Context) error {
		return c.JSON(http.StatusOK, doc)
	})

	// Serve OpenAPI YAML
	router.Get(yamlPath, func(c Context) error {
		yamlBytes, err := yaml.Marshal(doc)
		if err != nil {
			return NewInternalError(err, "failed to geenrate yaml")
		}
		c.SetHeader("Content-Type", "application/yaml")
		return c.Send(yamlBytes)
	})

	// Serve Stoplight Elements UI
	router.Get(cfg.docsPath, func(c Context) error {
		html := `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="referrer" content="same-origin" />
    <meta name="viewport" content="width=device-width, initial-scale=1, shrink-to-fit=no" />
    <title>` + cfg.title + `</title>
    <link href="https://unpkg.com/@stoplight/elements@8.1.0/styles.min.css" rel="stylesheet" />
    <script src="https://unpkg.com/@stoplight/elements@8.1.0/web-components.min.js"
            integrity="sha256-985sDMZYbGa0LDS8jYmC4VbkVlh7DZ0TWejFv+raZII="
            crossorigin="anonymous"></script>
  </head>
  <body style="height: 100vh;">

    <elements-api
      apiDescriptionUrl="` + yamlPath + `"
      router="hash"
      layout="sidebar"
      tryItCredentialsPolicy="same-origin"
    ></elements-api>

  </body>
</html>`
		return c.Send([]byte(html))
	})
}

func (o *OpenAPIRenderer) addRouteToPath(rt RouteDefinition) {
	// Clean up the path and ensure it starts with /
	fullPath := joinPaths(rt.Path)

	op := map[string]any{
		"summary":     rt.Summary,
		"description": rt.Description,
		"tags":        rt.Tags,
		"responses":   make(map[string]any),
	}

	// Parameters
	var params []any
	for _, p := range rt.Parameters {
		params = append(params, map[string]any{
			"name":        p.Name,
			"in":          p.In,
			"required":    p.Required,
			"schema":      p.Schema,
			"description": p.Description,
		})
	}
	if len(params) > 0 {
		op["parameters"] = params
	}

	// RequestBody
	if rb := rt.RequestBody; rb != nil {
		op["requestBody"] = map[string]any{
			"description": rb.Description,
			"required":    rb.Required,
			"content":     rb.Content,
		}
	}

	// Responses
	for _, r := range rt.Responses {
		op["responses"].(map[string]any)[fmt.Sprintf("%d", r.Code)] = map[string]any{
			"description": r.Description,
			"content":     r.Content,
		}
	}

	// Get or create path item
	pathItem, exists := o.Paths[fullPath]
	if !exists {
		pathItem = make(map[string]any)
	}

	// Convert to map if it isn't already
	pathItemMap, ok := pathItem.(map[string]any)
	if !ok {
		pathItemMap = make(map[string]any)
	}

	// Add operation to path
	methodLower := strings.ToLower(string(rt.Method))
	pathItemMap[methodLower] = op

	// Update paths
	fmt.Printf("==== update paths %s\n", fullPath)
	o.Paths[fullPath] = pathItemMap
}

func joinPaths(parts ...string) string {
	cleanParts := make([]string, 0)
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" && p != "/" {
			cleanParts = append(cleanParts, strings.Trim(p, "/"))
		}
	}
	if len(cleanParts) == 0 {
		return "/"
	}
	return "/" + strings.Join(cleanParts, "/")
}
