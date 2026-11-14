# TDD Plan: Enhanced Metadata Options for Schema Extraction

## Overview

This document outlines the Test-Driven Development (TDD) plan for enhancing the `ExtractSchemaFromType` function with additional metadata options and improved configurability. The goal is to provide rich metadata about Go struct fields while maintaining backward compatibility.

## Current State Analysis

### Existing Implementation
- **File**: `meta_extract.go`
- **Function**: `ExtractSchemaFromType(t reflect.Type, opts ...ExtractSchemaFromTypeOptions) SchemaMetadata`
- **Current Options**:
  - `GetTableName func(t reflect.Type) string`
  - `ToSnakeCasePlural func(s string) string`
  - `ToSingular func(s string) string`

### Current PropertyInfo Structure
```go
type PropertyInfo struct {
    Type        string                 `json:"type,omitempty"`
    Format      string                 `json:"format,omitempty"`
    Items       *PropertyInfo          `json:"items,omitempty"`
    Required    bool                   `json:"required,omitempty"`
    Nullable    bool                   `json:"nullable,omitempty"`
    ReadOnly    bool                   `json:"readOnly,omitempty"`
    WriteOnly   bool                   `json:"writeOnly,omitempty"`
    Description string                 `json:"description,omitempty"`
    OriginalName string                `json:"originalName,omitempty"`
}
```

### Current Hardcoded Behaviors
1. **Field Filtering**: Unexported fields are always skipped
2. **Anonymous Fields**: Always skipped (except for BaseModel detection)
3. **Tag Processing**: Fixed priority (json, bun, crud)
4. **Field Names**: Always use JSON tag or Go field name

## Proposed Enhancements

### 1. Enhanced Options Structure
```go
type ExtractSchemaFromTypeOptions struct {
    // Existing options
    GetTableName      func(t reflect.Type) string
    ToSnakeCasePlural func(s string) string
    ToSingular        func(s string) string

    // New metadata options
    IncludeOriginalNames bool                    // Include original Go field names
    IncludeOriginalTypes bool                    // Include original Go types
    IncludeTagMetadata   bool                    // Include all struct tags
    IncludeTypeMetadata  bool                    // Include type hierarchy info

    // Tag processing options
    CustomTagHandlers map[string]func(tag string) any  // Handle custom tags
    TagPriority       []string                                  // Order of tag precedence

    // Field filtering and transformation
    SkipUnexportedFields   bool                               // Currently hardcoded to true
    SkipAnonymousFields    bool                               // Currently hardcoded to true
    CustomFieldFilter      func(field reflect.StructField) bool // Custom field inclusion logic
    FieldNameTransformer   func(fieldName string) string     // Custom field name transformation
    PropertyTypeMapper     func(t reflect.Type) PropertyInfo // Custom type mapping
}
```

### 2. Enhanced PropertyInfo Structure
```go
type PropertyInfo struct {
    // Existing fields
    Type        string                 `json:"type,omitempty"`
    Format      string                 `json:"format,omitempty"`
    Items       *PropertyInfo          `json:"items,omitempty"`
    Required    bool                   `json:"required,omitempty"`
    Nullable    bool                   `json:"nullable,omitempty"`
    ReadOnly    bool                   `json:"readOnly,omitempty"`
    WriteOnly   bool                   `json:"writeOnly,omitempty"`
    Description string                 `json:"description,omitempty"`
    OriginalName string                `json:"originalName,omitempty"`

    // New metadata fields
    OriginalType     string            `json:"originalType,omitempty"`     // Go type string
    OriginalKind     reflect.Kind      `json:"originalKind,omitempty"`     // Go kind
    AllTags          map[string]string `json:"allTags,omitempty"`          // All struct tags
    TransformPath    []string          `json:"transformPath,omitempty"`    // Transformation steps
    GoPackage        string            `json:"goPackage,omitempty"`        // Package path
    CustomTagData    map[string]any `json:"customTagData,omitempty"` // Custom tag handler results
}
```

## TDD Implementation Plan

### Phase 1: Enhanced PropertyInfo Structure

