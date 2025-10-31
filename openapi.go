package router

import (
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"

	"dario.cat/mergo"
	"gopkg.in/yaml.v2"
)

// Functional options for configuring ServeOpenAPI
type openAPIConfig struct {
	docsPath        string
	openapiPath     string
	title           string
	includeDocPaths bool
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

func WithOpenAPIEndpointsInSpec(include bool) OpenAPIOption {
	return func(cfg *openAPIConfig) {
		cfg.includeDocPaths = include
	}
}

// Default paths: /meta/docs and /openapi.json
func defaultOpenAPIConfig() *openAPIConfig {
	return &openAPIConfig{
		docsPath:        "/meta/docs/",
		openapiPath:     "/openapi",
		title:           "API Documentation",
		includeDocPaths: true,
	}
}

type OpenAPIFieldContact struct {
	Email string
	Name  string
	URL   string
}

type OpenAPIRenderer struct {
	Info *OpenAPIInfo

	Servers  []OpenAPIServer
	Security []OpenAPISecuritySchemas
	// TODO: Remove
	Title          string
	Version        string
	Description    string
	TermsOfService string
	Contact        *OpenAPIFieldContact
	License        *OpenAPIInfoLicense

	Routes     []RouteDefinition
	Paths      map[string]any
	Tags       []any
	Components map[string]any
	providers  []OpenApiMetaGenerator
}

func NewOpenAPIRenderer(overrides ...OpenAPIRenderer) *OpenAPIRenderer {
	// base renderer with safe defaults
	base := &OpenAPIRenderer{
		Info:       &OpenAPIInfo{},
		Contact:    &OpenAPIFieldContact{},
		License:    &OpenAPIInfoLicense{},
		Servers:    make([]OpenAPIServer, 0),
		Security:   make([]OpenAPISecuritySchemas, 0),
		Routes:     make([]RouteDefinition, 0),
		Paths:      make(map[string]any),
		Tags:       make([]any, 0),
		Components: make(map[string]any),
		providers:  make([]OpenApiMetaGenerator, 0),
	}

	for _, override := range overrides {
		mergeOpenAPIRenderer(base, override)
	}

	return base
}

func (o *OpenAPIRenderer) WithMetadataProviders(providers ...OpenApiMetaGenerator) *OpenAPIRenderer {
	o.providers = append(o.providers, providers...)
	return o
}

func (o *OpenAPIRenderer) AppendServer(url, description string) *OpenAPIRenderer {
	o.Servers = append(o.Servers, OpenAPIServer{
		Url:         url,
		Description: description,
	})
	return o
}

// AppenRouteInfo updates the renderer with route information
func (o *OpenAPIRenderer) AppenRouteInfo(routes []RouteDefinition) *OpenAPIRenderer {
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

	return o
}

type OpenAPIInfo struct {
	Title          string
	Version        string
	Description    string
	TermsOfService string
	Contact        OpenAPIFieldContact
	License        OpenAPIInfoLicense
}

type OpenAPIInfoLicense struct {
	Name string
	Url  string
}

type OpenAPIServer struct {
	Url         string
	Description string
}

type OpenAPISecurity struct {
	Name         string
	Requirements []SecurityRequirement
}

type SecurityRequirement = string

type OpenAPISecuritySchemas struct {
	Name    string
	Schemas []OpenAPISecuritySchema
}

type OpenAPISecuritySchema struct {
	Name        string
	Type        string
	In          string
	Description string
}

func either(o ...string) string {
	for _, v := range o {
		if v != "" {
			return v
		}
	}
	return ""
}

func (o *OpenAPIRenderer) GenerateOpenAPI() map[string]any {
	// https://elements-demo.stoplight.io/#/operations/put-todos-id

	if o.Info == nil {
		o.Info = &OpenAPIInfo{}
	}

	base := map[string]any{
		"openapi": "3.0.3",
		"servers": []map[string]any{},
		"info": map[string]any{
			"title":          either(o.Info.Title, o.Title),
			"version":        either(o.Info.Version, o.Version),
			"description":    either(o.Info.Description, o.Description),
			"termsOfService": either(o.Info.TermsOfService, o.TermsOfService),
			"contact": func() map[string]any {
				if o.Contact != nil {
					return map[string]any{
						"email": o.Contact.Email,
						"name":  o.Contact.Name,
						"url":   o.Contact.URL,
					}
				}
				return map[string]any{
					"email": "",
					"name":  "",
					"url":   "",
				}
			}(),
			"license": func() map[string]any {
				if o.License != nil {
					return map[string]any{
						"name": o.License.Name,
						"url":  o.License.Url,
					}
				}
				return map[string]any{
					"name": "",
					"url":  "",
				}
			}(),
		},
		"paths":      o.Paths,
		"components": o.Components,
		"tags":       o.Tags,
	}

	baseServers := base["servers"].([]map[string]any)
	for _, server := range o.Servers {
		baseServers = append(baseServers, map[string]any{
			"url":         server.Url,
			"description": server.Description,
		})
	}
	base["servers"] = baseServers

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

	// Guard against concurrent access to the renderer when it cannot be cloned.
	var rendererMu sync.Mutex

	// We will serve /openapi.yaml by default: cfg.openapiPath + ".yaml"
	yamlPath := cfg.openapiPath
	if !strings.HasSuffix(yamlPath, ".yaml") {
		yamlPath = yamlPath + ".yaml"
	}

	jsonPath := cfg.openapiPath
	if !strings.HasSuffix(jsonPath, ".json") {
		jsonPath = jsonPath + ".json"
	}

	buildDoc := func() map[string]any {
		switch base := renderer.(type) {
		case *OpenAPIRenderer:
			cloned := cloneOpenAPIRenderer(base)
			routes := router.Routes()
			if !cfg.includeDocPaths {
				routes = filterOpenAPISelfRoutes(routes)
			}
			// Ensure base renderer state is accounted for before appending router metadata.
			cloned.AppenRouteInfo(nil)
			cloned.AppenRouteInfo(routes)
			autoCompileProviders(cloned.providers)
			return cloned.GenerateOpenAPI()
		default:
			// Fall back to locking around non-cloneable generators.
			rendererMu.Lock()
			defer rendererMu.Unlock()

			autoCompileGenerator(renderer)
			return renderer.GenerateOpenAPI()
		}
	}

	router.Get(jsonPath, func(c Context) error {
		doc := buildDoc()
		return c.JSON(http.StatusOK, doc)
	}).SetName("openapi.json")

	// Serve OpenAPI YAML
	router.Get(yamlPath, func(c Context) error {
		doc := buildDoc()
		yamlBytes, err := yaml.Marshal(doc)
		if err != nil {
			return NewInternalError(err, "failed to geenrate yaml")
		}
		c.SetHeader("Content-Type", "text/plain; charset=utf-8")
		return c.Send(yamlBytes)
	}).SetName("openapi.yaml")

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
      tryItCredentialsPolicy="same-origin">
    </elements-api>

  </body>
</html>`
		c.SetHeader("Content-Type", "text/html; charset=utf-8")
		return c.Send([]byte(html))
	}).SetName("openapi.docs")
}

func cloneOpenAPIRenderer(base *OpenAPIRenderer) *OpenAPIRenderer {
	if base == nil {
		return nil
	}

	cloned := &OpenAPIRenderer{
		Servers:        append([]OpenAPIServer(nil), base.Servers...),
		Security:       append([]OpenAPISecuritySchemas(nil), base.Security...),
		Title:          base.Title,
		Version:        base.Version,
		Description:    base.Description,
		TermsOfService: base.TermsOfService,
		Routes:         append([]RouteDefinition(nil), base.Routes...),
		Tags:           append([]any(nil), base.Tags...),
	}

	if base.Info != nil {
		infoCopy := *base.Info
		cloned.Info = &infoCopy
	}

	if base.Contact != nil {
		contactCopy := *base.Contact
		cloned.Contact = &contactCopy
	}

	if base.License != nil {
		licenseCopy := *base.License
		cloned.License = &licenseCopy
	}

	if len(base.Paths) > 0 {
		cloned.Paths = make(map[string]any, len(base.Paths))
		maps.Copy(cloned.Paths, base.Paths)
	}

	if len(base.Components) > 0 {
		cloned.Components = make(map[string]any, len(base.Components))
		maps.Copy(cloned.Components, base.Components)
	}

	if len(base.providers) > 0 {
		cloned.providers = cloneOpenAPIProviders(base.providers)
	}

	return cloned
}

func cloneOpenAPIProviders(providers []OpenApiMetaGenerator) []OpenApiMetaGenerator {
	copied := make([]OpenApiMetaGenerator, len(providers))
	for i, provider := range providers {
		switch p := provider.(type) {
		case *MetadataAggregator:
			copied[i] = p.Clone()
		default:
			copied[i] = provider
		}
	}
	return copied
}

func filterOpenAPISelfRoutes(routes []RouteDefinition) []RouteDefinition {
	if len(routes) == 0 {
		return routes
	}

	filtered := make([]RouteDefinition, 0, len(routes))
	for _, rt := range routes {
		switch rt.Name {
		case "openapi.json", "openapi.yaml", "openapi.docs":
			continue
		}
		filtered = append(filtered, rt)
	}
	return filtered
}

func autoCompileProviders(providers []OpenApiMetaGenerator) {
	for _, provider := range providers {
		autoCompileGenerator(provider)
	}
}

func autoCompileGenerator(generator OpenApiMetaGenerator) {
	type compiler interface {
		Compile()
	}
	if c, ok := generator.(compiler); ok {
		c.Compile()
	}
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
	o.Paths[fullPath] = pathItemMap
}

func joinPaths(parts ...string) string {
	cleanParts := make([]string, 0)
	for _, p := range parts {
		if p = strings.TrimSpace(p); p == "" || p == "/" {
			continue
		}
		trimmed := strings.Trim(p, "/")
		if trimmed == "" {
			continue
		}
		subParts := strings.Split(trimmed, "/")
		for _, part := range subParts {
			if part = strings.TrimSpace(part); part != "" {
				cleanParts = append(cleanParts, part)
			}
		}
	}
	if len(cleanParts) == 0 {
		return "/"
	}
	return normalizePathParams("/" + strings.Join(cleanParts, "/"))
}

func normalizePathParams(path string) string {
	if !strings.Contains(path, ":") {
		return path
	}

	var b strings.Builder
	b.Grow(len(path))

	for i := 0; i < len(path); {
		ch := path[i]
		if ch != ':' {
			b.WriteByte(ch)
			i++
			continue
		}

		j := i + 1
		for j < len(path) && isPathParamChar(path[j]) {
			j++
		}

		if j == i+1 {
			// Not a valid Fiber-style parameter, keep the colon as-is.
			b.WriteByte(ch)
			i++
			continue
		}

		paramName := path[i+1 : j]
		b.WriteByte('{')
		b.WriteString(paramName)
		b.WriteByte('}')

		// Skip optional marker (e.g. :id?)
		if j < len(path) && path[j] == '?' {
			j++
		}

		// Skip Fiber's inline type or regex declarations like :id<int> or :id([0-9]+)
		if j < len(path) && (path[j] == '<' || path[j] == '(') {
			closing := byte('>')
			if path[j] == '(' {
				closing = ')'
			}
			j++
			for j < len(path) && path[j] != closing {
				j++
			}
			if j < len(path) {
				j++
			}
		}

		i = j
	}

	return b.String()
}

func isPathParamChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_' ||
		ch == '-'
}

// mergeOpenAPIRenderer merges override into base, only overwriting non zero values
func mergeOpenAPIRenderer(base *OpenAPIRenderer, override OpenAPIRenderer) {
	if override.Title != "" {
		base.Title = override.Title
	}

	if override.Version != "" {
		base.Version = override.Version
	}

	if override.Description != "" {
		base.Description = override.Description
	}

	if override.TermsOfService != "" {
		base.TermsOfService = override.TermsOfService
	}

	if override.Info != nil {
		if base.Info == nil {
			base.Info = &OpenAPIInfo{}
		}
		mergeOpenAPIInfo(base.Info, *override.Info)
	}

	if override.Contact != nil {
		if base.Contact == nil {
			base.Contact = &OpenAPIFieldContact{}
		}
		mergeContact(base.Contact, *override.Contact)
	}

	if override.License != nil {
		if base.License == nil {
			base.License = &OpenAPIInfoLicense{}
		}
		mergeLicense(base.License, *override.License)
	}

	if len(override.Servers) > 0 {
		base.Servers = append(base.Servers, override.Servers...)
	}

	if len(override.Security) > 0 {
		base.Security = append(base.Security, override.Security...)
	}

	if len(override.Routes) > 0 {
		base.Routes = append(base.Routes, override.Routes...)
	}

	if len(override.Tags) > 0 {
		base.Tags = append(base.Tags, override.Tags...)
	}

	if len(override.providers) > 0 {
		base.providers = append(base.providers, override.providers...)
	}

	if len(override.Paths) > 0 {
		if base.Paths == nil {
			base.Paths = make(map[string]any)
		}
		maps.Copy(base.Paths, override.Paths)
	}

	if len(override.Components) > 0 {
		if base.Components == nil {
			base.Components = make(map[string]any)
		}
		maps.Copy(base.Components, override.Components)
	}
}

// mergeOpenAPIInfo merges override into base Info
func mergeOpenAPIInfo(base *OpenAPIInfo, override OpenAPIInfo) {
	if override.Title != "" {
		base.Title = override.Title
	}
	if override.Version != "" {
		base.Version = override.Version
	}
	if override.Description != "" {
		base.Description = override.Description
	}
	if override.TermsOfService != "" {
		base.TermsOfService = override.TermsOfService
	}

	if !isEmptyContact(override.Contact) {
		base.Contact = override.Contact
	}

	if !isEmptyLicense(override.License) {
		base.License = override.License
	}
}

// mergeContact merges override into base Contact
func mergeContact(base *OpenAPIFieldContact, override OpenAPIFieldContact) {
	if override.Name != "" {
		base.Name = override.Name
	}
	if override.Email != "" {
		base.Email = override.Email
	}
	if override.URL != "" {
		base.URL = override.URL
	}
}

func mergeLicense(base *OpenAPIInfoLicense, override OpenAPIInfoLicense) {
	if override.Name != "" {
		base.Name = override.Name
	}
	if override.Url != "" {
		base.Url = override.Url
	}
}

func isEmptyContact(contact OpenAPIFieldContact) bool {
	return contact.Name == "" && contact.Email == "" && contact.URL == ""
}

func isEmptyLicense(license OpenAPIInfoLicense) bool {
	return license.Name == "" && license.Url == ""
}
