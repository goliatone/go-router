package router

import "sort"

func sortRoutesBySpecificity(routes []*RouteDefinition) {
	sort.SliceStable(routes, func(i, j int) bool {
		left := routes[i]
		right := routes[j]
		if left.Method != right.Method {
			return false
		}
		return compareRouteSpecificity(left.Path, right.Path) > 0
	})
}

func compareRouteSpecificity(leftPath, rightPath string) int {
	leftParts := splitPathSegments(leftPath)
	rightParts := splitPathSegments(rightPath)

	minLen := len(leftParts)
	if len(rightParts) < minLen {
		minLen = len(rightParts)
	}

	for i := 0; i < minLen; i++ {
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
