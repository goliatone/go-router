package router

import "sort"

func sortRoutesBySpecificity(routes []*RouteDefinition) {
	// A comparator that returns false for routes with different methods is not
	// transitive when methods are interleaved. Sort each method independently,
	// then write it back into that method's original slots. This preserves the
	// cross-method layout while guaranteeing specificity within every method.
	byMethod := make(map[HTTPMethod][]*RouteDefinition)
	for _, route := range routes {
		byMethod[route.Method] = append(byMethod[route.Method], route)
	}
	for method := range byMethod {
		sort.SliceStable(byMethod[method], func(i, j int) bool {
			return compareRouteSpecificity(byMethod[method][i].Path, byMethod[method][j].Path) > 0
		})
	}

	next := make(map[HTTPMethod]int, len(byMethod))
	for index, route := range routes {
		method := route.Method
		routes[index] = byMethod[method][next[method]]
		next[method]++
	}
}

func compareRouteSpecificity(leftPath, rightPath string) int {
	leftParts := splitPathSegments(leftPath)
	rightParts := splitPathSegments(rightPath)

	minLen := min(len(rightParts), len(leftParts))

	for i := range minLen {
		leftSegment := leftParts[i]
		rightSegment := rightParts[i]
		leftKind := classifySegment(leftSegment)
		rightKind := classifySegment(rightSegment)

		if leftKind != rightKind {
			return compareSegmentKind(leftKind, rightKind)
		}

		if leftKind == segmentStatic && leftSegment != rightSegment {
			return 0
		}
	}

	if len(leftParts) != len(rightParts) {
		if len(leftParts) > len(rightParts) {
			return 1
		}
		return -1
	}

	return 0
}

func compareSegmentKind(left, right segmentKind) int {
	if left == right {
		return 0
	}
	if left == segmentStatic {
		return 1
	}
	if right == segmentStatic {
		return -1
	}
	if left == segmentParam {
		return 1
	}
	return -1
}
