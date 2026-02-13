package router

import "testing"

func TestDetectPathConflict_SharedParamPrefixStaticSibling(t *testing.T) {
	tests := []struct {
		name         string
		existingPath string
		newPath      string
	}{
		{
			name:         "param then static",
			existingPath: "/admin/content/:name/:id",
			newPath:      "/admin/content/:name/new",
		},
		{
			name:         "static then param",
			existingPath: "/admin/content/:name/new",
			newPath:      "/admin/content/:name/:id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strictConflict := detectPathConflict(tt.existingPath, tt.newPath, PathConflictModeStrict)
			if strictConflict == nil {
				t.Fatal("expected strict mode conflict")
			}
			if strictConflict.index != 3 {
				t.Fatalf("expected strict conflict at segment index 3, got %d", strictConflict.index)
			}
			if strictConflict.reason != "static segment conflicts with wildcard segment" {
				t.Fatalf("expected strict static/wildcard conflict reason, got %q", strictConflict.reason)
			}

			preferStaticConflict := detectPathConflict(tt.existingPath, tt.newPath, PathConflictModePreferStatic)
			if preferStaticConflict != nil {
				t.Fatalf("expected prefer_static mode to allow static/param sibling at discriminating segment, got %+v", preferStaticConflict)
			}
		})
	}
}
