package router

import (
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ExtractSchemaFromType generates SchemaMetadata from a Go type using reflection
// TODO: Need to support relationships
// TODO: take options, ie. include original $meta: original name, orignal type
func ExtractSchemaFromType(t reflect.Type) SchemaMetadata {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	required := make([]string, 0)
	properties := make(map[string]PropertyInfo)
	relationships := make(map[string]RelationshipInfo)

	for i := range t.NumField() {
		field := t.Field(i)

		// skip unexported fields
		if !field.IsExported() {
			continue
		}

		// check crud tag first, if it's "-" skip the field
		if crudTag := field.Tag.Get(TAG_CRUD); crudTag == "-" {
			continue
		}

		// embedded fields
		if field.Anonymous {
			// e.g. bun.BaseModel
			// TODO: extract the table name
			continue
		}

		// get JSON field name
		jsonTag := field.Tag.Get(TAG_JSON)
		if jsonTag == "-" {
			continue
		}
		jsonName := strings.Split(jsonTag, ",")[0]
		if jsonName == "" {
			jsonName = field.Name
		}

		// get bun tags with additional metadata
		bunTag := field.Tag.Get(TAG_BUN)
		isRequired := strings.Contains(bunTag, "notnull")

		if idx := strings.Index(bunTag, "m2m:"); idx != -1 {
			pivotStr := bunTag[idx+len("m2m:"):]
			pname, remain := splitByComa(pivotStr)
			relInfo := RelationshipInfo{
				RelationType:    "many-to-many",
				RelatedTypeName: getBaseTypeName(field.Type),
				IsSlice:         (field.Type.Kind() == reflect.Slice),
				PivotTable:      pname,
			}

			if strings.HasPrefix(remain, "join:") {
				joinClause := remain[len("join:"):]
				relInfo.JoinClause = joinClause
				relInfo.PivotJoin = joinClause
			}
			relationships[jsonName] = relInfo
			continue
		}

		if strings.Contains(bunTag, "rel:") {
			relInfo := RelationshipInfo{}
			switch {
			case strings.Contains(bunTag, "has-one"):
				relInfo.RelationType = "has-one"
			case strings.Contains(bunTag, "has-many"):
				relInfo.RelationType = "has-many"
			case strings.Contains(bunTag, "belongs-to"):
				relInfo.RelationType = "belongs-to"
			}

			if field.Type.Kind() == reflect.Slice {
				relInfo.IsSlice = true
			}

			if idx := strings.Index(bunTag, "join:"); idx != -1 {
				joinPart := extractSubAfter(bunTag, "join:")
				relInfo.JoinClause = joinPart

				parts := strings.Split(joinPart, "=")
				if len(parts) == 2 {
					relInfo.JoinKey = parts[0]
					relInfo.PrimaryKey = parts[1]
				}
			}
			relInfo.RelatedTypeName = getBaseTypeName(field.Type)

			relationships[jsonName] = relInfo

			// relationship wont appear in properties
			//TODO: decide if this is what we want
			continue
		}

		prop := extractPropertyInfo(field.Type)

		// add additional metadata
		prop.Description = field.Tag.Get("description")
		prop.Required = isRequired
		prop.Nullable = field.Type.Kind() == reflect.Ptr
		prop.ReadOnly = isReadOnly(field)
		prop.WriteOnly = isWriteOnly(field)
		prop.OriginalName = field.Name

		if strings.Contains(jsonTag, "omitempty") {
			prop.Required = false
		}

		// Add to required slice only after final determination
		if prop.Required {
			required = append(required, jsonName)
		}

		properties[jsonName] = prop
	}

	return SchemaMetadata{
		Name:          t.Name(),
		Description:   "Schema for " + t.Name(),
		Required:      required,
		Properties:    properties,
		Relationships: relationships,
	}
}

func splitByComa(s string) (before, after string) {
	if idx := strings.Index(s, ","); idx != -1 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

func extractSubAfter(s, prefix string) string {
	idx := strings.Index(s, prefix)
	if idx == -1 {
		return ""
	}
	return s[idx+len(prefix):]
}

func getBaseTypeName(t reflect.Type) string {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}
	return t.Name()
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
	return strings.Contains(field.Tag.Get(TAG_CRUD), "readonly")
}

func isWriteOnly(field reflect.StructField) bool {
	return strings.Contains(field.Tag.Get(TAG_CRUD), "writeonly")
}
