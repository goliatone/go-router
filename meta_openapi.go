package router

import (
	"fmt"
	"reflect"
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
	Routes []RouteDefinition `json:"routes"`

	// Schema information
	Schema SchemaMetadata `json:"schema"`
}

// RouteDefinition represents all metadata about a route,
// combining both runtime routing information and
// metadata
type RouteDefinition struct {
	// Core routing
	Method   HTTPMethod     `json:"method"`
	Path     string         `json:"path"`
	Name     string         `json:"name"`
	Handlers []NamedHandler `json:"-"` // Runtime only, not exported to JSON

	// metadata e.g. OpenAPI
	Summary     string       `json:"summary,omitempty"`
	Description string       `json:"description,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
	Parameters  []Parameter  `json:"parameters,omitempty"`
	RequestBody *RequestBody `json:"request_body,omitempty"`
	Responses   []Response   `json:"responses,omitempty"`
	Security    []string     `json:"security,omitempty"`
	onSetName   func(n string)
}

// Parameter unifies the parameter definitions
type Parameter struct {
	Name        string         `json:"name"`
	In          string         `json:"in"` // query, path, header, cookie
	Required    bool           `json:"required"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
	Example     any            `json:"example,omitempty"`
}

// RequestBody unifies the request body definitions
type RequestBody struct {
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required"`
	Content     map[string]any `json:"content,omitempty"`
}

// Response unifies the response definitions
type Response struct {
	Code        int            `json:"code"`
	Description string         `json:"description"`
	Headers     map[string]any `json:"headers,omitempty"`
	Content     map[string]any `json:"content,omitempty"`
}

type SchemaMetadata struct {
	Required      []string                    `json:"required"`
	Name          string                      `json:"entity_name"`
	Description   string                      `json:"description"`
	Properties    map[string]PropertyInfo     `json:"properties"`
	Relationships map[string]RelationshipInfo `json:"relationships,omitempty"`
}

type PropertyInfo struct {
	Type          string                  `json:"type"`
	Format        string                  `json:"format,omitempty"`
	Description   string                  `json:"description,omitempty"`
	Required      bool                    `json:"required"`
	Nullable      bool                    `json:"nullable"`
	ReadOnly      bool                    `json:"read_only"`
	WriteOnly     bool                    `json:"write_only"`
	OriginalName  string                  `json:"original_name"`
	Example       any                     `json:"example,omitempty"`
	Properties    map[string]PropertyInfo `json:"properties,omitempty"`    // For nested objects
	Items         *PropertyInfo           `json:"items,omitempty"`         // For arrays
	OriginalType  string                  `json:"originalType,omitempty"`  // Go type string
	OriginalKind  reflect.Kind            `json:"originalKind,omitempty"`  // Go kind
	AllTags       map[string]string       `json:"allTags,omitempty"`       // All struct tags
	TransformPath []string                `json:"transformPath,omitempty"` // Transformation steps
	GoPackage     string                  `json:"goPackage,omitempty"`     // Package path
	CustomTagData map[string]any          `json:"customTagData,omitempty"` // Custom tag handler results
}

type RelationshipInfo struct {
	RelationType      string `json:"relation_type"` // e.g. has-one, has-many, belongs-to, many-to-many
	RelatedTypeName   string `json:"related_type_name"`
	IsSlice           bool   `json:"is_slice"`
	JoinClause        string `json:"join_clause,omitempty"`
	JoinKey           string `json:"join_key,omitempty"`
	PrimaryKey        string `json:"primary_key,omitempty"`
	ForeignKey        string `json:"foreign_key,omitempty"`
	PivotTable        string `json:"pivot_table,omitempty"`         // e.g. "order_to_items"
	PivotJoin         string `json:"pivot_join,omitempty"`          // e.g. "Order=Item"
	SourceTable       string `json:"source_table,omitempty"`        // entity owning the relationship field
	SourceColumn      string `json:"source_column,omitempty"`       // FK column on the source table
	TargetTable       string `json:"target_table,omitempty"`        // referenced entity/table
	TargetColumn      string `json:"target_column,omitempty"`       // PK column on the target table
	SourcePivotColumn string `json:"source_pivot_column,omitempty"` // for M2M: column linking to source table
	TargetPivotColumn string `json:"target_pivot_column,omitempty"` // for M2M: column linking to target table
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
	info       *OpenAPIInfo
	Paths      map[string]any
	Schemas    map[string]any
	Tags       []any
	Components map[string]any
}

// NewMetadataAggregator creates a new aggregator
func NewMetadataAggregator() *MetadataAggregator {
	return &MetadataAggregator{
		providers: make([]MetadataProvider, 0),
		info: &OpenAPIInfo{
			Title:   "API Documentation",
			Version: "1.0.0",
		},
	}
}

// Clone creates a shallow copy of the aggregator with shared providers.
func (ma *MetadataAggregator) Clone() *MetadataAggregator {
	if ma == nil {
		return nil
	}

	cloned := &MetadataAggregator{
		providers:  append([]MetadataProvider(nil), ma.providers...),
		globalTags: append([]string(nil), ma.globalTags...),
	}

	if ma.info != nil {
		infoCopy := *ma.info
		cloned.info = &infoCopy
	}
	return cloned
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

// SetInfo configures the top-level OpenAPI info object.
func (ma *MetadataAggregator) SetInfo(info OpenAPIInfo) {
	ma.info = &info
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
			normalizedPath := joinPaths(route.Path)

			if existingPath, exists := paths[normalizedPath]; exists {
				existing := existingPath.(map[string]any)
				for k, v := range pathItem {
					existing[k] = v
				}
			} else {
				paths[normalizedPath] = pathItem
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
	info := ma.info
	if info == nil {
		info = &OpenAPIInfo{
			Title:   "API Documentation",
			Version: "1.0.0",
		}
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":          either(info.Title, "API Documentation"),
			"version":        either(info.Version, "1.0.0"),
			"description":    info.Description,
			"termsOfService": info.TermsOfService,
		},
		"paths":      ma.Paths,
		"tags":       ma.Tags,
		"components": ma.Components,
	}
}

func convertMapToArray(m map[string]any) []any {
	result := make([]any, 0, len(m))
	for _, v := range m {
		result = append(result, v)
	}
	return result
}

func convertRouteToPathItem(route RouteDefinition) map[string]any {
	operation := map[string]any{
		"summary":     route.Summary,
		"description": route.Description,
		"operationId": strings.ToLower(fmt.Sprintf("%s-%s", route.Method, route.Name)),
		"tags":        route.Tags,
		"parameters":  convertParameters(route.Parameters),
		"responses":   convertResponses(route.Responses),
		// "security":    route.Security(),
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

func convertParameters(params []Parameter) []map[string]any {
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

func convertRequestBody(rb *RequestBody) map[string]any {
	if rb == nil {
		return nil
	}

	return map[string]any{
		"description": rb.Description,
		"required":    rb.Required,
		"content":     rb.Content,
	}
}

func convertResponses(responses []Response) map[string]any {
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
