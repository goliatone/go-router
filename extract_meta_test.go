package router_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/goliatone/go-router"
	"github.com/google/uuid"
)

// Example test structs
type BaseModel struct {
	CreatedAt time.Time `bun:"created_at,notnull" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,notnull" json:"updated_at"`
}

// has-one relationship
type Profile struct {
	ID        int64     `bun:",pk" json:"id"`
	AvatarURL string    `bun:"avatar_url" json:"avatar_url"`
	UserID    int64     `bun:"user_id" json:"user_id"`
	CreatedAt time.Time `bun:"created_at,notnull" json:"created_at"`
}

// has-many relationship
type Post struct {
	ID     int64  `bun:",pk" json:"id"`
	UserID int64  `bun:"user_id" json:"user_id"`
	Title  string `bun:"title,notnull" json:"title"`
}

// belongs-to relationship
type Company struct {
	ID   int64  `bun:",pk" json:"id"`
	Name string `bun:"name,notnull" json:"name"`
}

// many-to-many pivot
type OrderToItem struct {
	OrderID int64  `bun:",pk" json:"order_id"`
	ItemID  int64  `bun:",pk" json:"item_id"`
	Order   *Order `bun:"rel:belongs-to,join:order_id=id" json:"order,omitempty"`
	Item    *Item  `bun:"rel:belongs-to,join:item_id=id" json:"item,omitempty"`
}

// m2m relationship
type Order struct {
	ID    int64  `bun:",pk" json:"id"`
	Items []Item `bun:"m2m:order_to_items,join:Order=Item" json:"items"`
}

type Item struct {
	ID int64 `bun:",pk" json:"id"`
}

// main user model that references many relationships
type User struct {
	BaseModel
	ID       uuid.UUID `bun:"id,pk,notnull" json:"id"`
	Name     string    `bun:"name,notnull" json:"name"`
	Email    string    `bun:"email,notnull" json:"email"`
	Age      int       `bun:"age" json:"age"`
	Password string    `bun:"password" json:"-" crud:"-"`
	// has-one
	Profile Profile `bun:"rel:has-one,join:id=user_id" json:"profile,omitempty"`
	// has-many
	Posts []Post `bun:"rel:has-many,join:id=user_id" json:"posts,omitempty"`
	// belongs-to
	CompanyID int64    `bun:"company_id" json:"company_id"`
	Company   *Company `bun:"rel:belongs-to,join:company_id=id" json:"company,omitempty"`
}

func TestExtractSchemaFromType(t *testing.T) {
	tests := []struct {
		name    string
		inType  reflect.Type
		wantMD  router.SchemaMetadata
		checkFn func(t *testing.T, got router.SchemaMetadata)
	}{
		{
			name:   "simple base model",
			inType: reflect.TypeOf(BaseModel{}),
			checkFn: func(t *testing.T, got router.SchemaMetadata) {
				// We expect 2 properties: created_at, updated_at
				if len(got.Properties) != 2 {
					t.Errorf("expected 2 properties, got %d", len(got.Properties))
				}
				// Check that relationships is empty
				if len(got.Relationships) != 0 {
					t.Errorf("did not expect relationships, got %d", len(got.Relationships))
				}
				if len(got.Required) != 2 {
					t.Errorf("expected 2 required fields, got %d", len(got.Required))
				}
			},
		},
		{
			name:   "user model with relationships",
			inType: reflect.TypeOf(User{}),
			checkFn: func(t *testing.T, got router.SchemaMetadata) {
				// Check a few expected properties
				if _, ok := got.Properties["id"]; !ok {
					t.Errorf("expected 'id' property, not found")
				}
				if _, ok := got.Properties["name"]; !ok {
					t.Errorf("expected 'name' property, not found")
				}
				// We also expect 5 or 6 properties (depending on how you skip BaseModel):
				//   id, name, email, age, password (actually skipped via crud:"-"), created_at, updated_at
				// Check relationships
				if len(got.Relationships) == 0 {
					t.Errorf("expected some relationships, got 0")
				}

				// Check has-one
				rel, ok := got.Relationships["profile"]
				if !ok {
					t.Errorf("expected 'profile' relationship, not found")
				} else {
					if rel.RelationType != "has-one" {
						t.Errorf("expected relationType=has-one, got %s", rel.RelationType)
					}
					if rel.RelatedTypeName != "Profile" {
						t.Errorf("expected RelatedTypeName=Profile, got %s", rel.RelatedTypeName)
					}
					if rel.IsSlice {
						t.Errorf("expected IsSlice=false for has-one")
					}
				}

				// Check has-many
				relPosts, ok := got.Relationships["posts"]
				if !ok {
					t.Errorf("expected 'posts' relationship, not found")
				} else {
					if relPosts.RelationType != "has-many" {
						t.Errorf("expected relationType=has-many, got %s", relPosts.RelationType)
					}
					if relPosts.RelatedTypeName != "Post" {
						t.Errorf("expected RelatedTypeName=Post, got %s", relPosts.RelatedTypeName)
					}
					if !relPosts.IsSlice {
						t.Errorf("expected IsSlice=true for has-many")
					}
				}

				// Check belongs-to
				relCompany, ok := got.Relationships["company"]
				if !ok {
					t.Errorf("expected 'company' relationship, not found")
				} else {
					if relCompany.RelationType != "belongs-to" {
						t.Errorf("expected relationType=belongs-to, got %s", relCompany.RelationType)
					}
					if relCompany.RelatedTypeName != "Company" {
						t.Errorf("expected RelatedTypeName=Company, got %s", relCompany.RelatedTypeName)
					}
					if relCompany.IsSlice {
						t.Errorf("expected IsSlice=false for belongs-to")
					}
				}
			},
		},
		{
			name:   "order with m2m relationship",
			inType: reflect.TypeOf(Order{}),
			checkFn: func(t *testing.T, got router.SchemaMetadata) {
				// Check properties: just ID
				if len(got.Properties) != 1 {
					t.Errorf("expected 1 property (ID), got %d", len(got.Properties))
				}
				// Check relationships - expect 1
				if len(got.Relationships) != 1 {
					t.Errorf("expected 1 relationship, got %d", len(got.Relationships))
				}
				// Validate the m2m relationship
				rel, ok := got.Relationships["items"]
				if !ok {
					t.Errorf("expected 'items' relationship for m2m, not found")
				} else {
					if rel.RelationType != "many-to-many" {
						t.Errorf("expected relationType=m2m, got %s", rel.RelationType)
					}
					if rel.RelatedTypeName != "Item" {
						t.Errorf("expected RelatedTypeName=Item, got %s", rel.RelatedTypeName)
					}
					if !rel.IsSlice {
						t.Errorf("expected IsSlice=true for m2m items")
					}
					if rel.PivotTable != "order_to_items" {
						t.Errorf("expected PivotTable=order_to_items, got %s", rel.PivotTable)
					}
					if rel.PivotJoin != "Order=Item" {
						t.Errorf("expected PivotJoin=Order=Item, got %s", rel.PivotJoin)
					}
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := router.ExtractSchemaFromType(tc.inType)
			tc.checkFn(t, got)
		})
	}
}
