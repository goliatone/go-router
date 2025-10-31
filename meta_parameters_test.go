package router_test

import (
	"reflect"
	"testing"

	"github.com/goliatone/go-router"
)

type parameterProviderFunc func() router.ResourceMetadata

func (fn parameterProviderFunc) GetMetadata() router.ResourceMetadata {
	return fn()
}

func TestMetadataAggregatorIncludesSharedParameters(t *testing.T) {
	type Book struct {
		ID    int    `json:"id"`
		Title string `json:"title"`
	}

	metadata := router.GetResourceMetadata(reflect.TypeOf(Book{}))
	if len(metadata.Parameters) == 0 {
		t.Fatalf("expected shared parameters to be populated, got none")
	}

	aggregator := router.NewMetadataAggregator()
	aggregator.AddProvider(parameterProviderFunc(func() router.ResourceMetadata {
		return *metadata
	}))
	aggregator.Compile()

	components := aggregator.Components
	paramsRaw, ok := components["parameters"]
	if !ok {
		t.Fatalf("expected components to include shared parameters, components: %+v", components)
	}

	parameters, ok := paramsRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected parameters component to be a map, got %T", paramsRaw)
	}

	expected := map[string]struct {
		description string
		defaultVal  any
	}{
		"Limit": {
			description: "Maximum number of records to return (default 25)",
			defaultVal:  25,
		},
		"Offset": {
			description: "Number of records to skip before starting to return results (default 0)",
			defaultVal:  0,
		},
		"Include": {
			description: "Related resources to include, comma separated (e.g. Company,Profile)",
		},
		"Select": {
			description: "Fields to include in the response, comma separated (e.g. id,name,email)",
		},
		"Order": {
			description: "Sort order, comma separated with direction (e.g. name asc,created_at desc)",
		},
	}

	for key, expectation := range expected {
		value, exists := parameters[key]
		if !exists {
			t.Fatalf("expected parameter component %q to be present", key)
		}

		paramMap, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("expected parameter component %q to be a map, got %T", key, value)
		}

		if desc := paramMap["description"]; desc != expectation.description {
			t.Fatalf("parameter %q description mismatch: got %v", key, desc)
		}

		if expectation.defaultVal != nil {
			schema, ok := paramMap["schema"].(map[string]any)
			if !ok {
				t.Fatalf("expected parameter %q to include schema map, got %T", key, paramMap["schema"])
			}
			if schema["default"] != expectation.defaultVal {
				t.Fatalf("parameter %q default mismatch: expected %v, got %v", key, expectation.defaultVal, schema["default"])
			}
		}
	}

	pathKey := "/" + metadata.PluralName
	pathItem, ok := aggregator.Paths[pathKey]
	if !ok {
		t.Fatalf("expected path %q to exist in aggregated paths", pathKey)
	}

	pathMap, ok := pathItem.(map[string]any)
	if !ok {
		t.Fatalf("expected path item to be a map, got %T", pathItem)
	}

	getOperation, ok := pathMap["get"].(map[string]any)
	if !ok {
		t.Fatalf("expected GET operation to exist for %q", pathKey)
	}

	rawParams, ok := getOperation["parameters"]
	if !ok {
		t.Fatalf("expected GET operation to include parameters")
	}

	params, ok := rawParams.([]map[string]any)
	if !ok {
		t.Fatalf("expected GET parameters to be []map[string]any, got %T", rawParams)
	}

	expectedRefs := []string{
		"#/components/parameters/Limit",
		"#/components/parameters/Offset",
		"#/components/parameters/Include",
		"#/components/parameters/Select",
		"#/components/parameters/Order",
	}

	if len(params) < len(expectedRefs) {
		t.Fatalf("expected at least %d parameters, got %d", len(expectedRefs), len(params))
	}

	for i, ref := range expectedRefs {
		if params[i]["$ref"] != ref {
			t.Fatalf("expected parameter %d to reference %s, got %v", i, ref, params[i])
		}
	}
}
