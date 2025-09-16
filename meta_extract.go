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

			// Get source and target table names
			sourceTable := getTableName(t)          // Current struct type
			targetTable := getTableName(field.Type) // Related struct type

			relInfo := RelationshipInfo{
				RelationType:    "many-to-many",
				RelatedTypeName: getBaseTypeName(field.Type),
				IsSlice:         (field.Type.Kind() == reflect.Slice),
				PivotTable:      pname,
				SourceTable:     sourceTable,
				TargetTable:     targetTable,
			}

			if strings.HasPrefix(remain, "join:") {
				joinClause := remain[len("join:"):]
				relInfo.JoinClause = joinClause
				relInfo.PivotJoin = joinClause

				// Parse pivot columns from join clause
				sourceCol, targetCol := parseM2MJoinClause(joinClause)
				relInfo.SourcePivotColumn = sourceCol
				relInfo.TargetPivotColumn = targetCol
			} else {
				// Default M2M column names when no explicit join clause
				relInfo.SourcePivotColumn = toSingular(sourceTable) + "_id"
				relInfo.TargetPivotColumn = toSingular(targetTable) + "_id"
			}

			// Check for supplemental crud tag overrides
			if crudTag := field.Tag.Get(TAG_CRUD); crudTag != "" && crudTag != "-" {
				parts := strings.Split(crudTag, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if strings.HasPrefix(part, "source_table=") {
						relInfo.SourceTable = strings.TrimPrefix(part, "source_table=")
					} else if strings.HasPrefix(part, "target_table=") {
						relInfo.TargetTable = strings.TrimPrefix(part, "target_table=")
					} else if strings.HasPrefix(part, "source_pivot_column=") {
						relInfo.SourcePivotColumn = strings.TrimPrefix(part, "source_pivot_column=")
					} else if strings.HasPrefix(part, "target_pivot_column=") {
						relInfo.TargetPivotColumn = strings.TrimPrefix(part, "target_pivot_column=")
					}
				}
			}

			relationships[jsonName] = relInfo
			continue
		}

		if strings.Contains(bunTag, "rel:") {
			// Get source and target table names
			sourceTable := getTableName(t)          // Current struct type
			targetTable := getTableName(field.Type) // Related struct type

			relInfo := RelationshipInfo{
				SourceTable: sourceTable,
				TargetTable: targetTable,
			}

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

				sourceCol, targetCol := parseJoinClause(joinPart)
				relInfo.JoinKey = sourceCol    // Keep for backward compatibility
				relInfo.PrimaryKey = targetCol // Keep for backward compatibility

				// Populate new fields based on relationship type
				switch relInfo.RelationType {
				case "belongs-to":
					// For belongs-to: source has FK pointing to target's PK
					relInfo.SourceColumn = sourceCol
					relInfo.TargetColumn = targetCol
				case "has-one", "has-many":
					// For has-one/has-many: source's PK is referenced by target's FK
					relInfo.SourceColumn = sourceCol // Source PK
					relInfo.TargetColumn = targetCol // Target FK
				}
			} else {
				// Default column names when no explicit join clause
				switch relInfo.RelationType {
				case "belongs-to":
					// Default: source has "{target_singular}_id" pointing to target's "id"
					relInfo.SourceColumn = toSingular(targetTable) + "_id"
					relInfo.TargetColumn = "id"
				case "has-one", "has-many":
					// Default: target has "{source_singular}_id" pointing to source's "id"
					relInfo.SourceColumn = "id"
					relInfo.TargetColumn = toSingular(sourceTable) + "_id"
				}
				relInfo.JoinKey = relInfo.SourceColumn    // For backward compatibility
				relInfo.PrimaryKey = relInfo.TargetColumn // For backward compatibility
			}

			relInfo.RelatedTypeName = getBaseTypeName(field.Type)

			// Check for supplemental crud tag overrides
			if crudTag := field.Tag.Get(TAG_CRUD); crudTag != "" && crudTag != "-" {
				parts := strings.Split(crudTag, ",")
				for _, part := range parts {
					part = strings.TrimSpace(part)
					if strings.HasPrefix(part, "source_table=") {
						relInfo.SourceTable = strings.TrimPrefix(part, "source_table=")
					} else if strings.HasPrefix(part, "target_table=") {
						relInfo.TargetTable = strings.TrimPrefix(part, "target_table=")
					} else if strings.HasPrefix(part, "source_column=") {
						relInfo.SourceColumn = strings.TrimPrefix(part, "source_column=")
					} else if strings.HasPrefix(part, "target_column=") {
						relInfo.TargetColumn = strings.TrimPrefix(part, "target_column=")
					} else if strings.HasPrefix(part, "source_pivot_column=") {
						relInfo.SourcePivotColumn = strings.TrimPrefix(part, "source_pivot_column=")
					} else if strings.HasPrefix(part, "target_pivot_column=") {
						relInfo.TargetPivotColumn = strings.TrimPrefix(part, "target_pivot_column=")
					}
				}
			}

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

