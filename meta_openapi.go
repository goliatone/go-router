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
	Name         string       `json:"name"`
	PluralName   string       `json:"plural_name"`
	Description  string       `json:"description"`
	Tags         []string     `json:"tags"`
	ResourceType reflect.Type `json:"-"`

	// Routes metadata
	Routes []RouteDefinition `json:"routes"`

	// Schema information
	Schema    SchemaMetadata      `json:"schema"`
	Relations *RelationDescriptor `json:"-"`

	// Shared parameter components (referenced from routes via $ref)
	Parameters map[string]Parameter `json:"parameters,omitempty"`
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
	Ref         string         `json:"-"`
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
	Required        []string                     `json:"required"`
	Name            string                       `json:"entity_name"`
	Description     string                       `json:"description"`
	LabelField      string                       `json:"label_field,omitempty"`
	Properties      map[string]PropertyInfo      `json:"properties"`
	Relationships   map[string]*RelationshipInfo `json:"relationships,omitempty"`
	RelationAliases map[string]string            `json:"relation_aliases,omitempty"`
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
	RelationName  string                  `json:"-"`                       // Populated for relation-backed fields
	RelatedSchema string                  `json:"-"`                       // Target schema/component name
}

type RelationshipInfo struct {
	RelationType      string           `json:"relation_type"` // e.g. has-one, has-many, belongs-to, many-to-many
	Cardinality       string           `json:"cardinality,omitempty"`
	RelatedTypeName   string           `json:"related_type_name"`
	RelatedSchema     string           `json:"related_schema,omitempty"`
	RelatedType       reflect.Type     `json:"-"`
	IsSlice           bool             `json:"is_slice"`
	JoinClause        string           `json:"join_clause,omitempty"`
	JoinKey           string           `json:"join_key,omitempty"`
	PrimaryKey        string           `json:"primary_key,omitempty"`
	ForeignKey        string           `json:"foreign_key,omitempty"`
	PivotTable        string           `json:"pivot_table,omitempty"`         // e.g. "order_to_items"
	PivotJoin         string           `json:"pivot_join,omitempty"`          // e.g. "Order=Item"
	SourceTable       string           `json:"source_table,omitempty"`        // entity owning the relationship field
	SourceColumn      string           `json:"source_column,omitempty"`       // FK column on the source table
	TargetTable       string           `json:"target_table,omitempty"`        // referenced entity/table
	TargetColumn      string           `json:"target_column,omitempty"`       // PK column on the target table
	SourcePivotColumn string           `json:"source_pivot_column,omitempty"` // for M2M: column linking to source table
	TargetPivotColumn string           `json:"target_pivot_column,omitempty"` // for M2M: column linking to target table
	SourceField       string           `json:"source_field,omitempty"`        // Scalar field holding the FK (e.g. author_id)
	Inverse           string           `json:"inverse,omitempty"`
	Endpoint          *EndpointHint    `json:"endpoint,omitempty"`
	Filters           []RelationFilter `json:"filters,omitempty"`
}

// EndpointHint provides UI-focused guidance for retrieving relation options.
type EndpointHint struct {
	URL           string            `json:"url,omitempty"`
	Method        string            `json:"method,omitempty"`
	LabelField    string            `json:"labelField,omitempty"`
	ValueField    string            `json:"valueField,omitempty"`
	Params        map[string]string `json:"params,omitempty"`
	DynamicParams map[string]string `json:"dynamicParams,omitempty"`
	Mode          string            `json:"mode,omitempty"`
	SearchParam   string            `json:"searchParam,omitempty"`
	SubmitAs      string            `json:"submitAs,omitempty"`
}

// EndpointDefaultFunc derives default endpoint hints when tags are absent.
type EndpointDefaultFunc func(resource *ResourceMetadata, relationName string, rel *RelationshipInfo) *EndpointHint

// RelationshipInfoFilter allows callers to mutate relationship metadata prior to serialization.
type RelationshipInfoFilter func(resource *ResourceMetadata, relationName string, rel *RelationshipInfo) *RelationshipInfo

