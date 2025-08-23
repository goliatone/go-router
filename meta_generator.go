package router

import (
	"reflect"
	"strings"
	"unicode"

	"github.com/ettle/strcase"
	"github.com/gertd/go-pluralize"
)

const (
	TAG_CRUD         = "crud"
	TAG_BUN          = "bun"
	TAG_JSON         = "json"
	TAG_KEY_RESOURCE = "resource"
)

var pluralizer = pluralize.NewClient()

func GetResourceMetadata(typ reflect.Type) *ResourceMetadata {
	resourceName, pluralName := GetResourceName(typ)

	metadata := ResourceMetadata{
		Name:       resourceName,
		PluralName: pluralName,
		Tags:       []string{resourceName},
		Schema:     ExtractSchemaFromType(typ),
		Routes:     buildRoutesMetadata(typ),
	}

	return &metadata
}

// GetResourceName returns the singular and plural resource names for type T.
// It first checks for a 'crud:"resource:..."' tag on any embedded fields.
// If found, it uses the specified resource name. Otherwise, it derives the name from the type's name.
func GetResourceName(typ reflect.Type) (string, string) {
	// If T is a pointer, get the element type
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	resourceName := ""
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		crudTag := field.Tag.Get(TAG_CRUD)
		if crudTag == "" {
			continue
		}

		// Parse the crud tag, expecting format 'resource:user'
		parts := strings.SplitN(crudTag, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if key == TAG_KEY_RESOURCE && value != "" {
			resourceName = value
			break
		}
	}

	if resourceName == "" {
		// No 'crud' tag found, derive from type name
		typeName := typ.Name()
		resourceName = ToKebabCase(typeName)
	}

	singular := pluralizer.Singular(resourceName)
	plural := pluralizer.Plural(resourceName)

	return singular, plural
}

func GetResourceTitle(typ reflect.Type) (string, string) {
	resourceName, pluralName := GetResourceName(typ)
	name := strcase.ToCase(resourceName, strcase.TitleCase, ' ')
	names := strcase.ToCase(pluralName, strcase.TitleCase, ' ')
	return name, names
}