// getTableName derives table name from struct type following Bun conventions
func getTableName(t reflect.Type) string {
	// Handle pointer and slice types
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		t = t.Elem()
	}

	// Check for bun.BaseModel embedded struct with table tag
	if t.Kind() == reflect.Struct {
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if field.Anonymous && field.Type.Name() == "BaseModel" {
				if tableTag := field.Tag.Get("bun"); tableTag != "" {
					parts := strings.Split(tableTag, ",")
					for _, part := range parts {
						if strings.HasPrefix(part, "table:") {
							return strings.TrimPrefix(part, "table:")
						}
					}
				}
			}
		}
	}

	// Fall back to snake_case plural of struct name
	return toSnakeCasePlural(t.Name())
}

// toSnakeCasePlural converts CamelCase to snake_case and pluralizes
func toSnakeCasePlural(s string) string {
	// Convert CamelCase to snake_case
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	snakeCase := strings.ToLower(result.String())

	// Simple pluralization rules
	if strings.HasSuffix(snakeCase, "y") && len(snakeCase) > 1 {
		// Check if the character before 'y' is a consonant
		prevChar := snakeCase[len(snakeCase)-2]
		if prevChar != 'a' && prevChar != 'e' && prevChar != 'i' && prevChar != 'o' && prevChar != 'u' {
			return snakeCase[:len(snakeCase)-1] + "ies"
		}
	}
	if strings.HasSuffix(snakeCase, "s") || strings.HasSuffix(snakeCase, "x") || strings.HasSuffix(snakeCase, "z") {
		return snakeCase + "es"
	}
	return snakeCase + "s"
}

// parseJoinClause parses join clauses like "user_id=id" or "id=order_id"
func parseJoinClause(joinClause string) (sourceCol, targetCol string) {
	parts := strings.Split(joinClause, "=")
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", ""
}

// parseM2MJoinClause parses M2M join clauses like "order=id,item=item_id"
func parseM2MJoinClause(joinClause string) (sourceCol, targetCol string) {
	// Handle formats like "order=id,item=item_id" or just "order_id,item_id"
	parts := strings.Split(joinClause, ",")
	if len(parts) >= 2 {
		// Parse first part (source)
		if strings.Contains(parts[0], "=") {
			sourceParts := strings.Split(parts[0], "=")
			if len(sourceParts) == 2 {
				sourceCol = strings.TrimSpace(sourceParts[1])
			}
		} else {
			sourceCol = strings.TrimSpace(parts[0])
		}

		// Parse second part (target)
		if strings.Contains(parts[1], "=") {
			targetParts := strings.Split(parts[1], "=")
			if len(targetParts) == 2 {
				targetCol = strings.TrimSpace(targetParts[1])
			}
		} else {
			targetCol = strings.TrimSpace(parts[1])
		}
	}
	return sourceCol, targetCol
}

// toSingular converts plural table names back to singular for column naming
func toSingular(s string) string {
	// Simple singularization rules (reverse of pluralization)
	if strings.HasSuffix(s, "ies") && len(s) > 3 {
		return s[:len(s)-3] + "y"
	}
	if strings.HasSuffix(s, "es") && len(s) > 2 {
		// Check if it's a simple "s" addition or genuine "es" addition
		withoutEs := s[:len(s)-2]
		if strings.HasSuffix(withoutEs, "s") || strings.HasSuffix(withoutEs, "x") || strings.HasSuffix(withoutEs, "z") {
			return withoutEs
		}
		// Fall back to removing just the "s"
		return s[:len(s)-1]
	}
	if strings.HasSuffix(s, "s") && len(s) > 1 {
		return s[:len(s)-1]
	}
	return s
}
