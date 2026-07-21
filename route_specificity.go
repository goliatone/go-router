package router

func sortRoutesBySpecificity(routes []*RouteDefinition) {
	// Order each method independently, then write it back into that method's
	// original slots. The per-method order is derived from route containment,
	// not a comparator: unrelated patterns are not meaningfully comparable and
	// treating them as equal makes comparator-based sorting non-transitive.
	byMethod := make(map[HTTPMethod][]*RouteDefinition)
	for _, route := range routes {
		byMethod[route.Method] = append(byMethod[route.Method], route)
	}
	for method := range byMethod {
		byMethod[method] = stableContainmentOrder(byMethod[method])
	}

	next := make(map[HTTPMethod]int, len(byMethod))
	for index, route := range routes {
		method := route.Method
		routes[index] = byMethod[method][next[method]]
		next[method]++
	}
}

// stableContainmentOrder performs a stable topological sort. If one route
// contains every request accepted by another route, the narrower route must be
// mounted first. The earliest currently eligible declaration is selected at
// each step, minimizing movement that is not required by containment.
func stableContainmentOrder(routes []*RouteDefinition) []*RouteDefinition {
	if len(routes) < 2 {
		return routes
	}

	outgoing := make([][]int, len(routes))
	indegree := make([]int, len(routes))
	for broadIndex, broad := range routes {
		for narrowIndex, narrow := range routes {
			if broadIndex == narrowIndex ||
				!routePatternContains(broad.Path, narrow.Path) ||
				routePatternContains(narrow.Path, broad.Path) {
				continue
			}
			outgoing[narrowIndex] = append(outgoing[narrowIndex], broadIndex)
			indegree[broadIndex]++
		}
	}

	ordered := make([]*RouteDefinition, 0, len(routes))
	emitted := make([]bool, len(routes))
	for len(ordered) < len(routes) {
		next := -1
		for index := range routes {
			if !emitted[index] && indegree[index] == 0 {
				next = index
				break
			}
		}
		if next == -1 {
			// Equivalent patterns can form a cycle. Preserve their declaration
			// order; ordinary conflict handling remains responsible for them.
			for index := range routes {
				if !emitted[index] {
					ordered = append(ordered, routes[index])
				}
			}
			break
		}
		emitted[next] = true
		ordered = append(ordered, routes[next])
		for _, dependent := range outgoing[next] {
			indegree[dependent]--
		}
	}
	return ordered
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