#### Task 1.1: Update PropertyInfo Struct
- [x] **File**: Update struct definition in existing file (likely `types.go` or `meta_extract.go`)
- [x] **Changes**: Add new metadata fields to PropertyInfo
- [x] **Test**: Create test verifying all fields are properly serialized to JSON

#### Task 1.2: Backward Compatibility Test
- [x] **Test**: Ensure existing code continues to work unchanged
- [x] **Verification**: All current tests pass without modification
- [x] **Edge Cases**: Empty/nil values in new fields don't break JSON serialization

### Phase 2: Enhanced Options Structure

#### Task 2.1: Extend ExtractSchemaFromTypeOptions
- [x] **File**: `meta_extract.go`
- [x] **Changes**: Add new option fields with proper default values
- [x] **Test**: Create tests for each new option individually

#### Task 2.2: Default Values and Fallbacks
- [x] **Implementation**: Define sensible defaults for all new options
- [x] **Test**: Verify that empty options struct behaves identically to current implementation
- [x] **Default Values**:
  - `IncludeOriginalNames: false`
  - `IncludeOriginalTypes: false`
  - `IncludeTagMetadata: false`
  - `IncludeTypeMetadata: false`
  - `SkipUnexportedFields: true`
  - `SkipAnonymousFields: true`
  - `TagPriority: []string{"json", "bun", "crud"}`

### Phase 3: Metadata Collection Implementation

#### Task 3.1: Original Names and Types
- [x] **Test Cases**:
  ```go
  type TestStruct struct {
      UserID   int    `json:"user_id"`
      UserName string `json:"userName"`
  }
  ```
- [x] **Expected Results**:
  - `OriginalName: "UserID"` for `user_id` property
  - `OriginalType: "int"` for `user_id` property
  - `OriginalKind: reflect.Int` for `user_id` property

#### Task 3.2: Tag Metadata Collection
- [x] **Test Cases**:
  ```go
  type TestStruct struct {
      ID int `json:"id" bun:"id,pk" validate:"required" custom:"value"`
  }
  ```
- [x] **Expected Results**:
  - `AllTags: {"json": "id", "bun": "id,pk", "validate": "required", "custom": "value"}`

#### Task 3.3: Type Hierarchy Metadata
- [x] **Test Cases**:
  ```go
  type TestStruct struct {
      Data   *[]map[string]*CustomType `json:"data"`
      Simple int                       `json:"simple"`
  }
  ```
- [x] **Expected Results**:
  - `TransformPath: ["pointer", "slice", "map", "pointer", "CustomType"]` for complex type
  - `GoPackage: "current/package/path"` for CustomType

### Phase 4: Custom Processing Implementation

#### Task 4.1: Custom Tag Handlers
- [x] **Test Cases**:
  ```go
  opts := ExtractSchemaFromTypeOptions{
      CustomTagHandlers: map[string]func(tag string) any{
          "validate": parseValidationRules,
          "custom":   parseCustomMetadata,
      },
  }
  ```
- [x] **Implementation**: Process custom tags and store results in `CustomTagData`
- [x] **Test**: Verify custom handler results are included in PropertyInfo

#### Task 4.2: Tag Priority Processing
- [x] **Test Cases**:
  ```go
  type TestStruct struct {
      Field string `xml:"xmlName" json:"jsonName" yaml:"yamlName"`
  }
  opts := ExtractSchemaFromTypeOptions{
      TagPriority: []string{"yaml", "xml", "json"},
  }
  ```
- [x] **Expected**: Field name should be "yamlName"
- [x] **Test**: Verify tag priority affects field naming

#### Task 4.3: Custom Field Filtering
- [x] **Test Cases**:
  ```go
  opts := ExtractSchemaFromTypeOptions{
      SkipUnexportedFields: false,  // Include unexported fields
      CustomFieldFilter: func(field reflect.StructField) bool {
          return !strings.HasPrefix(field.Name, "internal")
      },
  }
  ```
- [x] **Test**: Verify custom filtering logic is applied correctly

