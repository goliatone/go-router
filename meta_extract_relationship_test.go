package router

import (
	"reflect"
	"testing"
)

// Test models for relationship metadata extraction
type TestUser struct {
	ID    int64  `bun:",pk" json:"id"`
	Name  string `bun:"name" json:"name"`
	Email string `bun:"email,unique" json:"email"`
}

type TestOrder struct {
	ID     int64           `bun:",pk" json:"id"`
	UserID int64           `bun:"user_id,notnull" json:"user_id"`
	User   TestUser        `bun:"rel:belongs-to,join:user_id=id" json:"user"`
	Items  []TestOrderItem `bun:"rel:has-many,join:id=order_id" json:"items"`
}

type TestOrderItem struct {
	ID      int64     `bun:",pk" json:"id"`
	OrderID int64     `bun:"order_id,notnull" json:"order_id"`
	Order   TestOrder `bun:"rel:belongs-to,join:order_id=id" json:"order"`
}

type TestProductCategory struct {
	ID       int64         `bun:",pk" json:"id"`
	Name     string        `bun:"name" json:"name"`
	Products []TestProduct `bun:"m2m:product_categories,join:category=id,product=product_id" json:"products"`
}

type TestProduct struct {
	ID         int64                 `bun:",pk" json:"id"`
	Name       string                `bun:"name" json:"name"`
	Categories []TestProductCategory `bun:"m2m:product_categories" json:"categories"`
}

// Test with manual overrides
type TestComplexRelation struct {
	ID         int64    `bun:",pk" json:"id"`
	CustomUser TestUser `bun:"rel:belongs-to,join:custom_user_uuid=uuid" crud:"target_table=users,target_column=uuid" json:"custom_user"`
}

func TestRelationshipMetadataExtraction(t *testing.T) {
	tests := []struct {
		name    string
		inType  reflect.Type
		checkFn func(t *testing.T, got SchemaMetadata)
	}{
		{
			name:   "belongs-to relationship with explicit join",
			inType: reflect.TypeOf(TestOrder{}),
			checkFn: func(t *testing.T, got SchemaMetadata) {
				rel, exists := got.Relationships["user"]
				if !exists {
					t.Fatal("expected 'user' relationship")
				}

				if rel.RelationType != "belongs-to" {
					t.Errorf("expected RelationType=belongs-to, got %s", rel.RelationType)
				}
				if rel.SourceTable != "test_orders" {
					t.Errorf("expected SourceTable=test_orders, got %s", rel.SourceTable)
				}
				if rel.SourceColumn != "user_id" {
					t.Errorf("expected SourceColumn=user_id, got %s", rel.SourceColumn)
				}
				if rel.TargetTable != "test_users" {
					t.Errorf("expected TargetTable=test_users, got %s", rel.TargetTable)
				}
				if rel.TargetColumn != "id" {
					t.Errorf("expected TargetColumn=id, got %s", rel.TargetColumn)
				}
			},
		},
		{
			name:   "has-many relationship with explicit join",
			inType: reflect.TypeOf(TestOrder{}),
			checkFn: func(t *testing.T, got SchemaMetadata) {
				rel, exists := got.Relationships["items"]
				if !exists {
					t.Fatal("expected 'items' relationship")
				}

				if rel.RelationType != "has-many" {
					t.Errorf("expected RelationType=has-many, got %s", rel.RelationType)
				}
				if rel.SourceTable != "test_orders" {
					t.Errorf("expected SourceTable=test_orders, got %s", rel.SourceTable)
				}
				if rel.SourceColumn != "id" {
					t.Errorf("expected SourceColumn=id, got %s", rel.SourceColumn)
				}
				if rel.TargetTable != "test_order_items" {
					t.Errorf("expected TargetTable=test_order_items, got %s", rel.TargetTable)
				}
				if rel.TargetColumn != "order_id" {
					t.Errorf("expected TargetColumn=order_id, got %s", rel.TargetColumn)
				}
				if !rel.IsSlice {
					t.Error("expected IsSlice=true for has-many")
				}
			},
		},
		{
			name:   "many-to-many with explicit join columns",
			inType: reflect.TypeOf(TestProductCategory{}),
			checkFn: func(t *testing.T, got SchemaMetadata) {
				rel, exists := got.Relationships["products"]
				if !exists {
					t.Fatal("expected 'products' relationship")
				}

				if rel.RelationType != "many-to-many" {
					t.Errorf("expected RelationType=many-to-many, got %s", rel.RelationType)
				}
				if rel.PivotTable != "product_categories" {
					t.Errorf("expected PivotTable=product_categories, got %s", rel.PivotTable)
				}
				if rel.SourceTable != "test_product_categories" {
					t.Errorf("expected SourceTable=test_product_categories, got %s", rel.SourceTable)
				}
				if rel.TargetTable != "test_products" {
					t.Errorf("expected TargetTable=test_products, got %s", rel.TargetTable)
				}
				if rel.SourcePivotColumn != "id" {
					t.Errorf("expected SourcePivotColumn=id, got %s", rel.SourcePivotColumn)
				}
				if rel.TargetPivotColumn != "product_id" {
					t.Errorf("expected TargetPivotColumn=product_id, got %s", rel.TargetPivotColumn)
				}
			},
		},
		{
			name:   "many-to-many with simple pivot table name",
			inType: reflect.TypeOf(TestProduct{}),
			checkFn: func(t *testing.T, got SchemaMetadata) {
				rel, exists := got.Relationships["categories"]
				if !exists {
					t.Fatal("expected 'categories' relationship")
				}

				if rel.PivotTable != "product_categories" {
					t.Errorf("expected PivotTable=product_categories, got %s", rel.PivotTable)
				}
				// Should use default column names
				if rel.SourcePivotColumn != "test_product_id" {
					t.Errorf("expected SourcePivotColumn=test_product_id, got %s", rel.SourcePivotColumn)
				}
				if rel.TargetPivotColumn != "test_product_category_id" {
					t.Errorf("expected TargetPivotColumn=test_product_category_id, got %s", rel.TargetPivotColumn)
				}
			},
		},
		{
			name:   "relationship with crud tag overrides",
			inType: reflect.TypeOf(TestComplexRelation{}),
			checkFn: func(t *testing.T, got SchemaMetadata) {
				rel, exists := got.Relationships["custom_user"]
				if !exists {
					t.Fatal("expected 'custom_user' relationship")
				}

				if rel.TargetTable != "users" {
					t.Errorf("expected TargetTable=users (overridden), got %s", rel.TargetTable)
				}
				if rel.TargetColumn != "uuid" {
					t.Errorf("expected TargetColumn=uuid (overridden), got %s", rel.TargetColumn)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractSchemaFromType(tt.inType)
			tt.checkFn(t, got)
		})
	}
}
