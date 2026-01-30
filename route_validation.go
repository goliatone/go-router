package router

import (
	"fmt"
	"net/http"
	"strings"

	goerrors "github.com/goliatone/go-errors"
)

func (br *BaseRouter) ValidateRoutes() []error {
	routes := make([]*RouteDefinition, 0, len(br.root.routes)+len(br.root.lateRoutes))
	routes = append(routes, br.root.routes...)
	for _, late := range br.root.lateRoutes {
		routes = append(routes, &RouteDefinition{
			Method: late.method,
			Path:   late.path,
		})
	}
	return ValidateRouteDefinitions(routes)
}

// ValidateRouteDefinitions checks for conflicting or ambiguous routes.
func ValidateRouteDefinitions(routes []*RouteDefinition) []error {
	var errs []error

	for i := 0; i < len(routes); i++ {
		for j := i + 1; j < len(routes); j++ {
			left := routes[i]
			right := routes[j]
			if left.Method != right.Method {
				continue
			}

			if left.Path == right.Path {
				conflict := &routeConflict{
					existing: left,
					reason:   "duplicate route",
					index:    -1,
				}
				errs = append(errs, newRouteConflictError(left.Method, right.Path, conflict, HTTPRouterConflictPanic))
				continue
			}

			if conflict := detectPathConflict(left.Path, right.Path); conflict != nil {
				conflict.existing = left
				errs = append(errs, newRouteConflictError(left.Method, right.Path, conflict, HTTPRouterConflictPanic))
			}

			if lintErr := detectBareIDParamLint(left, right); lintErr != nil {
				errs = append(errs, lintErr)
			}
			if lintErr := detectBareIDParamLint(right, left); lintErr != nil {
				errs = append(errs, lintErr)
			}
		}
	}

	return errs
}

func detectBareIDParamLint(paramRoute, staticRoute *RouteDefinition) error {
	if paramRoute.Method != staticRoute.Method {
		return nil
	}

	paramParts := splitPathSegments(paramRoute.Path)
	staticParts := splitPathSegments(staticRoute.Path)
	if len(paramParts) != len(staticParts) {
		return nil
	}

	for i, paramSegment := range paramParts {
		name, constraint, ok := parseParamSegment(paramSegment)
		if !ok || name != "id" || constraint != "" {
			continue
		}
		if classifySegment(staticParts[i]) != segmentStatic {
			continue
		}

		for k := 0; k < len(paramParts); k++ {
			if k == i {
				continue
			}
			if paramParts[k] != staticParts[k] {
				return nil
			}
		}

		return newRouteLintError(paramRoute.Method, paramRoute.Path, staticRoute.Path, name)
	}

	return nil
}

func parseParamSegment(segment string) (name string, constraint string, ok bool) {
	if !strings.HasPrefix(segment, ":") {
		return "", "", false
	}
	raw := strings.TrimPrefix(segment, ":")
	if raw == "" {
		return "", "", false
	}
	if idx := strings.Index(raw, "<"); idx >= 0 && strings.HasSuffix(raw, ">") {
		return raw[:idx], raw[idx+1 : len(raw)-1], true
	}
	return raw, "", true
}

func newRouteLintError(method HTTPMethod, path, siblingPath, param string) error {
	message := fmt.Sprintf("route lint: %s %s uses bare :%s with static sibling %s; consider constraining the param", method, path, param, siblingPath)
	metadata := map[string]any{
		"method":       method,
		"path":         path,
		"sibling_path": siblingPath,
		"param":        param,
	}

	return goerrors.New(message, goerrors.CategoryConflict).
		WithCode(http.StatusConflict).
		WithTextCode("ROUTE_LINT").
		WithMetadata(metadata)
}