#### Task 4.4: Field Name Transformation
- [x] **Test Cases**:
  ```go
  opts := ExtractSchemaFromTypeOptions{
      FieldNameTransformer: func(fieldName string) string {
          return "prefix_" + strings.ToLower(fieldName)
      },
  }
  ```
- [x] **Test**: Verify field names are transformed according to custom logic

#### Task 4.5: Custom Property Type Mapping
- [x] **Test Cases**:
  ```go
  opts := ExtractSchemaFromTypeOptions{
      PropertyTypeMapper: func(t reflect.Type) PropertyInfo {
          if t == reflect.TypeOf(CustomType{}) {
              return PropertyInfo{Type: "custom", Format: "special"}
          }
          return PropertyInfo{} // Use default handling
      },
  }
  ```
- [x] **Test**: Verify custom type mapping overrides default behavior

### Phase 5: Integration and Performance

#### Task 5.1: Integration Tests
- [x] **Complex Structs**: Test with deeply nested structs, multiple relationships
- [x] **Real-world Models**: Test with actual project structs
- [x] **Performance**: Ensure new features don't significantly impact performance

#### Task 5.2: Documentation and Examples
- [x] **Usage Examples**: Create comprehensive examples for each new feature
- [x] **Migration Guide**: Document how to use new features
- [x] **Performance Notes**: Document performance implications

## Test Data Structures

### Basic Test Struct
```go
type BasicTestStruct struct {
    ID        int       `json:"id" bun:"id,pk" validate:"required"`
    Name      string    `json:"name" bun:"name,notnull" validate:"min=1"`
    Email     *string   `json:"email,omitempty" bun:"email" validate:"email"`
    CreatedAt time.Time `json:"created_at" bun:"created_at"`
    internal  string    // unexported field
}
```

### Complex Test Struct
```go
type ComplexTestStruct struct {
    bun.BaseModel `bun:"table:complex_items"`

    ID       int                    `json:"id" bun:"id,pk"`
    Data     *[]map[string]*CustomType `json:"data"`
    Related  RelatedStruct          `json:"related" bun:"rel:belongs-to"`
    Items    []ItemStruct           `json:"items" bun:"rel:has-many"`
    Tags     []TagStruct            `json:"tags" bun:"m2m:item_tags"`
    Metadata map[string]any `json:"metadata"`
}
```

## Expected Test Results Examples

### Basic Metadata Example
```json
{
    "name": "BasicTestStruct",
    "properties": {
        "id": {
            "type": "integer",
            "format": "int32",
            "required": true,
            "originalName": "ID",
            "originalType": "int",
            "originalKind": 2,
            "allTags": {
                "json": "id",
                "bun": "id,pk",
                "validate": "required"
            },
            "customTagData": {
                "validate": {"required": true}
            }
        }
    }
}
```

### Complex Type Metadata Example
```json
{
    "data": {
        "type": "array",
        "items": {
            "type": "object",
            "additionalProperties": {
                "type": "object"
            }
        },
        "originalName": "Data",
        "originalType": "*[]map[string]*CustomType",
        "originalKind": 22,
        "transformPath": ["pointer", "slice", "map", "pointer", "CustomType"],
        "goPackage": "github.com/goliatone/go-router"
    }
}
```

## Implementation Order

1. **Phase 1**: Update structures (low risk, foundation)
2. **Phase 2**: Add new options with defaults (backward compatible)
3. **Phase 3**: Implement basic metadata collection (core functionality)
4. **Phase 4**: Add custom processing features (advanced functionality)
5. **Phase 5**: Integration testing and optimization

## Backward Compatibility Guarantees

- All existing function signatures remain unchanged
- Default behavior is identical to current implementation
- All current tests continue to pass
- New fields in JSON output are optional and don't break existing consumers
- Performance regression is minimal (< 10% for default usage)

## Success Criteria

1. All existing tests pass without modification
2. New features have comprehensive test coverage (>95%)
3. Documentation includes clear examples for all new features
4. Performance impact is acceptable
5. Integration with existing codebase is seamless
6. All new functionality is optional and configurable