// UISchemaOptions configures UI-specific enrichment hooks.
type UISchemaOptions struct {
	EndpointDefaults  EndpointDefaultFunc                 // Optional callback to derive default endpoints.
	EndpointOverrides map[string]map[string]*EndpointHint // Overrides keyed by resource -> relation.
	RelationFilters   []RelationshipInfoFilter            // Additional filters applied before serialization.
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
	info                *OpenAPIInfo
	Paths               map[string]any
	Schemas             map[string]any
	Tags                []any
	Components          map[string]any
	relationProvider    RelationMetadataProvider
	relationProviders   map[reflect.Type]RelationMetadataProvider
	RelationDescriptors map[string]*RelationDescriptor
	uiOptions           *UISchemaOptions
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
		providers:        append([]MetadataProvider(nil), ma.providers...),
		globalTags:       append([]string(nil), ma.globalTags...),
		relationProvider: ma.relationProvider,
	}

	if ma.info != nil {
		infoCopy := *ma.info
		cloned.info = &infoCopy
	}
	if len(ma.relationProviders) > 0 {
		cloned.relationProviders = make(map[reflect.Type]RelationMetadataProvider, len(ma.relationProviders))
		for t, provider := range ma.relationProviders {
			cloned.relationProviders[t] = provider
		}
	}
	if len(ma.RelationDescriptors) > 0 {
		cloned.RelationDescriptors = make(map[string]*RelationDescriptor, len(ma.RelationDescriptors))
		for name, descriptor := range ma.RelationDescriptors {
			cloned.RelationDescriptors[name] = descriptor
		}
	}
	if ma.uiOptions != nil {
		cloned.uiOptions = ma.uiOptions
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

// WithRelationProvider sets the default relation metadata provider.
func (ma *MetadataAggregator) WithRelationProvider(provider RelationMetadataProvider) *MetadataAggregator {
	ma.relationProvider = provider
	return ma
}

// WithRelationProviders registers per-resource relation metadata providers.
func (ma *MetadataAggregator) WithRelationProviders(overrides map[reflect.Type]RelationMetadataProvider) *MetadataAggregator {
	if len(overrides) == 0 {
		return ma
	}

	if ma.relationProviders == nil {
		ma.relationProviders = make(map[reflect.Type]RelationMetadataProvider, len(overrides))
	}

	for t, provider := range overrides {
		if t == nil || provider == nil {
			continue
		}
		ma.relationProviders[indirectType(t)] = provider
	}
	return ma
}

// WithUISchemaOptions registers UI schema enrichment configuration.
func (ma *MetadataAggregator) WithUISchemaOptions(opts UISchemaOptions) *MetadataAggregator {
	cloned := opts
	if len(opts.EndpointOverrides) > 0 {
		clone := make(map[string]map[string]*EndpointHint, len(opts.EndpointOverrides))
		for resource, overrides := range opts.EndpointOverrides {
			if len(overrides) == 0 {
				continue
			}
			inner := make(map[string]*EndpointHint, len(overrides))
			for relation, hint := range overrides {
				inner[relation] = cloneEndpointHint(hint)
			}
			clone[resource] = inner
		}
		cloned.EndpointOverrides = clone
	}
	if len(opts.RelationFilters) > 0 {
		cloned.RelationFilters = append([]RelationshipInfoFilter(nil), opts.RelationFilters...)
	}
	ma.uiOptions = &cloned
	return ma
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
	metadataList := make([]*ResourceMetadata, 0, len(ma.providers))
	typeToSchema := make(map[reflect.Type]string, len(ma.providers))
	typeNameToSchema := make(map[string]string, len(ma.providers))

	for _, provider := range ma.providers {
		metadata := provider.GetMetadata()
		md := metadata
		metadataPtr := &md
		metadataList = append(metadataList, metadataPtr)

		if metadata.ResourceType != nil {
			baseType := indirectType(metadata.ResourceType)
			typeToSchema[baseType] = metadata.Name
			if baseType.Name() != "" {
				typeNameToSchema[baseType.Name()] = metadata.Name
			}
		}
	}

	paths := make(map[string]any)
	schemas := make(map[string]any)
	tags := make(map[string]any)
	parameters := make(map[string]any)
	relationDescriptors := make(map[string]*RelationDescriptor)

	for _, metadata := range metadataList {
		if metadata == nil {
			continue
		}

		ma.prepareSchemaForCompilation(metadata, typeToSchema, typeNameToSchema)

		if descriptor := ma.buildRelationDescriptor(metadata); descriptor != nil {
			relationDescriptors[metadata.Name] = descriptor
		}

		schema := convertSchemaToOpenAPI(metadata.Schema)
		if metadata.Relations != nil {
			schema["x-formgen-relations"] = convertRelationDescriptor(metadata.Relations)
		}
		schemas[metadata.Name] = schema

		for name, parameter := range metadata.Parameters {
			parameters[name] = convertParameterDefinition(parameter)
		}

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
				}
			}
		}
	}

	ma.ensureRelatedSchemas(metadataList, schemas)

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
	components := map[string]any{
		"schemas": schemas,
	}
	if len(parameters) > 0 {
		components["parameters"] = parameters
	}
	ma.Components = components
	if len(relationDescriptors) > 0 {
		ma.RelationDescriptors = relationDescriptors
	} else {
		ma.RelationDescriptors = nil
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
	result := map[string]any{
		"type":        "object",
		"properties":  convertPropertiesWithRelations(schema),
		"required":    schema.Required,
		"description": schema.Description,
	}

	if schema.LabelField != "" {
		result["x-formgen-label-field"] = schema.LabelField
	}

	return result
}

