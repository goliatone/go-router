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

func detectPathConflict(existingPath, newPath string, mode PathConflictMode) *routeConflict {
	mode = mode.normalize()
	existingParts := splitPathSegments(existingPath)
	newParts := splitPathSegments(newPath)

	minLen := len(existingParts)
	if len(newParts) < minLen {
		minLen = len(newParts)
	}

	firstWildcardIndex := -1
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

		if existingKind == segmentCatchAll || newKind == segmentCatchAll {
			return &routeConflict{
				reason:          "catch-all segment conflicts with existing route",
				index:           i,
				existingSegment: existingSegment,
				newSegment:      newSegment,
			}
		}

		if existingKind == segmentParam || newKind == segmentParam {
			if firstWildcardIndex == -1 {
				firstWildcardIndex = i
			}
		}
	}

	if len(existingParts) != len(newParts) {
		return nil
	}

	if firstWildcardIndex == -1 {
		return nil
	}

	existingSegment := existingParts[firstWildcardIndex]
	newSegment := newParts[firstWildcardIndex]
	existingKind := classifySegment(existingSegment)
	newKind := classifySegment(newSegment)

	if mode == PathConflictModePreferStatic && isStaticParamSibling(existingKind, newKind) {
		return nil
	}

	reason := "static segment conflicts with wildcard segment"
	if existingKind == segmentParam && newKind == segmentParam {
		reason = "wildcard segment conflicts with existing route"
	}

	return &routeConflict{
		reason:          reason,
		index:           firstWildcardIndex,
		existingSegment: existingSegment,
		newSegment:      newSegment,
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
