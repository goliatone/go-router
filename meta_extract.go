package router

import (
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ExtractSchemaFromTypeOptions struct {
	GetTableName      func(t reflect.Type) string
	ToSnakeCasePlural func(s string) string
	ToSingular        func(s string) string

	IncludeOriginalNames bool // Include original Go field names
	IncludeOriginalTypes bool // Include original Go types
	IncludeTagMetadata   bool // Include all struct tags
	IncludeTypeMetadata  bool // Include type hierarchy info

	CustomTagHandlers map[string]func(tag string) any // Handle custom tags
	TagPriority       []string                        // Order of tag precedence

	SkipUnexportedFields *bool                                // Use pointer to distinguish between false and not set
	SkipAnonymousFields  *bool                                // Use pointer to distinguish between false and not set
	CustomFieldFilter    func(field reflect.StructField) bool // Custom field inclusion logic
	FieldNameTransformer func(fieldName string) string        // Custom field name transformation
	PropertyTypeMapper   func(t reflect.Type) PropertyInfo    // Custom type mapping
}

// ExtractSchemaFromType generates SchemaMetadata from a Go type using reflection
func ExtractSchemaFromType(t reflect.Type, opts ...ExtractSchemaFromTypeOptions) SchemaMetadata {

	// Set up defaults
	skipUnexportedFields := true
	skipAnonymousFields := true

	opt := ExtractSchemaFromTypeOptions{
		GetTableName:      getTableName,
		ToSnakeCasePlural: toSnakeCasePlural,
		ToSingular:        toSingular,

		IncludeOriginalNames: false,
		IncludeOriginalTypes: false,
		IncludeTagMetadata:   false,
		IncludeTypeMetadata:  false,
		SkipUnexportedFields: &skipUnexportedFields,
		SkipAnonymousFields:  &skipAnonymousFields,
		TagPriority:          []string{"json", "bun", "crud"},
	}

	if len(opts) > 0 {
		provided := opts[0]

		if provided.GetTableName != nil {
			opt.GetTableName = provided.GetTableName
		}

		if provided.ToSnakeCasePlural != nil {
			opt.ToSnakeCasePlural = provided.ToSnakeCasePlural
		}

		if provided.ToSingular != nil {
			opt.ToSingular = provided.ToSingular
		}

		opt.IncludeOriginalNames = provided.IncludeOriginalNames
		opt.IncludeOriginalTypes = provided.IncludeOriginalTypes
		opt.IncludeTagMetadata = provided.IncludeTagMetadata
		opt.IncludeTypeMetadata = provided.IncludeTypeMetadata

		if provided.CustomTagHandlers != nil {
			opt.CustomTagHandlers = provided.CustomTagHandlers
		}
		if provided.TagPriority != nil {
			opt.TagPriority = provided.TagPriority
		}

		// Only override boolean options if explicitly provided (non-nil pointers)
		if provided.SkipUnexportedFields != nil {
			opt.SkipUnexportedFields = provided.SkipUnexportedFields
		}
		if provided.SkipAnonymousFields != nil {
			opt.SkipAnonymousFields = provided.SkipAnonymousFields
		}
		if provided.CustomFieldFilter != nil {
			opt.CustomFieldFilter = provided.CustomFieldFilter
		}
		if provided.FieldNameTransformer != nil {
			opt.FieldNameTransformer = provided.FieldNameTransformer
		}
		if provided.PropertyTypeMapper != nil {
			opt.PropertyTypeMapper = provided.PropertyTypeMapper
		}
	}

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	required := make([]string, 0)
	properties := make(map[string]PropertyInfo)
	relationships := make(map[string]RelationshipInfo)

	for i := range t.NumField() {
		field := t.Field(i)

		if *opt.SkipUnexportedFields && !field.IsExported() {
			continue
		}

		if *opt.SkipAnonymousFields && field.Anonymous {
			// e.g. bun.BaseModel
			// TODO: extract the table name
			continue
		}

		if opt.CustomFieldFilter != nil && !opt.CustomFieldFilter(field) {
			continue
		}

		// check crud tag first, if it's "-" skip the field
		if crudTag := field.Tag.Get(TAG_CRUD); crudTag == "-" {
			continue
		}

		// get field name using tag priority
		fieldName := getFieldNameFromTags(field, opt.TagPriority)
		if fieldName == "-" {
			continue
		}
		if fieldName == "" {
			fieldName = field.Name
		}

		// apply field name transformation if provided
		if opt.FieldNameTransformer != nil {
			fieldName = opt.FieldNameTransformer(fieldName)
		}

		// get bun tags with additional metadata
		bunTag := field.Tag.Get(TAG_BUN)
		isRequired := strings.Contains(bunTag, "notnull")

		if idx := strings.Index(bunTag, "m2m:"); idx != -1 {
			pivotStr := bunTag[idx+len("m2m:"):]
			pname, remain := splitByComa(pivotStr)

			// Get source and target table names
			sourceTable := opt.GetTableName(t)          // Current struct type
			targetTable := opt.GetTableName(field.Type) // Related struct type

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
				relInfo.SourcePivotColumn = opt.ToSingular(sourceTable) + "_id"
				relInfo.TargetPivotColumn = opt.ToSingular(targetTable) + "_id"
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

			relationships[fieldName] = relInfo
			continue
		}

		if strings.Contains(bunTag, "rel:") {
			// Get source and target table names
			sourceTable := opt.GetTableName(t)          // Current struct type
			targetTable := opt.GetTableName(field.Type) // Related struct type

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
				relInfo.JoinKey = sourceCol
				relInfo.PrimaryKey = targetCol

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
					relInfo.SourceColumn = opt.ToSingular(targetTable) + "_id"
					relInfo.TargetColumn = "id"
				case "has-one", "has-many":
					// Default: target has "{source_singular}_id" pointing to source's "id"
					relInfo.SourceColumn = "id"
					relInfo.TargetColumn = opt.ToSingular(sourceTable) + "_id"
				}
				relInfo.JoinKey = relInfo.SourceColumn
				relInfo.PrimaryKey = relInfo.TargetColumn
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

			relationships[fieldName] = relInfo

			// relationship wont appear in properties
			//TODO: decide if this is what we want
			continue
		}

		var prop PropertyInfo

		// Use custom property type mapper if provided
		if opt.PropertyTypeMapper != nil {
			customProp := opt.PropertyTypeMapper(field.Type)
			// If custom mapper returns a non-empty PropertyInfo, use it as base
			if customProp.Type != "" {
				prop = customProp
			} else {
				// Fall back to default behavior if custom mapper returns empty PropertyInfo
				prop = extractPropertyInfoWithPath(field.Type, nil, opt.IncludeTypeMetadata)
			}
		} else {
			prop = extractPropertyInfoWithPath(field.Type, nil, opt.IncludeTypeMetadata)
		}

		// add additional metadata (preserve custom mapper settings where possible)
		if prop.Description == "" {
			prop.Description = field.Tag.Get("description")
		}

		// Set other metadata only if not already set by custom mapper
		if !prop.Required {
			prop.Required = isRequired
		}
		if !prop.Nullable && field.Type.Kind() == reflect.Ptr {
			prop.Nullable = true
		}
		if !prop.ReadOnly {
			prop.ReadOnly = isReadOnly(field)
		}
		if !prop.WriteOnly {
			prop.WriteOnly = isWriteOnly(field)
		}
		if prop.OriginalName == "" {
			prop.OriginalName = field.Name
		}

		// Add original type metadata if enabled
		if opt.IncludeOriginalTypes {
			prop.OriginalType = field.Type.String()
			prop.OriginalKind = field.Type.Kind()
		}

		// Add tag metadata if enabled
		if opt.IncludeTagMetadata {
			prop.AllTags = make(map[string]string)
			// Collect all struct tags
			// Common tag names we want to capture
			tagNames := []string{"json", "bun", "crud", "validate", "custom", "xml", "yaml", "form", "query", "param", "header", "db", "gorm", "binding", "description"}

			for _, tagName := range tagNames {
				if tagValue := field.Tag.Get(tagName); tagValue != "" {
					prop.AllTags[tagName] = tagValue
				}
			}

			// Also capture any other tags by parsing the full tag string
			// This ensures we don't miss any custom tags
			fullTag := string(field.Tag)
			if fullTag != "" {
				parseAllTags(fullTag, prop.AllTags)
			}
		}

		// Process custom tag handlers if provided
		if len(opt.CustomTagHandlers) > 0 {
			prop.CustomTagData = make(map[string]any)

			for tagName, handler := range opt.CustomTagHandlers {
				if tagValue := field.Tag.Get(tagName); tagValue != "" {
					// Call the custom handler for this tag
					result := handler(tagValue)
					// Only store non-nil results
					if result != nil {
						prop.CustomTagData[tagName] = result
					}
				}
			}

			// If no custom tag data was collected, set to nil to avoid empty maps in JSON
			if len(prop.CustomTagData) == 0 {
				prop.CustomTagData = nil
			}
		}

		// Check for omitempty in the actual tag that was used for field naming
		actualTag := getActualTagValue(field, opt.TagPriority)
		if strings.Contains(actualTag, "omitempty") {
			prop.Required = false
		}

		// Add to required slice only after final determination
		if prop.Required {
			required = append(required, fieldName)
		}

		properties[fieldName] = prop
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
	return extractPropertyInfoWithPath(t, nil, false)
}

// extractPropertyInfoWithPath extracts property info and tracks transformation path
func extractPropertyInfoWithPath(t reflect.Type, transformPath []string, includeTypeMetadata bool) PropertyInfo {
	// Build the complete transformation path for the entire type
	if includeTypeMetadata {
		fullPath := buildTransformPath(t)
		return extractPropertyInfoWithTransformPath(t, fullPath, includeTypeMetadata)
	}
	return extractPropertyInfoWithTransformPath(t, nil, includeTypeMetadata)
}

// buildTransformPath builds the complete transformation path for a type
func buildTransformPath(t reflect.Type) []string {
	var path []string

	for {
		switch t.Kind() {
		case reflect.Ptr:
			path = append(path, "pointer")
			t = t.Elem()
		case reflect.Slice, reflect.Array:
			path = append(path, "slice")
			t = t.Elem()
		case reflect.Map:
			path = append(path, "map")
			t = t.Elem() // For maps, continue with the value type
		case reflect.Struct:
			if t.Name() != "" {
				path = append(path, t.Name())
			}
			return path
		default:
			// For basic types (int, string, etc.), we don't add them to the path
			return path
		}
	}
}

// extractPropertyInfoWithTransformPath does the actual property extraction with a pre-built path
func extractPropertyInfoWithTransformPath(t reflect.Type, transformPath []string, includeTypeMetadata bool) PropertyInfo {
	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		return extractPropertyInfoWithTransformPath(t.Elem(), transformPath, includeTypeMetadata)
	}

	// Handle special types first
	if specialProp, ok := handleSpecialType(t); ok {
		if includeTypeMetadata {
			specialProp.TransformPath = transformPath
			if t.PkgPath() != "" {
				specialProp.GoPackage = t.PkgPath()
			}
		}
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
		// NOTE: We don't want to recursively extract properties here
		// instead, these should be handled as separate schema components

		if includeTypeMetadata {
			prop.TransformPath = transformPath
			// Add package info for custom types
			if t.PkgPath() != "" {
				prop.GoPackage = t.PkgPath()
			}
		}

	case reflect.Slice, reflect.Array:
		prop.Type = "array"
		prop.Items = &PropertyInfo{}
		*prop.Items = extractPropertyInfoWithTransformPath(t.Elem(), transformPath, includeTypeMetadata)

		if includeTypeMetadata {
			prop.TransformPath = transformPath
		}

	case reflect.Map:
		prop.Type = "object"
		// TODO: for maps, we could potentially add additionalProperties schema

		if includeTypeMetadata {
			prop.TransformPath = transformPath
		}
	}

	// Set transform path and package info for basic types
	if includeTypeMetadata {
		if len(prop.TransformPath) == 0 {
			prop.TransformPath = transformPath
		}

		// Set package info for the type if it has one and not already set
		if t.PkgPath() != "" && prop.GoPackage == "" {
			prop.GoPackage = t.PkgPath()
		}
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

// parseAllTags parses the full struct tag string to extract all tag key-value pairs
func parseAllTags(tagStr string, result map[string]string) {
	// Simple tag parsing - look for key:"value" patterns
	for len(tagStr) > 0 {
		// Skip whitespace
		for len(tagStr) > 0 && tagStr[0] == ' ' {
			tagStr = tagStr[1:]
		}
		if len(tagStr) == 0 {
			break
		}

		// Find the key (everything before the colon)
		keyEnd := strings.Index(tagStr, ":")
		if keyEnd == -1 {
			break
		}
		key := tagStr[:keyEnd]
		tagStr = tagStr[keyEnd+1:]

		// Skip any whitespace and expect a quote
		for len(tagStr) > 0 && tagStr[0] == ' ' {
			tagStr = tagStr[1:]
		}
		if len(tagStr) == 0 || tagStr[0] != '"' {
			break
		}

		// Find the value (quoted string)
		valueStart := 1 // Skip the opening quote
		valueEnd := valueStart
		for valueEnd < len(tagStr) && tagStr[valueEnd] != '"' {
			if tagStr[valueEnd] == '\\' && valueEnd+1 < len(tagStr) {
				valueEnd += 2 // Skip escaped character
			} else {
				valueEnd++
			}
		}
		if valueEnd >= len(tagStr) {
			break
		}

		value := tagStr[valueStart:valueEnd]
		tagStr = tagStr[valueEnd+1:] // Skip the closing quote

		// Only add if not already present (the explicit tag names take priority)
		if _, exists := result[key]; !exists {
			result[key] = value
		}
	}
}

// getFieldNameFromTags gets the field name using tag priority order
func getFieldNameFromTags(field reflect.StructField, tagPriority []string) string {
	for _, tagName := range tagPriority {
		if tagValue := field.Tag.Get(tagName); tagValue != "" {
			// Extract the field name part (before any comma-separated options)
			fieldName := strings.Split(tagValue, ",")[0]
			return fieldName
		}
	}
	return ""
}

// getActualTagValue gets the actual tag value that was used for field naming
func getActualTagValue(field reflect.StructField, tagPriority []string) string {
	for _, tagName := range tagPriority {
		if tagValue := field.Tag.Get(tagName); tagValue != "" {
			return tagValue
		}
	}
	return ""
}
