package router

import (
	"fmt"
	"net/http"
	"path"
	"strings"

	goerrors "github.com/goliatone/go-errors"
)

type OwnedRouteSet struct {
	Owner  string
	Routes []RouteDefinition
}

type ReservedRootClaim struct {
	Owner string
	Root  string
}

type OwnerRoutePolicy struct {
	Owner             string
	AllowedPrefixes   []string
	RouteNamePrefixes []string
}

type RouteOwnershipPolicy struct {
	ReservedRoots   []ReservedRootClaim
	Owners          []OwnerRoutePolicy
	RouteValidation RouteValidationOptions
}

func (p RouteOwnershipPolicy) withDefaults() RouteOwnershipPolicy {
	p.RouteValidation = p.RouteValidation.withDefaults()
	p.RouteValidation.PathConflictMode = p.RouteValidation.PathConflictMode.normalize()
	return p
}

func StrictRouteOwnershipPolicy() RouteOwnershipPolicy {
	return RouteOwnershipPolicy{
		RouteValidation: RouteValidationOptions{
			PathConflictMode: PathConflictModeStrict,
			NamedRoutePolicy: NamedRouteCollisionPolicyError,
		},
	}
}

func ValidateOwnedRouteSets(routeSets []OwnedRouteSet, policy RouteOwnershipPolicy) []error {
	policy = policy.withDefaults()

	var errs []error
	errs = append(errs, validateReservedRootClaims(policy.ReservedRoots)...)

	flattened := make([]*RouteDefinition, 0)
	for i := range routeSets {
		for j := range routeSets[i].Routes {
			route := routeSets[i].Routes[j]
			flattened = append(flattened, &route)
		}
	}
	errs = append(errs, ValidateRouteDefinitionsWithOptions(flattened, policy.RouteValidation)...)

	ownerPolicy := make(map[string]OwnerRoutePolicy, len(policy.Owners))
	for _, owner := range policy.Owners {
		normalized := OwnerRoutePolicy{
			Owner:             owner.Owner,
			AllowedPrefixes:   make([]string, 0, len(owner.AllowedPrefixes)),
			RouteNamePrefixes: append([]string(nil), owner.RouteNamePrefixes...),
		}
		for _, prefix := range owner.AllowedPrefixes {
			normalized.AllowedPrefixes = append(normalized.AllowedPrefixes, normalizeOwnershipPath(prefix))
		}
		ownerPolicy[owner.Owner] = normalized
	}

	for _, routeSet := range routeSets {
		owner := routeSet.Owner
		ownerCfg, hasOwnerCfg := ownerPolicy[owner]

		for _, route := range routeSet.Routes {
			routePath := normalizeOwnershipPath(route.Path)

			for _, claim := range policy.ReservedRoots {
				root := normalizeOwnershipPath(claim.Root)
				if claim.Owner == owner {
					continue
				}
				if ownershipPathHasPrefix(routePath, root) {
					errs = append(errs, newRouteReservedRootConflictError(owner, claim.Owner, root, route))
				}
			}

			if hasOwnerCfg && len(ownerCfg.AllowedPrefixes) > 0 && !matchesAnyOwnershipPrefix(routePath, ownerCfg.AllowedPrefixes) {
				errs = append(errs, newRouteOwnerPrefixMismatchError(owner, route, ownerCfg.AllowedPrefixes))
			}

			publicName := route.effectivePublicName()
			if hasOwnerCfg && publicName != "" && len(ownerCfg.RouteNamePrefixes) > 0 && !matchesAnyNamePrefix(publicName, ownerCfg.RouteNamePrefixes) {
				errs = append(errs, newRouteNamePrefixMismatchError(owner, route, ownerCfg.RouteNamePrefixes))
			}
		}
	}

	return errs
}

func validateReservedRootClaims(claims []ReservedRootClaim) []error {
	var errs []error
	ownersByRoot := make(map[string]string, len(claims))

	for _, claim := range claims {
		root := normalizeOwnershipPath(claim.Root)
		if existingOwner, ok := ownersByRoot[root]; ok && existingOwner != claim.Owner {
			errs = append(errs, newReservedRootConflictError(root, existingOwner, claim.Owner))
			continue
		}
		ownersByRoot[root] = claim.Owner
	}

	return errs
}

func normalizeOwnershipPath(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimSpace(raw))
}

func ownershipPathHasPrefix(routePath, prefix string) bool {
	if prefix == "/" {
		return true
	}
	return routePath == prefix || strings.HasPrefix(routePath, prefix+"/")
}

func matchesAnyOwnershipPrefix(routePath string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if ownershipPathHasPrefix(routePath, prefix) {
			return true
		}
	}
	return false
}

func matchesAnyNamePrefix(routeName string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(routeName, prefix) {
			return true
		}
	}
	return false
}

func newReservedRootConflictError(root, existingOwner, incomingOwner string) error {
	message := fmt.Sprintf("reserved root conflict: %q is claimed by %q and %q", root, existingOwner, incomingOwner)
	return goerrors.New(message, goerrors.CategoryConflict).
		WithCode(http.StatusConflict).
		WithTextCode("ROUTE_RESERVED_ROOT_CONFLICT").
		WithMetadata(map[string]any{
			"root":           root,
			"existing_owner": existingOwner,
			"incoming_owner": incomingOwner,
		})
}

func newRouteReservedRootConflictError(owner, reservedOwner, root string, route RouteDefinition) error {
	message := fmt.Sprintf("route reserved-root conflict: owner %q route %s %s is under %q owned by %q", owner, route.Method, route.Path, root, reservedOwner)
	return goerrors.New(message, goerrors.CategoryConflict).
		WithCode(http.StatusConflict).
		WithTextCode("ROUTE_RESERVED_ROOT_CONFLICT").
		WithMetadata(map[string]any{
			"owner":          owner,
			"reserved_owner": reservedOwner,
			"root":           root,
			"method":         route.Method,
			"path":           route.Path,
			"route_name":     route.Name,
		})
}

func newRouteOwnerPrefixMismatchError(owner string, route RouteDefinition, prefixes []string) error {
	message := fmt.Sprintf("route owner prefix mismatch: owner %q route %s %s is outside allowed prefixes", owner, route.Method, route.Path)
	return goerrors.New(message, goerrors.CategoryConflict).
		WithCode(http.StatusConflict).
		WithTextCode("ROUTE_OWNER_PREFIX_MISMATCH").
		WithMetadata(map[string]any{
			"owner":            owner,
			"method":           route.Method,
			"path":             route.Path,
			"route_name":       route.Name,
			"allowed_prefixes": prefixes,
		})
}

func newRouteNamePrefixMismatchError(owner string, route RouteDefinition, prefixes []string) error {
	message := fmt.Sprintf("route name prefix mismatch: owner %q route name %q does not match configured prefixes", owner, route.Name)
	return goerrors.New(message, goerrors.CategoryConflict).
		WithCode(http.StatusConflict).
		WithTextCode("ROUTE_NAME_PREFIX_MISMATCH").
		WithMetadata(map[string]any{
			"owner":               owner,
			"method":              route.Method,
			"path":                route.Path,
			"route_name":          route.Name,
			"route_name_prefixes": prefixes,
		})
}
