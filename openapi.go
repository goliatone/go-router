package router

import (
	"fmt"
	"net/http"
	"strings"
)

// Functional options for configuring ServeOpenAPI
type openAPIConfig struct {
	docsPath    string
	openapiPath string
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

// Default paths: /meta/docs and /openapi.json
func defaultOpenAPIConfig() *openAPIConfig {
	return &openAPIConfig{
		docsPath:    "/meta/docs",
		openapiPath: "/openapi.json",
	}
}

type OpenAPIRenderer struct {
	Title       string
	Version     string
	Description string
}

func (o *OpenAPIRenderer) GenerateOpenAPI(routes []RouteDefinition) map[string]any {
	paths := make(map[string]any)
	for _, rt := range routes {
		op := map[string]any{
			"summary":     rt.Operation.Description,
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
		},
		"paths": paths,
	}
}

func ServeOpenAPI[T any](router Router[T], renderer *OpenAPIRenderer, opts ...OpenAPIOption) {
	cfg := defaultOpenAPIConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	// Serve OpenAPI JSON
	router.Get(cfg.openapiPath, func(c Context) error {
		doc := renderer.GenerateOpenAPI(router.Routes())
		return c.JSON(http.StatusOK, doc)
	})

	// Serve Stoplight Elements UI
	router.Get(cfg.docsPath, func(c Context) error {
		html := `
<!DOCTYPE html>
<html>
<head>
<title>API Docs</title>
<link rel="stylesheet" href="https://unpkg.com/@stoplight/elements/styles.min.css">
</head>
<body>
<div id="elements"></div>
<script src="https://unpkg.com/@stoplight/elements/web-components.min.js"></script>
<script>
  const Elements = window["@stoplight/elements"];
  Elements.loadElements(document.getElementById('elements'), {
    layout: 'sidebar',
    apiDescriptionUrl: '` + cfg.openapiPath + `'
  });
</script>
</body>
</html>
`
		return c.Send([]byte(html))
	})
}
