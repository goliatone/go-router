package router

import (
	"reflect"
	"testing"
)

type relationTestAuthor struct {
	ID    int                     `bun:"id,pk"`
	Name  string                  `json:"name"`
	Books []relationTestBook      `bun:"rel:has-many,join:id=book.author_id" json:"books"`
	Meta  *relationTestAuthorMeta `bun:"rel:has-one,join:id=meta.author_id"`
}

type relationTestBook struct {
	ID          int                    `bun:"id,pk"`
	AuthorID    int                    `bun:"author_id"`
	Title       string                 `json:"title"`
	Publisher   *relationTestPublisher `bun:"rel:belongs-to,join:publisher_id=id" json:"publisher"`
	contributor string                 `bun:"rel:belongs-to,join:contributor_id=id"`
}

type relationTestPublisher struct {
	ID   int    `bun:"id,pk"`
	Name string `json:"name"`
}

type relationTestAuthorMeta struct {
	ID       int    `bun:"id,pk"`
	AuthorID int    `bun:"author_id"`
	Notes    string `json:"notes"`
}

func TestDefaultRelationProvider_BuildRelationDescriptor(t *testing.T) {
	resetRelationFilters()

	provider := NewDefaultRelationProvider()
	descriptor, err := provider.BuildRelationDescriptor(reflect.TypeOf(relationTestAuthor{}))
	if err != nil {
		t.Fatalf("unexpected error building relation descriptor: %v", err)
	}

	if descriptor == nil || descriptor.Tree == nil {
		t.Fatalf("expected non-nil descriptor and tree")
	}

	if descriptor.Tree.Name == "" {
		t.Fatalf("expected root node name to be populated")
	}

	if len(descriptor.Tree.Fields) == 0 {
		t.Fatalf("expected root fields to be populated, got %v", descriptor.Tree.Fields)
	}

	booksNode := descriptor.Tree.Children["books"]
	if booksNode == nil {
		t.Fatalf("expected books relation to be registered: %+v", descriptor.Tree.Children)
	}

	if booksNode.Name != "books" {
		t.Fatalf("expected books node name to be 'books', got %q", booksNode.Name)
	}

	if _, ok := booksNode.Children["publisher"]; !ok {
		t.Fatalf("expected publisher relation under books")
	}

	if len(descriptor.Includes) == 0 {
		t.Fatalf("expected includes to be populated")
	}

	expectedIncludes := map[string]struct{}{
		"books":           {},
		"books.publisher": {},
		"meta":            {},
	}

	for _, include := range descriptor.Includes {
		delete(expectedIncludes, include)
	}

	if len(expectedIncludes) != 0 {
		t.Fatalf("missing include paths: %v", expectedIncludes)
	}
}

func TestRelationFiltersAreApplied(t *testing.T) {
	resetRelationFilters()

	provider := NewDefaultRelationProvider()

	RegisterRelationFilter(func(_ reflect.Type, descriptor *RelationDescriptor) *RelationDescriptor {
		if descriptor == nil || descriptor.Tree == nil {
			return descriptor
		}
		// Drop the books relation to validate filters run.
		delete(descriptor.Tree.Children, "books")
		descriptor.Includes = nil
		return descriptor
	})

	descriptor, err := provider.BuildRelationDescriptor(reflect.TypeOf(&relationTestAuthor{}))
	if err != nil {
		t.Fatalf("unexpected error building descriptor: %v", err)
	}

	descriptor = ApplyRelationFilters(reflect.TypeOf(&relationTestAuthor{}), descriptor)

	if _, ok := descriptor.Tree.Children["books"]; ok {
		t.Fatalf("expected books relation to be removed by filter")
	}
}

func TestDefaultRelationProviderDetectsNilType(t *testing.T) {
	provider := NewDefaultRelationProvider()
	if _, err := provider.BuildRelationDescriptor(nil); err == nil {
		t.Fatal("expected error when building descriptor for nil type")
	}
}
