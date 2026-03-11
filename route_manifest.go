package router

import "sort"

type RouteManifestEntry struct {
	Method HTTPMethod `json:"method"`
	Path   string     `json:"path"`
	Name   string     `json:"name"`
}

type RouteManifestChange struct {
	Identity string             `json:"identity"`
	Before   RouteManifestEntry `json:"before"`
	After    RouteManifestEntry `json:"after"`
}

type RouteManifestDiff struct {
	Added   []RouteManifestEntry  `json:"added"`
	Removed []RouteManifestEntry  `json:"removed"`
	Changed []RouteManifestChange `json:"changed"`
}

func BuildRouteManifest(routes []RouteDefinition) []RouteManifestEntry {
	return buildRouteManifest(routes, false)
}

// BuildRouteManifestWithInternalNames includes runtime/helper names in the Name field.
// This is intended for debugging and internal router introspection rather than public API snapshots.
func BuildRouteManifestWithInternalNames(routes []RouteDefinition) []RouteManifestEntry {
	return buildRouteManifest(routes, true)
}

func buildRouteManifest(routes []RouteDefinition, includeInternalNames bool) []RouteManifestEntry {
	manifest := make([]RouteManifestEntry, len(routes))
	for i, route := range routes {
		name := route.effectivePublicName()
		if includeInternalNames && route.Name != "" {
			name = route.Name
		}
		manifest[i] = RouteManifestEntry{
			Method: route.Method,
			Path:   route.Path,
			Name:   name,
		}
	}
	sortRouteManifestEntries(manifest)
	return manifest
}

func BuildRouterManifest(r interface{ Routes() []RouteDefinition }) []RouteManifestEntry {
	if r == nil {
		return nil
	}
	return BuildRouteManifest(r.Routes())
}

// BuildRouterManifestWithInternalNames includes runtime/helper names in the Name field.
func BuildRouterManifestWithInternalNames(r interface{ Routes() []RouteDefinition }) []RouteManifestEntry {
	if r == nil {
		return nil
	}
	return BuildRouteManifestWithInternalNames(r.Routes())
}

func DiffRouteManifests(before, after []RouteManifestEntry) RouteManifestDiff {
	beforeManifest := append([]RouteManifestEntry(nil), before...)
	afterManifest := append([]RouteManifestEntry(nil), after...)
	sortRouteManifestEntries(beforeManifest)
	sortRouteManifestEntries(afterManifest)

	beforeManifest, afterManifest = removeMatchedManifestEntries(beforeManifest, afterManifest)

	changed := make([]RouteManifestChange, 0)
	usedBefore := make(map[int]struct{})
	usedAfter := make(map[int]struct{})

	beforeByName := uniqueManifestIndexesByName(beforeManifest)
	afterByName := uniqueManifestIndexesByName(afterManifest)

	for name, beforeIdx := range beforeByName {
		afterIdx, ok := afterByName[name]
		if !ok {
			continue
		}

		beforeEntry := beforeManifest[beforeIdx]
		afterEntry := afterManifest[afterIdx]
		if beforeEntry == afterEntry {
			usedBefore[beforeIdx] = struct{}{}
			usedAfter[afterIdx] = struct{}{}
			continue
		}

		changed = append(changed, RouteManifestChange{
			Identity: name,
			Before:   beforeEntry,
			After:    afterEntry,
		})
		usedBefore[beforeIdx] = struct{}{}
		usedAfter[afterIdx] = struct{}{}
	}

	added := make([]RouteManifestEntry, 0)
	for i, entry := range afterManifest {
		if _, ok := usedAfter[i]; ok {
			continue
		}
		added = append(added, entry)
	}

	removed := make([]RouteManifestEntry, 0)
	for i, entry := range beforeManifest {
		if _, ok := usedBefore[i]; ok {
			continue
		}
		removed = append(removed, entry)
	}

	sort.Slice(changed, func(i, j int) bool {
		if changed[i].Identity != changed[j].Identity {
			return changed[i].Identity < changed[j].Identity
		}
		if changed[i].After.Path != changed[j].After.Path {
			return changed[i].After.Path < changed[j].After.Path
		}
		if changed[i].After.Method != changed[j].After.Method {
			return changed[i].After.Method < changed[j].After.Method
		}
		return changed[i].After.Name < changed[j].After.Name
	})

	return RouteManifestDiff{
		Added:   added,
		Removed: removed,
		Changed: changed,
	}
}

func removeMatchedManifestEntries(before, after []RouteManifestEntry) ([]RouteManifestEntry, []RouteManifestEntry) {
	afterCounts := make(map[RouteManifestEntry]int, len(after))
	for _, entry := range after {
		afterCounts[entry]++
	}

	filteredBefore := make([]RouteManifestEntry, 0, len(before))
	for _, entry := range before {
		if afterCounts[entry] > 0 {
			afterCounts[entry]--
			continue
		}
		filteredBefore = append(filteredBefore, entry)
	}

	beforeCounts := make(map[RouteManifestEntry]int, len(before))
	for _, entry := range before {
		beforeCounts[entry]++
	}

	filteredAfter := make([]RouteManifestEntry, 0, len(after))
	for _, entry := range after {
		if beforeCounts[entry] > 0 {
			beforeCounts[entry]--
			continue
		}
		filteredAfter = append(filteredAfter, entry)
	}

	return filteredBefore, filteredAfter
}

func sortRouteManifestEntries(entries []RouteManifestEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		if entries[i].Method != entries[j].Method {
			return entries[i].Method < entries[j].Method
		}
		return entries[i].Name < entries[j].Name
	})
}

func uniqueManifestIndexesByName(entries []RouteManifestEntry) map[string]int {
	indexes := make(map[string]int)
	duplicates := make(map[string]struct{})

	for i, entry := range entries {
		if entry.Name == "" {
			continue
		}
		if _, dup := duplicates[entry.Name]; dup {
			continue
		}
		if _, exists := indexes[entry.Name]; exists {
			delete(indexes, entry.Name)
			duplicates[entry.Name] = struct{}{}
			continue
		}
		indexes[entry.Name] = i
	}

	return indexes
}
