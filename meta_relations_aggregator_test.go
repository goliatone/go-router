package router

import (
	"reflect"
	"testing"
)

type stubRelationProvider struct {
	descriptor *RelationDescriptor
}

func (s stubRelationProvider) BuildRelationDescriptor(reflect.Type) (*RelationDescriptor, error) {
	return s.descriptor, nil
}

type relationStubMetadataProvider struct {
	metadata ResourceMetadata
}

func (s relationStubMetadataProvider) GetMetadata() ResourceMetadata {
	return s.metadata
}

func TestMetadataAggregatorUsesDefaultRelationProvider(t *testing.T) {
	resetRelationFilters()

	resourceType := reflect.TypeOf(relationTestAuthor{})
	descriptor := &RelationDescriptor{Includes: []string{"books"}}

	aggregator := NewMetadataAggregator().
		WithRelationProvider(stubRelationProvider{descriptor: descriptor})

	aggregator.AddProvider(relationStubMetadataProvider{
		metadata: ResourceMetadata{
			Name:         "author",
			ResourceType: resourceType,
			Schema:       SchemaMetadata{},
		},
	})

	aggregator.Compile()

	got, ok := aggregator.RelationDescriptors["author"]
	if !ok {
		t.Fatalf("expected relation descriptor for resource 'author'")
	}
	if got != descriptor {
		t.Fatalf("expected descriptor pointer to match default provider")
	}
}

func TestMetadataAggregatorPrefersOverrideProvider(t *testing.T) {
	resetRelationFilters()

	resourceType := reflect.TypeOf(relationTestAuthor{})
	defaultDescriptor := &RelationDescriptor{Includes: []string{"default"}}
	overrideDescriptor := &RelationDescriptor{Includes: []string{"override"}}

	aggregator := NewMetadataAggregator().
		WithRelationProvider(stubRelationProvider{descriptor: defaultDescriptor}).
		WithRelationProviders(map[reflect.Type]RelationMetadataProvider{
			resourceType: stubRelationProvider{descriptor: overrideDescriptor},
		})

	aggregator.AddProvider(relationStubMetadataProvider{
		metadata: ResourceMetadata{
			Name:         "author",
			ResourceType: resourceType,
			Schema:       SchemaMetadata{},
		},
	})

	aggregator.Compile()

	got := aggregator.RelationDescriptors["author"]
	if got != overrideDescriptor {
		t.Fatalf("expected override descriptor to be used, got %+v", got)
	}
}

func TestMetadataAggregatorAppliesFilters(t *testing.T) {
	resetRelationFilters()

	resourceType := reflect.TypeOf(relationTestAuthor{})
	descriptor := &RelationDescriptor{Includes: []string{"books"}}

	RegisterRelationFilter(func(_ reflect.Type, desc *RelationDescriptor) *RelationDescriptor {
		if desc == nil {
			return nil
		}
		desc.Includes = append(desc.Includes, "filtered")
		return desc
	})
	defer resetRelationFilters()

	aggregator := NewMetadataAggregator().
		WithRelationProvider(stubRelationProvider{descriptor: descriptor})

	aggregator.AddProvider(relationStubMetadataProvider{
		metadata: ResourceMetadata{
			Name:         "author",
			ResourceType: resourceType,
			Schema:       SchemaMetadata{},
		},
	})

	aggregator.Compile()

	got := aggregator.RelationDescriptors["author"]
	if got == nil {
		t.Fatalf("expected descriptor after filters")
	}
	found := false
	for _, include := range got.Includes {
		if include == "filtered" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected filter to append 'filtered' to includes, got %v", got.Includes)
	}
}
