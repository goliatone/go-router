package router

import (
	"reflect"
	"strings"
	"testing"
)

func legacySplitPathSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func TestPathCursorPreservesSegmentsAndContainment(t *testing.T) {
	paths := []string{"", "/", "//", "a", "/a/", "//a//", "/a//b", "/a/:id", "/a/:id<int>", "/a/*rest", "/:a/b", "/:a/*rest", "/*rest", "/a/*rest/b", "/a/:id/b", "/a//:id/", "/a//b/"}
	for _, path := range paths {
		parts := splitPathSegments(path)
		var got []string
		for parts.remaining > 0 {
			got = append(got, parts.next())
		}
		if want := legacySplitPathSegments(path); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: got %q, want %q", path, got, want)
		}
	}
	for _, left := range paths {
		for _, right := range paths {
			for _, distinct := range []bool{false, true} {
				semantics := RouteMatchingSemantics{TrailingSlashDistinct: distinct}
				got := routePatternContainsWithSemantics(left, right, semantics)
				want := legacyRoutePatternContainsWithSemantics(left, right, semantics)
				if got != want {
					t.Fatalf("contains(%q, %q), distinct=%v: got %v, want %v", left, right, distinct, got, want)
				}
			}
		}
	}
}

func TestRoutePathComparisonsDoNotAllocate(t *testing.T) {
	left := &RouteDefinition{Method: GET, Path: "/workbench/projects/:project/context/adrs/:id<int>"}
	right := &RouteDefinition{Method: GET, Path: "/workbench/projects/:project/context/adrs/new"}
	allocs := testing.AllocsPerRun(100, func() {
		_ = detectPathConflict(left.Path, right.Path, PathConflictModePreferStatic, false)
		_ = compareRouteSpecificity(left.Path, right.Path)
		_ = routePatternContains(left.Path, right.Path)
		_ = detectBareIDParamLint(left, right)
		_ = containsCatchAll(left.Path)
		_ = containsParameter(left.Path)
	})
	if allocs != 0 {
		t.Fatalf("route path comparisons allocated %g times", allocs)
	}
}

func legacyRoutePatternContainsWithSemantics(earlierPath, candidatePath string, semantics RouteMatchingSemantics) bool {
	if semantics.TrailingSlashDistinct && legacyHasTrailingSlash(earlierPath) != legacyHasTrailingSlash(candidatePath) {
		return false
	}
	earlier := legacySplitPathSegments(earlierPath)
	candidate := legacySplitPathSegments(candidatePath)

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

func legacyHasTrailingSlash(value string) bool {
	return len(value) > 1 && strings.HasSuffix(value, "/")
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
