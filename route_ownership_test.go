package router

import (
	"strings"
	"testing"
)

func TestValidateOwnedRouteSetsReservedRootOwnershipChecks(t *testing.T) {
	errs := ValidateOwnedRouteSets([]OwnedRouteSet{
		{
			Owner: "translations",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/admin/translations", Name: "translations.list"},
			},
		},
	}, RouteOwnershipPolicy{
		ReservedRoots: []ReservedRootClaim{
			{Owner: "host", Root: "/admin"},
		},
		Owners: []OwnerRoutePolicy{
			{Owner: "translations", AllowedPrefixes: []string{"/modules/translations"}},
		},
	})

	if len(errs) == 0 {
		t.Fatal("expected reserved-root ownership violation")
	}
	if !strings.Contains(errs[0].Error(), "ROUTE_RESERVED_ROOT_CONFLICT") {
		t.Fatalf("expected reserved root conflict, got %q", errs[0].Error())
	}
}

func TestValidateOwnedRouteSetsDuplicateReservedRootClaimsFail(t *testing.T) {
	errs := ValidateOwnedRouteSets(nil, RouteOwnershipPolicy{
		ReservedRoots: []ReservedRootClaim{
			{Owner: "host", Root: "/admin"},
			{Owner: "translations", Root: "/admin"},
		},
	})

	if len(errs) == 0 {
		t.Fatal("expected duplicate reserved-root claim error")
	}
	if !strings.Contains(errs[0].Error(), "ROUTE_RESERVED_ROOT_CONFLICT") {
		t.Fatalf("expected reserved root conflict, got %q", errs[0].Error())
	}
}

func TestValidateOwnedRouteSetsRouteOutsideOwnerPrefixFails(t *testing.T) {
	errs := ValidateOwnedRouteSets([]OwnedRouteSet{
		{
			Owner: "translations",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/api/translations", Name: "translations.list"},
			},
		},
	}, RouteOwnershipPolicy{
		Owners: []OwnerRoutePolicy{
			{Owner: "translations", AllowedPrefixes: []string{"/modules/translations"}},
		},
	})

	if len(errs) == 0 {
		t.Fatal("expected owner prefix mismatch")
	}
	if !strings.Contains(errs[0].Error(), "ROUTE_OWNER_PREFIX_MISMATCH") {
		t.Fatalf("expected owner prefix mismatch, got %q", errs[0].Error())
	}
}

func TestValidateOwnedRouteSetsRouteNamePrefixMismatchFails(t *testing.T) {
	errs := ValidateOwnedRouteSets([]OwnedRouteSet{
		{
			Owner: "translations",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/modules/translations", Name: "content.list"},
			},
		},
	}, RouteOwnershipPolicy{
		Owners: []OwnerRoutePolicy{
			{
				Owner:             "translations",
				AllowedPrefixes:   []string{"/modules/translations"},
				RouteNamePrefixes: []string{"translations."},
			},
		},
	})

	if len(errs) == 0 {
		t.Fatal("expected route-name prefix mismatch")
	}
	if !strings.Contains(errs[0].Error(), "ROUTE_NAME_PREFIX_MISMATCH") {
		t.Fatalf("expected route-name prefix mismatch, got %q", errs[0].Error())
	}
}

func TestValidateOwnedRouteSetsMixedHostAndModulePolicyPasses(t *testing.T) {
	errs := ValidateOwnedRouteSets([]OwnedRouteSet{
		{
			Owner: "host",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/admin/users", Name: "admin.users.list"},
			},
		},
		{
			Owner: "translations",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/modules/translations/entries", Name: "translations.entries.list"},
			},
		},
	}, RouteOwnershipPolicy{
		ReservedRoots: []ReservedRootClaim{
			{Owner: "host", Root: "/admin"},
		},
		Owners: []OwnerRoutePolicy{
			{
				Owner:             "host",
				AllowedPrefixes:   []string{"/admin"},
				RouteNamePrefixes: []string{"admin."},
			},
			{
				Owner:             "translations",
				AllowedPrefixes:   []string{"/modules/translations"},
				RouteNamePrefixes: []string{"translations."},
			},
		},
	})

	if len(errs) != 0 {
		t.Fatalf("expected mixed host/module policy to pass, got %v", errs)
	}
}

func TestValidateOwnedRouteSetsReusesSharedRouteValidation(t *testing.T) {
	errs := ValidateOwnedRouteSets([]OwnedRouteSet{
		{
			Owner: "host",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/admin/users/:id", Name: "admin.users.show"},
			},
		},
		{
			Owner: "host",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/admin/users/history", Name: "admin.users.history"},
			},
		},
	}, RouteOwnershipPolicy{
		Owners: []OwnerRoutePolicy{
			{Owner: "host", AllowedPrefixes: []string{"/admin"}},
		},
		RouteValidation: RouteValidationOptions{
			PathConflictMode: PathConflictModeStrict,
		},
	})

	if len(errs) == 0 {
		t.Fatal("expected shared route validation conflict")
	}
	if !strings.Contains(errs[0].Error(), "ROUTE_CONFLICT") {
		t.Fatalf("expected route conflict from shared validation, got %q", errs[0].Error())
	}
}

func TestValidateOwnedRouteSetsExplicitReplacePreservesCallerIntent(t *testing.T) {
	errs := ValidateOwnedRouteSets([]OwnedRouteSet{
		{
			Owner: "host",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/users/:id", Name: "users.show"},
			},
		},
		{
			Owner: "host",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/members/:id", Name: "users.show"},
			},
		},
	}, RouteOwnershipPolicy{
		RouteValidation: RouteValidationOptions{
			NamedRoutePolicy: NamedRouteCollisionPolicyReplace,
		},
	})

	if len(errs) != 0 {
		t.Fatalf("expected explicit replace policy to avoid named-route validation errors, got %v", errs)
	}
}

func TestStrictRouteOwnershipPolicyUsesStrictNamedRouteDefaults(t *testing.T) {
	policy := StrictRouteOwnershipPolicy()
	errs := ValidateOwnedRouteSets([]OwnedRouteSet{
		{
			Owner: "host",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/users/:id", Name: "users.show"},
			},
		},
		{
			Owner: "host",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/members/:id", Name: "users.show"},
			},
		},
	}, policy)

	if len(errs) == 0 {
		t.Fatal("expected strict ownership helper to enforce named-route conflicts")
	}
	if !strings.Contains(errs[0].Error(), "ROUTE_NAME_CONFLICT") {
		t.Fatalf("expected ROUTE_NAME_CONFLICT, got %q", errs[0].Error())
	}
}

func TestValidateOwnedRouteSetsIgnoresInternalNamesForPrefixChecks(t *testing.T) {
	errs := ValidateOwnedRouteSets([]OwnedRouteSet{
		{
			Owner: "translations",
			Routes: []RouteDefinition{
				{Method: GET, Path: "/modules/translations/openapi.json", Name: "openapi.json", nameMode: routeNameModeInternal},
			},
		},
	}, RouteOwnershipPolicy{
		Owners: []OwnerRoutePolicy{
			{
				Owner:             "translations",
				AllowedPrefixes:   []string{"/modules/translations"},
				RouteNamePrefixes: []string{"translations."},
			},
		},
	})

	if len(errs) != 0 {
		t.Fatalf("expected internal helper names to be ignored by prefix validation, got %v", errs)
	}
}
