package router

import (
	"strconv"
	"strings"
)

// ResourceMetadata represents collected metadata about an API resource
type ResourceMetadata struct {
	// Resource identifiers
	Name        string   `json:"name"`
	PluralName  string   `json:"plural_name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`

	// Routes metadata
	Routes []RouteMetadata `json:"routes"`

	// Schema information
	Schema SchemaMetadata `json:"schema"`
}

type RouteMetadata struct {
	Method      HTTPMethod       `json:"method"`
	Path        string           `json:"path"`
	Name        string           `json:"name"`
	Summary     string           `json:"summary"`
	Description string           `json:"description"`
	Tags        []string         `json:"tags"`
	Parameters  []ParameterInfo  `json:"parameters"`
	RequestBody *RequestBodyInfo `json:"request_body,omitempty"`
	Responses   []ResponseInfo   `json:"responses"`
}

type ParameterInfo struct {
	Name        string         `json:"name"`
	In          string         `json:"in"` // query, path, header, cookie
	Required    bool           `json:"required"`
	Description string         `json:"description"`
	Schema      map[string]any `json:"schema"`
	Example     any            `json:"example,omitempty"`
}

type RequestBodyInfo struct {
	Description string         `json:"description"`
	Required    bool           `json:"required"`
	Content     map[string]any `json:"content"`
}

type ResponseInfo struct {
	Code        int            `json:"code"`
	Description string         `json:"description"`
	Headers     map[string]any `json:"headers,omitempty"`
	Content     map[string]any `json:"content"`
}

type SchemaMetadata struct {
	Properties  map[string]PropertyInfo `json:"properties"`
	Required    []string                `json:"required"`
	Description string                  `json:"description"`
}

type PropertyInfo struct {
	Type        string                  `json:"type"`
	Format      string                  `json:"format,omitempty"`
	Description string                  `json:"description,omitempty"`
	Required    bool                    `json:"required"`
	Nullable    bool                    `json:"nullable"`
	ReadOnly    bool                    `json:"read_only"`
	WriteOnly   bool                    `json:"write_only"`
	Example     any                     `json:"example,omitempty"`
	Properties  map[string]PropertyInfo `json:"properties,omitempty"` // For nested objects
	Items       *PropertyInfo           `json:"items,omitempty"`      // For arrays
}

// MetadataProvider interface for components that can provide API metadata
type MetadataProvider interface {
	GetMetadata() ResourceMetadata
}

// MetadataAggregator collects and merges metadata from multiple providers
type MetadataAggregator struct {
	providers  []MetadataProvider
	globalTags []string

	////
	Paths      map[string]any
	Schemas    map[string]any
	Tags       []any
	Components map[string]any
}

// NewMetadataAggregator creates a new aggregator
func NewMetadataAggregator() *MetadataAggregator {
	return &MetadataAggregator{
		providers: make([]MetadataProvider, 0),
	}
}

// AddProvider adds a metadata provider to the aggregator
func (ma *MetadataAggregator) AddProvider(provider MetadataProvider) {
	ma.providers = append(ma.providers, provider)
}

// AddProviders adds multiple metadata providers to the aggregator
func (ma *MetadataAggregator) AddProviders(providers ...MetadataProvider) {
	ma.providers = append(ma.providers, providers...)
}

// SetTags sets global tags that will be added to all operations
func (ma *MetadataAggregator) SetTags(tags []string) {
	ma.globalTags = tags
}

