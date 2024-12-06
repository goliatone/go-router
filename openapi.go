package router

import (
	"fmt"
	"net/http"
	"strings"

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
}

func (o *OpenAPIRenderer) GenerateOpenAPI(routes []RouteDefinition) map[string]any {
	paths := make(map[string]any)
	for _, rt := range routes {
		op := map[string]any{
			"summary":     rt.Operation.Summary,
			"description": rt.Operation.Description,
			"tags":        rt.Operation.Tags,
			"responses":   map[string]any{},
		}

		// Parameters
		var params []any
		for _, p := range rt.Operation.Parameters {
			params = append(params, map[string]any{
				"name":     p.Name,
				"in":       p.In,
				"required": p.Required,
				"schema":   p.Schema,
			})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}

		// RequestBody
		if rb := rt.Operation.RequestBody; rb != nil {
			op["requestBody"] = map[string]any{
				"description": rb.Description,
				"required":    rb.Required,
				"content":     rb.Content,
			}
		}

		// Responses
		respObj := map[string]any{}
		for _, r := range rt.Operation.Responses {
			respObj[fmt.Sprintf("%d", r.Code)] = map[string]any{
				"description": r.Description,
				"content":     r.Content,
			}
		}
		if len(respObj) > 0 {
			op["responses"] = respObj
		}

		pathItem, ok := paths[rt.Path].(map[string]any)
		if !ok {
			pathItem = map[string]any{}
		}
		methodLower := strings.ToLower(string(rt.Method))
		pathItem[methodLower] = op
		paths[rt.Path] = pathItem
	}

	return map[string]any{
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
		"paths": paths,
	}
}

func ServeOpenAPI[T any](router Router[T], renderer *OpenAPIRenderer, opts ...OpenAPIOption) {
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

	doc := renderer.GenerateOpenAPI(router.Routes())

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