func buildRoutesMetadata(typ reflect.Type) []RouteDefinition {
	resourceName, resourcePluralName := GetResourceName(typ)
	resourceLabel, resourcePluralLabel := GetResourceTitle(typ)

	// Common error response schema
	errorResponseSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"success": map[string]any{"type": "boolean"},
			"error":   map[string]any{"type": "string"},
		},
	}

	return []RouteDefinition{
		{
			Method:  "GET",
			Path:    "/" + resourcePluralName,
			Name:    resourceName + ":list",
			Summary: "List " + resourcePluralLabel,
			Tags:    []string{resourceLabel},
			Parameters: []Parameter{
				{
					Name:     "limit",
					In:       "query",
					Required: false,
					Schema:   map[string]any{"type": "integer", "default": 25},
				},
				{
					Name:     "offset",
					In:       "query",
					Required: false,
					Schema:   map[string]any{"type": "integer", "default": 0},
				},
				{
					Name:        "include",
					In:          "query",
					Required:    false,
					Description: "Related resources to include, comma separated (e.g. Company,Profile)",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "select",
					In:          "query",
					Required:    false,
					Description: "Fields to include in the response, comma separated (e.g. id,name,email)",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "order",
					In:          "query",
					Required:    false,
					Description: "Sort order, comma separated with direction (e.g. name asc,created_at desc)",
					Schema:      map[string]any{"type": "string"},
				},
				// Field filtering parameters with operators
				{
					Name:        "{field}",
					In:          "query",
					Required:    false,
					Description: "Filter by field value (e.g. name=John)",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "{field}__eq",
					In:          "query",
					Required:    false,
					Description: "Filter where field equals value",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "{field}__ne",
					In:          "query",
					Required:    false,
					Description: "Filter where field does not equal value",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "{field}__gt",
					In:          "query",
					Required:    false,
					Description: "Filter where field is greater than value",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "{field}__lt",
					In:          "query",
					Required:    false,
					Description: "Filter where field is less than value",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "{field}__gte",
					In:          "query",
					Required:    false,
					Description: "Filter where field is greater than or equal to value",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "{field}__lte",
					In:          "query",
					Required:    false,
					Description: "Filter where field is less than or equal to value",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "{field}__like",
					In:          "query",
					Required:    false,
					Description: "Filter where field matches pattern (SQL LIKE)",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "{field}__ilike",
					In:          "query",
					Required:    false,
					Description: "Filter where field matches pattern case insensitive (SQL ILIKE)",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "{field}__and",
					In:          "query",
					Required:    false,
					Description: "Filter where field matches all values (comma separated)",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "{field}__or",
					In:          "query",
					Required:    false,
					Description: "Filter where field matches any value (comma separated)",
					Schema:      map[string]any{"type": "string"},
				},
			},
			Responses: []Response{
				{
					Code:        200,
					Description: "Successful response",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"type": "array",
								"items": map[string]any{
									"$ref": "#/components/schemas/" + resourceName,
								},
							},
						},
					},
				},
			},
		},
		// Get single resource by ID
		{
			Method:      "GET",
			Path:        "/" + resourceName + "/:id",
			Name:        resourceName + ":read",
			Summary:     "Get " + resourceLabel + " by ID",
			Tags:        []string{resourceLabel},
			Description: "Retrieves a single " + resourceLabel + " by its ID",
			Parameters: []Parameter{
				{
					Name:        "id",
					In:          "path",
					Required:    true,
					Description: "ID of the " + resourceLabel,
					Schema:      map[string]any{"type": "string", "format": "uuid"},
				},
				{
					Name:        "include",
					In:          "query",
					Required:    false,
					Description: "Related resources to include, comma separated",
					Schema:      map[string]any{"type": "string"},
				},
				{
					Name:        "select",
					In:          "query",
					Required:    false,
					Description: "Fields to include in the response, comma separated",
					Schema:      map[string]any{"type": "string"},
				},
			},
			Responses: []Response{
				{
					Code:        200,
					Description: "Successful response",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"$ref": "#/components/schemas/" + resourceName,
							},
						},
					},
				},
				{
					Code:        404,
					Description: resourceLabel + " not found",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": errorResponseSchema,
						},
					},
				},
			},
		},
		// Create single resource
		{
			Method:      "POST",
			Path:        "/" + resourceName,
			Name:        resourceName + ":create",
			Summary:     "Create new " + resourceLabel,
			Tags:        []string{resourceLabel},
			Description: "Creates a new " + resourceLabel + " record",
			RequestBody: &RequestBody{
				Description: "New " + resourceLabel + " data",
				Required:    true,
				Content: map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{
							"$ref": "#/components/schemas/" + resourceName,
						},
					},
				},
			},
			Responses: []Response{
				{
					Code:        201,
					Description: resourceLabel + " created successfully",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"$ref": "#/components/schemas/" + resourceName,
							},
						},
					},
				},
				{
					Code:        400,
					Description: "Invalid input",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": errorResponseSchema,
						},
					},
				},
			},
		},
		// Create batch
		{
			Method:      "POST",
			Path:        "/" + resourceName + "/batch",
			Name:        resourceLabel + ":create:batch",
			Summary:     "Create multiple " + resourcePluralLabel,
			Tags:        []string{resourceLabel},
			Description: "Creates multiple " + resourceLabel + " records in a single request",
			RequestBody: &RequestBody{
				Description: "Array of new " + resourceLabel + " data",
				Required:    true,
				Content: map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{
							"type": "array",
							"items": map[string]any{
								"$ref": "#/components/schemas/" + resourceName,
							},
						},
					},
				},
			},
			Responses: []Response{
				{
					Code:        201,
					Description: "Records created successfully",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"type": "array",
								"items": map[string]any{
									"$ref": "#/components/schemas/" + resourceName,
								},
							},
						},
					},
				},
				{
					Code:        400,
					Description: "Invalid input",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": errorResponseSchema,
						},
					},
				},
			},
		},
		// Update single resource
		{
			Method:      "PUT",
			Path:        "/" + resourceName + "/:id",
			Name:        resourceName + ":update",
			Summary:     "Update " + resourceLabel + " by ID",
			Tags:        []string{resourceLabel},
			Description: "Updates an existing " + resourceLabel + " record",
			Parameters: []Parameter{
				{
					Name:        "id",
					In:          "path",
					Required:    true,
					Description: "ID of the " + resourceLabel + " to update",
					Schema:      map[string]any{"type": "string", "format": "uuid"},
				},
			},
			RequestBody: &RequestBody{
				Description: "Updated " + resourceLabel + " data",
				Required:    true,
				Content: map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{
							"$ref": "#/components/schemas/" + resourceName,
						},
					},
				},
			},
			Responses: []Response{
				{
					Code:        200,
					Description: resourceLabel + " updated successfully",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"$ref": "#/components/schemas/" + resourceName,
							},
						},
					},
				},
				{
					Code:        404,
					Description: resourceLabel + " not found",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": errorResponseSchema,
						},
					},
				},
			},
		},
		// Update batch
		{
			Method:      "PUT",
			Path:        "/" + resourceName + "/batch",
			Name:        resourceName + ":update:batch",
			Summary:     "Update multiple " + resourcePluralLabel,
			Tags:        []string{resourceLabel},
			Description: "Updates multiple " + resourceLabel + " records in a single request",
			RequestBody: &RequestBody{
				Description: "Array of " + resourceLabel + " updates",
				Required:    true,
				Content: map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{
							"type": "array",
							"items": map[string]any{
								"$ref": "#/components/schemas/" + resourceName,
							},
						},
					},
				},
			},
			Responses: []Response{
				{
					Code:        200,
					Description: "Records updated successfully",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{
								"type": "array",
								"items": map[string]any{
									"$ref": "#/components/schemas/" + resourceName,
								},
							},
						},
					},
				},
				{
					Code:        400,
					Description: "Invalid input",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": errorResponseSchema,
						},
					},
				},
			},
		},
		// Delete single resource
		{
			Method:      "DELETE",
			Path:        "/" + resourceName + "/:id",
			Name:        resourceName + ":delete",
			Summary:     "Delete " + resourceLabel + " by ID",
			Tags:        []string{resourceLabel},
			Description: "Deletes a " + resourceLabel + " record",
			Parameters: []Parameter{
				{
					Name:        "id",
					In:          "path",
					Required:    true,
					Description: "ID of the " + resourceLabel + " to delete",
					Schema:      map[string]any{"type": "string", "format": "uuid"},
				},
			},
			Responses: []Response{
				{
					Code:        204,
					Description: resourceLabel + " deleted successfully",
				},
				{
					Code:        404,
					Description: resourceLabel + " not found",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": errorResponseSchema,
						},
					},
				},
			},
		},
		// Delete batch
		{
			Method:      "DELETE",
			Path:        "/" + resourceName + "/batch",
			Name:        resourceName + ":delete:batch",
			Summary:     "Delete multiple " + resourcePluralLabel,
			Tags:        []string{resourceLabel},
			Description: "Deletes multiple " + resourceLabel + " records in a single request",
			RequestBody: &RequestBody{
				Description: "Array of " + resourceLabel + " IDs to delete",
				Required:    true,
				Content: map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{
							"type": "array",
							"items": map[string]any{
								"$ref": "#/components/schemas/" + resourceName,
							},
						},
					},
				},
			},
			Responses: []Response{
				{
					Code:        204,
					Description: "Records deleted successfully",
				},
				{
					Code:        400,
					Description: "Invalid input",
					Content: map[string]any{
						"application/json": map[string]any{
							"schema": errorResponseSchema,
						},
					},
				},
			},
		},
	}
}

func ToKebabCase(s string) string {
	runes := []rune(s)
	var result []rune

	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				if unicode.IsLower(prev) || (unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
					result = append(result, '-')
				}
			}
			result = append(result, unicode.ToLower(r))
		} else {
			result = append(result, r)
		}
	}

	return string(result)
}
