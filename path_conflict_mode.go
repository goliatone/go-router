package router

// PathConflictMode controls how static and wildcard path segments are treated.
type PathConflictMode string

const (
	// PathConflictModeStrict preserves current behavior and treats static/param siblings as conflicts.
	PathConflictModeStrict PathConflictMode = "strict"
	// PathConflictModePreferStatic allows static/param siblings and relies on specificity ordering.
	PathConflictModePreferStatic PathConflictMode = "prefer_static"
)

func (m PathConflictMode) normalize() PathConflictMode {
	switch m {
	case PathConflictModePreferStatic:
		return PathConflictModePreferStatic
	default:
		return PathConflictModeStrict
	}
}

func (m PathConflictMode) String() string {
	return string(m.normalize())
}
