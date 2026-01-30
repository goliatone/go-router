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

func detectPathConflict(existingPath, newPath string) *routeConflict {
	existingParts := splitPathSegments(existingPath)
	newParts := splitPathSegments(newPath)

	minLen := len(existingParts)
	if len(newParts) < minLen {
		minLen = len(newParts)
	}

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

		if existingKind == segmentParam && newKind == segmentParam {
			if i == len(existingParts)-1 && i == len(newParts)-1 {
				return &routeConflict{
					reason:          "wildcard segment conflicts with existing route",
					index:           i,
					existingSegment: existingSegment,
					newSegment:      newSegment,
				}
			}
			continue
		}

		if existingKind == segmentParam || newKind == segmentParam {
			return &routeConflict{
				reason:          "static segment conflicts with wildcard segment",
				index:           i,
				existingSegment: existingSegment,
				newSegment:      newSegment,
			}
		}
	}

	return nil
}

func newRouteConflictError(method HTTPMethod, path string, conflict *routeConflict, policy HTTPRouterConflictPolicy) error {
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
