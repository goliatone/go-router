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
			strictConflict := detectPathConflict(tt.existingPath, tt.newPath, PathConflictModeStrict, true)
			if strictConflict == nil {
				t.Fatal("expected strict mode conflict")
			}
			if strictConflict.index != 3 {
				t.Fatalf("expected strict conflict at segment index 3, got %d", strictConflict.index)
			}
			if strictConflict.reason != "static segment conflicts with wildcard segment" {
				t.Fatalf("expected strict static/wildcard conflict reason, got %q", strictConflict.reason)
			}

			preferStaticConflict := detectPathConflict(tt.existingPath, tt.newPath, PathConflictModePreferStatic, true)
			if preferStaticConflict != nil {
				t.Fatalf("expected prefer_static mode to allow static/param sibling at discriminating segment, got %+v", preferStaticConflict)
			}
		})
	}
}

func TestDetectPathConflict_CatchAllEnforcementOptIn(t *testing.T) {
	existingPath := "/files/*filepath"
	newPath := "/files/:id"

	conflict := detectPathConflict(existingPath, newPath, PathConflictModePreferStatic, false)
	if conflict != nil {
		t.Fatalf("expected no conflict when catch-all enforcement is disabled, got %+v", conflict)
	}

	conflict = detectPathConflict(existingPath, newPath, PathConflictModePreferStatic, true)
	if conflict == nil {
		t.Fatal("expected conflict when catch-all enforcement is enabled")
	}
	if conflict.reason != "catch-all segment conflicts with existing route" {
		t.Fatalf("unexpected conflict reason: %q", conflict.reason)
	}
}
