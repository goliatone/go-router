package router

import (
	"errors"
	"fmt"
)

// RegistrationState describes whether a router still accepts declarations.
// A zero value is equivalent to RegistrationCollecting.
type RegistrationState string

const (
	RegistrationCollecting RegistrationState = "collecting"
	RegistrationSealed     RegistrationState = "sealed"
)

var (
	// ErrRouterSealed is returned when code attempts to mutate a sealed route plan.
	ErrRouterSealed = errors.New("router registration is sealed")
	// ErrRouteNotFound is returned when an explicit replacement has no exact target.
	ErrRouteNotFound = errors.New("route not found")
)

// RegistrationError is the typed failure raised by compatibility methods whose
// signatures cannot return an error (Handle, Use, Static, and HandleMiss).
type RegistrationError struct {
	Operation string
	Method    HTTPMethod
	Path      string
	State     RegistrationState
	Err       error
}

func (e *RegistrationError) Error() string {
	if e == nil {
		return "router registration error"
	}
	target := e.Path
	if e.Method != "" {
		target = fmt.Sprintf("%s %s", e.Method, e.Path)
	}
	if target == "" {
		return fmt.Sprintf("%s: router is %s: %v", e.Operation, e.State, e.Err)
	}
	return fmt.Sprintf("%s %s: router is %s: %v", e.Operation, target, e.State, e.Err)
}

func (e *RegistrationError) Unwrap() error { return e.Err }

// RegistrationSnapshot is safe to expose to diagnostics. DeclaredRoutes are
// the logical plan; MountedRoutes are in physical dispatch order.
type RegistrationSnapshot struct {
	State          RegistrationState `json:"state"`
	Revision       uint64            `json:"revision"`
	DeclaredRoutes []RouteDefinition `json:"declared_routes,omitempty"`
	MountedRoutes  []RouteDefinition `json:"mounted_routes,omitempty"`
}

// RegistrationInspector is implemented by routers that expose lifecycle and
// physical route-table state.
type RegistrationInspector interface {
	RegistrationSnapshot() RegistrationSnapshot
}

// RouteReplacer supports an intentional exact-route override while the plan is
// collecting. Ordinary duplicate registration remains governed by conflict policy.
type RouteReplacer interface {
	TryReplace(method HTTPMethod, path string, handler HandlerFunc, middlewares ...MiddlewareFunc) (RouteInfo, error)
}

// RouteUpserter explicitly replaces an exact declaration when present or adds
// it when an optional upstream feature did not declare the route. The bool is
// true when an existing route was replaced.
type RouteUpserter interface {
	TryUpsert(method HTTPMethod, path string, handler HandlerFunc, middlewares ...MiddlewareFunc) (RouteInfo, bool, error)
}

// RouteShadow describes a later route that cannot be selected because an
// earlier, broader route accepts every request represented by it.
type RouteShadow struct {
	Method          HTTPMethod `json:"method"`
	Path            string     `json:"path"`
	RouteIndex      int        `json:"route_index"`
	ShadowedByPath  string     `json:"shadowed_by_path"`
	ShadowedByIndex int        `json:"shadowed_by_index"`
	Reason          string     `json:"reason"`
}

// AnalyzeRouteShadows evaluates routes in physical dispatch order.
func AnalyzeRouteShadows(routes []RouteDefinition) []RouteShadow {
	findings := make([]RouteShadow, 0)
	for routeIndex, route := range routes {
		for earlierIndex := range routeIndex {
			earlier := routes[earlierIndex]
			if earlier.Method != route.Method || !routePatternContains(earlier.Path, route.Path) {
				continue
			}
			reason := "earlier route matches the same request set"
			if earlier.Path == route.Path {
				reason = "earlier duplicate route"
			} else if containsCatchAll(earlier.Path) {
				reason = "earlier catch-all route"
			} else {
				reason = "earlier parameter route"
			}
			findings = append(findings, RouteShadow{
				Method:          route.Method,
				Path:            route.Path,
				RouteIndex:      routeIndex,
				ShadowedByPath:  earlier.Path,
				ShadowedByIndex: earlierIndex,
				Reason:          reason,
			})
			break
		}
	}
	return findings
}

func containsCatchAll(path string) bool {
	for _, segment := range splitPathSegments(path) {
		if classifySegment(segment) == segmentCatchAll {
			return true
		}
	}
	return false
}

// routePatternContains reports whether every request accepted by candidate is
// also accepted by earlier. It models Fiber/httprouter static, :param, and
// terminal *catch-all segments.
func routePatternContains(earlierPath, candidatePath string) bool {
	earlier := splitPathSegments(earlierPath)
	candidate := splitPathSegments(candidatePath)

	for i := range earlier {
		if classifySegment(earlier[i]) == segmentCatchAll {
			return prefixesCompatible(earlier[:i], candidate)
		}
		if i >= len(candidate) {
			return false
		}

		earlierKind := classifySegment(earlier[i])
		candidateKind := classifySegment(candidate[i])
		switch {
		case candidateKind == segmentCatchAll:
			return false
		case earlierKind == segmentStatic && candidateKind == segmentStatic:
			if earlier[i] != candidate[i] {
				return false
			}
		case earlierKind == segmentStatic:
			return false
		}
	}

	return len(earlier) == len(candidate)
}

func prefixesCompatible(earlierPrefix, candidate []string) bool {
	if len(candidate) < len(earlierPrefix) {
		return false
	}
	for i, segment := range earlierPrefix {
		earlierKind := classifySegment(segment)
		candidateKind := classifySegment(candidate[i])
		if candidateKind == segmentCatchAll {
			return false
		}
		if earlierKind == segmentStatic {
			if candidateKind != segmentStatic || segment != candidate[i] {
				return false
			}
		}
	}
	return true
}

func newRegistrationError(operation string, method HTTPMethod, path string, state RegistrationState, err error) error {
	if err == nil {
		err = ErrRouterSealed
	}
	return &RegistrationError{
		Operation: operation,
		Method:    method,
		Path:      path,
		State:     state,
		Err:       err,
	}
}

func newRouteNotFoundError(method HTTPMethod, path string) error {
	return &RegistrationError{
		Operation: "replace route",
		Method:    method,
		Path:      path,
		State:     RegistrationCollecting,
		Err:       fmt.Errorf("%w: %s %s", ErrRouteNotFound, method, path),
	}
}