// GenerateOpenAPI generates a complete OpenAPI specification from all providers
func (ma *MetadataAggregator) Compile() {
	paths := make(map[string]any)
	schemas := make(map[string]any)
	tags := make(map[string]any)

	for _, provider := range ma.providers {
		metadata := provider.GetMetadata()

		schemas[metadata.Name] = convertSchemaToOpenAPI(metadata.Schema)

		for _, route := range metadata.Routes {
			pathItem := convertRouteToPathItem(route)

			if existingPath, exists := paths[route.Path]; exists {
				existing := existingPath.(map[string]any)
				for k, v := range pathItem {
					existing[k] = v
				}
			} else {
				paths[route.Path] = pathItem
			}
		}

		for _, tag := range metadata.Tags {
			if _, exists := tags[tag]; !exists {
				tags[tag] = map[string]any{
					"name": tag,
					// TODO: get description from somewhere
				}
			}
		}
	}

	for _, tag := range ma.globalTags {
		if _, exists := tags[tag]; !exists {
			tags[tag] = map[string]any{
				"name": tag,
			}
		}
	}

	ma.Paths = paths
	ma.Schemas = schemas
	ma.Tags = convertMapToArray(tags)
	ma.Components = map[string]any{
		"schemas": schemas,
	}
}

func (ma *MetadataAggregator) GenerateOpenAPI() map[string]any {
	return map[string]any{
		"openapi":    "3.0.3",
		"paths":      ma.Paths,
		"components": ma.Components,
		"tags":       ma.Tags,
	}
}

func convertMapToArray(m map[string]any) []any {
	result := make([]any, 0, len(m))
	for _, v := range m {
		result = append(result, v)
	}
	return result
}

func convertRouteToPathItem(route RouteMetadata) map[string]any {
	operation := map[string]any{
		"summary":     route.Summary,
		"description": route.Description,
		"tags":        route.Tags,
		"parameters":  convertParameters(route.Parameters),
		"responses":   convertResponses(route.Responses),
	}

	if route.RequestBody != nil {
		operation["requestBody"] = convertRequestBody(route.RequestBody)
	}

	return map[string]any{
		strings.ToLower(string(route.Method)): operation,
	}
}

func convertSchemaToOpenAPI(schema SchemaMetadata) map[string]any {
	return map[string]any{
		"type":        "object",
		"properties":  convertProperties(schema.Properties),
		"required":    schema.Required,
		"description": schema.Description,
	}
}

func convertParameters(params []ParameterInfo) []map[string]any {
	result := make([]map[string]any, len(params))
	for i, p := range params {
		param := map[string]any{
			"name":        p.Name,
			"in":          p.In,
			"required":    p.Required,
			"description": p.Description,
			"schema":      p.Schema,
		}
		if p.Example != nil {
			param["example"] = p.Example
		}
		result[i] = param
	}
	return result
}

func convertRequestBody(rb *RequestBodyInfo) map[string]any {
	if rb == nil {
		return nil
	}
	return map[string]any{
		"description": rb.Description,
		"required":    rb.Required,
		"content":     rb.Content,
	}
}

func convertResponses(responses []ResponseInfo) map[string]any {
	result := make(map[string]any)
	for _, r := range responses {
		response := map[string]any{
			"description": r.Description,
		}
		if len(r.Headers) > 0 {
			response["headers"] = r.Headers
		}
		if len(r.Content) > 0 {
			response["content"] = r.Content
		}
		result[strconv.Itoa(r.Code)] = response
	}
	return result
}

func convertProperties(props map[string]PropertyInfo) map[string]any {
	result := make(map[string]any)
	for name, prop := range props {
		property := map[string]any{
			"type": prop.Type,
		}

		if prop.Format != "" {
			property["format"] = prop.Format
		}
		if prop.Description != "" {
			property["description"] = prop.Description
		}
		if prop.ReadOnly {
			property["readOnly"] = true
		}
		if prop.WriteOnly {
			property["writeOnly"] = true
		}
		if prop.Example != nil {
			property["example"] = prop.Example
		}
		if prop.Nullable {
			property["nullable"] = true
		}

		if len(prop.Properties) > 0 {
			property["properties"] = convertProperties(prop.Properties)
		}

		if prop.Items != nil {
			items := make(map[string]any)
			if prop.Items.Type != "" {
				items["type"] = prop.Items.Type
			}
			if prop.Items.Format != "" {
				items["format"] = prop.Items.Format
			}
			property["items"] = items
		}

		result[name] = property
	}
	return result
}
