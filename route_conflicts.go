package router

import (
	"fmt"
	"net/http"
	"strings"

	goerrors "github.com/goliatone/go-errors"
)

type routeConflict struct {
	existing        *RouteDefinition
	reason          string
	index           int
	existingSegment string
	newSegment      string
}

type segmentKind int

const (
	segmentStatic segmentKind = iota
	segmentParam
	segmentCatchAll
)

func splitPathSegments(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func classifySegment(segment string) segmentKind {
	if strings.HasPrefix(segment, "*") {
		return segmentCatchAll
	}
	if strings.HasPrefix(segment, ":") {
		return segmentParam
	}
	return segmentStatic
}

func isStaticParamSibling(left, right segmentKind) bool {
	return (left == segmentStatic && right == segmentParam) || (left == segmentParam && right == segmentStatic)
}

func detectPathConflict(existingPath, newPath string, mode PathConflictMode, enforceCatchAllConflicts bool) *routeConflict {
	mode = mode.normalize()
	existingParts := splitPathSegments(existingPath)
	newParts := splitPathSegments(newPath)

	minLen := len(existingParts)
	if len(newParts) < minLen {
		minLen = len(newParts)
	}

	firstParamPairIndex := -1
	firstParamPairExistingSegment := ""
	firstParamPairNewSegment := ""
	firstStaticParamIndex := -1
	firstStaticParamExistingSegment := ""
	firstStaticParamNewSegment := ""

	for i := 0; i < minLen; i++ {
		existingSegment := existingParts[i]
		newSegment := newParts[i]
		existingKind := classifySegment(existingSegment)
		newKind := classifySegment(newSegment)

		if existingKind == segmentStatic && newKind == segmentStatic {
			if existingSegment != newSegment {
				return nil
			}
			continue
		}

		if (existingKind == segmentCatchAll || newKind == segmentCatchAll) && enforceCatchAllConflicts {
			return &routeConflict{
				reason:          "catch-all segment conflicts with existing route",
				index:           i,
				existingSegment: existingSegment,
				newSegment:      newSegment,
			}
		}

		if existingKind == segmentParam && newKind == segmentParam {
			if firstParamPairIndex == -1 {
				firstParamPairIndex = i
				firstParamPairExistingSegment = existingSegment
				firstParamPairNewSegment = newSegment
			}
			continue
		}

		if isStaticParamSibling(existingKind, newKind) && firstStaticParamIndex == -1 {
			firstStaticParamIndex = i
			firstStaticParamExistingSegment = existingSegment
			firstStaticParamNewSegment = newSegment
		}
	}

	if len(existingParts) != len(newParts) {
		return nil
	}

	if firstStaticParamIndex != -1 {
		if mode == PathConflictModePreferStatic {
			return nil
		}
		return &routeConflict{
			reason:          "static segment conflicts with wildcard segment",
			index:           firstStaticParamIndex,
			existingSegment: firstStaticParamExistingSegment,
			newSegment:      firstStaticParamNewSegment,
		}
	}

	if firstParamPairIndex == -1 {
		return nil
	}

	return &routeConflict{
		reason:          "wildcard segment conflicts with existing route",
		index:           firstParamPairIndex,
		existingSegment: firstParamPairExistingSegment,
		newSegment:      firstParamPairNewSegment,
	}
}

func newRouteConflictError(method HTTPMethod, path string, conflict *routeConflict, policy HTTPRouterConflictPolicy, mode PathConflictMode) error {
	message := fmt.Sprintf("route conflict: %s %s conflicts with %s", method, path, conflict.existing.Path)
	if conflict.reason != "" {
		message = fmt.Sprintf("%s (%s)", message, conflict.reason)
	}

	metadata := map[string]any{
		"adapter":       "shared",
		"method":        method,
		"path":          path,
		"existing_path": conflict.existing.Path,
		"policy":        policy.String(),
		"path_mode":     mode.String(),
		"reason":        conflict.reason,
	}

	if conflict.index >= 0 {
		metadata["segment_index"] = conflict.index
		metadata["segment"] = conflict.newSegment
		metadata["existing_segment"] = conflict.existingSegment
	}

	return goerrors.New(message, goerrors.CategoryConflict).
		WithCode(http.StatusConflict).
		WithTextCode("ROUTE_CONFLICT").
		WithMetadata(metadata)
}

func newRouteNameConflictError(routeName string, existing, incoming *RouteDefinition, policy NamedRouteCollisionPolicy) error {
	existingName := routeName
	if existing != nil {
		if publicName := existing.effectivePublicName(); publicName != "" {
			existingName = publicName
		} else if existing.Name != "" {
			existingName = existing.Name
		}
	}

	message := fmt.Sprintf(
		"route name conflict: %q maps to %s %s and %s %s",
		routeName,
		existing.Method,
		existing.Path,
		incoming.Method,
		incoming.Path,
	)

	metadata := map[string]any{
		"route_name":      routeName,
		"existing_method": existing.Method,
		"existing_path":   existing.Path,
		"incoming_method": incoming.Method,
		"incoming_path":   incoming.Path,
		"policy":          policy.String(),
		"existing_name":   existingName,
		"incoming_name":   routeName,
	}

	return goerrors.New(message, goerrors.CategoryConflict).
		WithCode(http.StatusConflict).
		WithTextCode("ROUTE_NAME_CONFLICT").
		WithMetadata(metadata)
}

func newUnsupportedPathConflictModeError(adapter string, mode PathConflictMode) error {
	mode = mode.normalize()
	message := fmt.Sprintf("path conflict mode %q is not supported by %s adapter", mode, adapter)
	metadata := map[string]any{
		"adapter":   adapter,
		"path_mode": mode.String(),
	}

	return goerrors.New(message, goerrors.CategoryConflict).
		WithCode(http.StatusNotImplemented).
		WithTextCode("ROUTE_CONFLICT_MODE_UNSUPPORTED").
		WithMetadata(metadata)
}