func convertParameters(params []Parameter) []map[string]any {
	result := make([]map[string]any, len(params))
	for i, p := range params {
		if p.Ref != "" {
			result[i] = map[string]any{"$ref": p.Ref}
			continue
		}

		result[i] = convertParameterDefinition(p)
	}
	return result
}

func convertParameterDefinition(p Parameter) map[string]any {
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
	return param
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

func convertPropertiesWithRelations(schema SchemaMetadata) map[string]any {
	if len(schema.Properties) == 0 {
		return nil
	}

	result := make(map[string]any, len(schema.Properties))
	for name, prop := range schema.Properties {
		property := convertPropertyInfo(prop)

		relInfo, isRelationship := schema.Relationships[name]
		aliasTarget := ""
		if !isRelationship && len(schema.RelationAliases) > 0 {
			if relName, ok := schema.RelationAliases[name]; ok {
				aliasTarget = relName
				relInfo = schema.Relationships[relName]
			}
		}

		if relInfo != nil {
			applyRelationshipExtensions(property, prop, relInfo, aliasTarget != "")
		}

		result[name] = property
	}
	return result
}

func convertPropertyInfo(prop PropertyInfo) map[string]any {
	property := make(map[string]any)

	if prop.Type != "" {
		property["type"] = prop.Type
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
		nested := make(map[string]any, len(prop.Properties))
		for nestedName, nestedProp := range prop.Properties {
			nested[nestedName] = convertPropertyInfo(nestedProp)
		}
		property["properties"] = nested
	}

	if prop.Items != nil {
		property["items"] = convertPropertyInfo(*prop.Items)
	}

	return property
}

func applyRelationshipExtensions(property map[string]any, prop PropertyInfo, rel *RelationshipInfo, isAlias bool) {
	if property == nil || rel == nil {
		return
	}

	ref := ""
	if rel.RelatedSchema != "" {
		ref = "#/components/schemas/" + rel.RelatedSchema
	} else if rel.RelatedTypeName != "" {
		ref = "#/components/schemas/" + ToKebabCase(rel.RelatedTypeName)
	}

	if ref != "" && !isAlias {
		switch rel.Cardinality {
		case "many":
			property["type"] = "array"
			property["items"] = map[string]any{"$ref": ref}
			delete(property, "properties")
		case "one", "":
			if _, ok := property["type"]; !ok || property["type"] == "" {
				property["type"] = "object"
			}
			property["allOf"] = []any{map[string]any{"$ref": ref}}
			delete(property, "properties")
			delete(property, "items")
		}
	}

	includeSource := !isAlias
	if relExt := buildRelationshipExtension(rel, ref, includeSource); len(relExt) > 0 {
		property["x-relationships"] = relExt
	}

	if endpoint := convertEndpointHint(rel.Endpoint); len(endpoint) > 0 {
		property["x-endpoint"] = endpoint
	}
}

func buildRelationshipExtension(rel *RelationshipInfo, targetRef string, includeSource bool) map[string]any {
	if rel == nil {
		return nil
	}

	result := make(map[string]any)

	if rel.RelationType != "" {
		result["type"] = formatRelationshipType(rel.RelationType)
	}
	if targetRef != "" {
		result["target"] = targetRef
	}
	if rel.ForeignKey != "" {
		result["foreignKey"] = rel.ForeignKey
	}
	if rel.Cardinality != "" {
		result["cardinality"] = rel.Cardinality
	}
	if includeSource && rel.SourceField != "" {
		result["sourceField"] = rel.SourceField
	}
	if rel.Inverse != "" {
		result["inverse"] = rel.Inverse
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func convertEndpointHint(hint *EndpointHint) map[string]any {
	if hint == nil {
		return nil
	}

	result := make(map[string]any)

	if hint.URL != "" {
		result["url"] = hint.URL
	}

	method := strings.ToUpper(strings.TrimSpace(hint.Method))
	if method == "" {
		method = "GET"
	}
	result["method"] = method

	if hint.LabelField != "" {
		result["labelField"] = hint.LabelField
	}
	if hint.ValueField != "" {
		result["valueField"] = hint.ValueField
	}
	if len(hint.Params) > 0 {
		result["params"] = hint.Params
	}
	if len(hint.DynamicParams) > 0 {
		result["dynamicParams"] = hint.DynamicParams
	}
	if hint.Mode != "" {
		result["mode"] = hint.Mode
	}
	if hint.SearchParam != "" {
		result["searchParam"] = hint.SearchParam
	}
	if hint.SubmitAs != "" {
		result["submitAs"] = hint.SubmitAs
	}

	if _, hasURL := result["url"]; !hasURL && len(result) == 1 {
		return nil
	}

	return result
}

func formatRelationshipType(relType string) string {
	relType = strings.TrimSpace(relType)
	if relType == "" {
		return relType
	}

	parts := strings.FieldsFunc(relType, func(r rune) bool {
		switch r {
		case '-', '_', ' ':
			return true
		default:
			return false
		}
	})

	if len(parts) == 0 {
		return relType
	}

	for i := range parts {
		parts[i] = strings.ToLower(parts[i])
	}

	builder := strings.Builder{}
	builder.WriteString(parts[0])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			builder.WriteString(part[1:])
		}
	}
	return builder.String()
}

func (ma *MetadataAggregator) prepareSchemaForCompilation(metadata *ResourceMetadata, typeToSchema map[reflect.Type]string, typeNameToSchema map[string]string) {
	if metadata == nil {
		return
	}

	relationships := metadata.Schema.Relationships
	if len(relationships) == 0 {
		return
	}

	for relName, relInfo := range relationships {
		if relInfo == nil {
			continue
		}

		updated := ma.applyRelationshipFilters(metadata, relName, relInfo)
		if updated == nil {
			delete(relationships, relName)
			ma.removeRelationAliasEntries(metadata, relName)
			continue
		}
		relInfo = updated

		if override := ma.lookupEndpointOverride(metadata.Name, relName); override != nil {
			relInfo.Endpoint = cloneEndpointHint(override)
		} else if relInfo.Endpoint == nil && ma.uiOptions != nil && ma.uiOptions.EndpointDefaults != nil {
			if hint := ma.uiOptions.EndpointDefaults(metadata, relName, relInfo); hint != nil {
				relInfo.Endpoint = hint
			}
		}

		if relInfo.RelatedSchema == "" {
			if relInfo.RelatedType != nil {
				if schemaName, ok := typeToSchema[indirectType(relInfo.RelatedType)]; ok {
					relInfo.RelatedSchema = schemaName
				}
			}
			if relInfo.RelatedSchema == "" && relInfo.RelatedTypeName != "" {
				if schemaName, ok := typeNameToSchema[relInfo.RelatedTypeName]; ok {
					relInfo.RelatedSchema = schemaName
				}
			}
			if relInfo.RelatedSchema == "" && relInfo.RelatedTypeName != "" {
				relInfo.RelatedSchema = ToKebabCase(relInfo.RelatedTypeName)
			}
		}

		relationships[relName] = relInfo

		if prop, ok := metadata.Schema.Properties[relName]; ok {
			if relInfo.RelatedSchema != "" {
				prop.RelatedSchema = relInfo.RelatedSchema
			}
			prop.RelationName = relName
			metadata.Schema.Properties[relName] = prop
		}
	}

	if len(metadata.Schema.RelationAliases) == 0 {
		return
	}

	for propName, relName := range metadata.Schema.RelationAliases {
		relInfo, ok := relationships[relName]
		if !ok || relInfo == nil {
			delete(metadata.Schema.RelationAliases, propName)
			continue
		}

		prop, ok := metadata.Schema.Properties[propName]
		if !ok {
			continue
		}
		if relInfo.RelatedSchema != "" {
			prop.RelatedSchema = relInfo.RelatedSchema
		}
		prop.RelationName = relName
		metadata.Schema.Properties[propName] = prop
	}
}

func (ma *MetadataAggregator) applyRelationshipFilters(metadata *ResourceMetadata, relationName string, rel *RelationshipInfo) *RelationshipInfo {
	if ma == nil || ma.uiOptions == nil || len(ma.uiOptions.RelationFilters) == 0 {
		return rel
	}

	current := rel
	for _, filter := range ma.uiOptions.RelationFilters {
		if filter == nil || current == nil {
			continue
		}
		current = filter(metadata, relationName, current)
	}
	return current
}

func (ma *MetadataAggregator) lookupEndpointOverride(resourceName, relationName string) *EndpointHint {
	if ma == nil || ma.uiOptions == nil || len(ma.uiOptions.EndpointOverrides) == 0 {
		return nil
	}

	overrides, ok := ma.uiOptions.EndpointOverrides[resourceName]
	if !ok {
		return nil
	}
	return overrides[relationName]
}

func (ma *MetadataAggregator) removeRelationAliasEntries(metadata *ResourceMetadata, relationName string) {
	if metadata == nil || len(metadata.Schema.RelationAliases) == 0 {
		return
	}
	for propName, rel := range metadata.Schema.RelationAliases {
		if rel == relationName {
			delete(metadata.Schema.RelationAliases, propName)
		}
	}
}

func cloneEndpointHint(hint *EndpointHint) *EndpointHint {
	if hint == nil {
		return nil
	}
	clone := *hint
	if len(hint.Params) > 0 {
		clone.Params = make(map[string]string, len(hint.Params))
		for k, v := range hint.Params {
			clone.Params[k] = v
		}
	}
	if len(hint.DynamicParams) > 0 {
		clone.DynamicParams = make(map[string]string, len(hint.DynamicParams))
		for k, v := range hint.DynamicParams {
			clone.DynamicParams[k] = v
		}
	}
	return &clone
}

func (ma *MetadataAggregator) ensureRelatedSchemas(metadataList []*ResourceMetadata, schemas map[string]any) {
	for _, metadata := range metadataList {
		if metadata == nil {
			continue
		}

		for _, relInfo := range metadata.Schema.Relationships {
			if relInfo == nil {
				continue
			}

			schemaName := relInfo.RelatedSchema
			if schemaName == "" {
				continue
			}

			if _, exists := schemas[schemaName]; exists {
				continue
			}

			relatedType := relInfo.RelatedType
			if relatedType == nil {
				continue
			}

			baseType := indirectType(relatedType)
			if baseType == nil {
				continue
			}

			generatedSchema := convertSchemaToOpenAPI(ExtractSchemaFromType(baseType))
			schemas[schemaName] = generatedSchema
		}
	}
}

func (ma *MetadataAggregator) buildRelationDescriptor(metadata *ResourceMetadata) *RelationDescriptor {
	if metadata == nil || metadata.ResourceType == nil {
		return nil
	}

	provider := ma.selectRelationProvider(metadata.ResourceType)
	if provider == nil {
		return nil
	}

	descriptor, err := provider.BuildRelationDescriptor(metadata.ResourceType)
	if err != nil {
		return nil
	}

	descriptor = ApplyRelationFilters(metadata.ResourceType, descriptor)
	metadata.Relations = descriptor
	return descriptor
}

func (ma *MetadataAggregator) selectRelationProvider(resourceType reflect.Type) RelationMetadataProvider {
	if resourceType == nil {
		return nil
	}

	if len(ma.relationProviders) > 0 {
		if provider, ok := ma.relationProviders[indirectType(resourceType)]; ok && provider != nil {
			return provider
		}
	}
	return ma.relationProvider
}

func convertRelationDescriptor(descriptor *RelationDescriptor) map[string]any {
	if descriptor == nil {
		return nil
	}

	result := make(map[string]any)
	if len(descriptor.Includes) > 0 {
		result["includes"] = descriptor.Includes
	}
	if len(descriptor.Relations) > 0 {
		relations := make([]map[string]any, len(descriptor.Relations))
		for i, rel := range descriptor.Relations {
			entry := map[string]any{
				"name": rel.Name,
			}
			if len(rel.Filters) > 0 {
				filters := make([]map[string]any, len(rel.Filters))
				for idx, filter := range rel.Filters {
					filters[idx] = map[string]any{
						"field":    filter.Field,
						"operator": filter.Operator,
						"value":    filter.Value,
					}
				}
				entry["filters"] = filters
			}
			relations[i] = entry
		}
		result["relations"] = relations
	}
	if descriptor.Tree != nil {
		result["tree"] = convertRelationNode(descriptor.Tree)
	}
	return result
}

func convertRelationNode(node *RelationNode) map[string]any {
	if node == nil {
		return nil
	}

	result := map[string]any{
		"name": node.Name,
	}
	if node.Display != "" {
		result["display"] = node.Display
	}
	if node.TypeName != "" {
		result["typeName"] = node.TypeName
	}
	if len(node.Fields) > 0 {
		result["fields"] = node.Fields
	}
	if len(node.Aliases) > 0 {
		result["aliases"] = node.Aliases
	}
	if len(node.Operators) > 0 {
		result["operators"] = node.Operators
	}
	if len(node.Children) > 0 {
		children := make(map[string]any, len(node.Children))
		for key, child := range node.Children {
			if converted := convertRelationNode(child); converted != nil {
				children[key] = converted
			}
		}
		if len(children) > 0 {
			result["children"] = children
		}
	}
	return result
}
