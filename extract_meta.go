package router

import (
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ExtractSchemaFromType generates SchemaMetadata from a Go type using reflection
func ExtractSchemaFromType(t reflect.Type) SchemaMetadata {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	properties := make(map[string]PropertyInfo)
	required := make([]string, 0)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// skip unexported fields
		if !field.IsExported() {
			continue
		}

		// check crud tag first, if it's "-" skip the field
		if crudTag := field.Tag.Get("crud"); crudTag == "-" {
			continue
		}

		// embedded fields
		if field.Anonymous {
			// e.g. bun.BaseModel
			continue
		}

		// get JSON field name
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}
		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName == "" {
			jsonName = field.Name
		}

		// get bun tags with additional metadata
		bunTag := field.Tag.Get("bun")
		isRequired := strings.Contains(bunTag, "notnull")
		if isRequired {
			required = append(required, jsonName)
		}

		prop := extractPropertyInfo(field.Type)

		// add additional metadata
		prop.Description = field.Tag.Get("description")
		prop.Required = isRequired
		prop.Nullable = field.Type.Kind() == reflect.Ptr
		prop.ReadOnly = isReadOnly(field)
		prop.WriteOnly = isWriteOnly(field)

		if strings.Contains(jsonTag, "omitempty") {
			prop.Required = false
		}

		properties[jsonName] = prop
	}

	return SchemaMetadata{
		Properties:  properties,
		Required:    required,
		Description: "Schema for " + t.Name(),
	}
}

// extractPropertyInfo extracts OpenAPI property information from a type
func extractPropertyInfo(t reflect.Type) PropertyInfo {
	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		return extractPropertyInfo(t.Elem())
	}

	// Handle special types first
	if specialProp, ok := handleSpecialType(t); ok {
		return specialProp
	}

	prop := PropertyInfo{}

	switch t.Kind() {
	case reflect.Bool:
		prop.Type = "boolean"

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		prop.Type = "integer"
		prop.Format = "int32"

	case reflect.Int64:
		prop.Type = "integer"
		prop.Format = "int64"

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		prop.Type = "integer"
		prop.Format = "int32"

	case reflect.Uint64:
		prop.Type = "integer"
		prop.Format = "int64"

	case reflect.Float32:
		prop.Type = "number"
		prop.Format = "float"

	case reflect.Float64:
		prop.Type = "number"
		prop.Format = "double"

	case reflect.String:
		prop.Type = "string"

	case reflect.Struct:
		// For structs, we only set the type and reference
		prop.Type = "object"
		// NOTE: We don't wnat to recursively extract properties here
		// instead, these should be handled as separate schema components

	case reflect.Slice, reflect.Array:
		prop.Type = "array"
		prop.Items = &PropertyInfo{}
		*prop.Items = extractPropertyInfo(t.Elem())

	case reflect.Map:
		prop.Type = "object"
		// TODO: for maps, we could potentially add additionalProperties schema
	}

	return prop
}

// handleSpecialType handles special Go types that need specific OpenAPI formats
func handleSpecialType(t reflect.Type) (PropertyInfo, bool) {
	switch t {
	case reflect.TypeOf(time.Time{}):
		return PropertyInfo{
			Type:   "string",
			Format: "date-time",
		}, true

	case reflect.TypeOf(uuid.UUID{}):
		return PropertyInfo{
			Type:   "string",
			Format: "uuid",
		}, true

	// TODO: add more special types as needed
	default:
		return PropertyInfo{}, false
	}
}

func isReadOnly(field reflect.StructField) bool {
	return strings.Contains(field.Tag.Get("crud"), "readonly")
}

func isWriteOnly(field reflect.StructField) bool {
	return strings.Contains(field.Tag.Get("crud"), "writeonly")
}
