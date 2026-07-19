// Package authz holds the per-repo role ladder (REQ-021) — the single
// vocabulary for every authorization decision: tenant roles, derived GitHub
// roles and explicit repo grants all speak these four levels.
package authz

// Role is one rung of the ladder: viewer < editor < maintainer < admin.
// The zero value None means no access; roles compare with plain <, >.
type Role int

const (
	None Role = iota
	// Viewer reads everything and comments on PRs.
	Viewer
	// Editor writes workspace branches, commits, opens/approves/closes PRs,
	// co-edits and uses the copilot.
	Editor
	// Maintainer merges PRs into protected branches and manages share links.
	Maintainer
	// Admin manages the repo's grants and settings; the tenant-level admin
	// role additionally gates tenant management and derives repo admin
	// everywhere.
	Admin
)

var names = map[Role]string{Viewer: "viewer", Editor: "editor", Maintainer: "maintainer", Admin: "admin"}

// Parse maps a stored role string onto the ladder; anything unknown
// (including "") is None — unknown strings must never grant access.
func Parse(s string) Role {
	switch s {
	case "viewer":
		return Viewer
	case "editor":
		return Editor
	case "maintainer":
		return Maintainer
	case "admin":
		return Admin
	}
	return None
}

// String is Parse's inverse; None renders as "".
func (r Role) String() string { return names[r] }

// Max returns the higher rung — how derived roles and explicit grants
// compose (effective role = max(derived, granted), REQ-020/REQ-021).
func Max(a, b Role) Role {
	if a > b {
		return a
	}
	return b
}

// FromGitHub maps a GitHub repo permission onto the ladder (REQ-021.3):
// pull/read → viewer, triage/push/write → editor, maintain → maintainer,
// admin → admin. Unknown permissions grant nothing.
func FromGitHub(permission string) Role {
	switch permission {
	case "admin":
		return Admin
	case "maintain":
		return Maintainer
	case "write", "push", "triage":
		return Editor
	case "read", "pull":
		return Viewer
	}
	return None
}

// ValidGrant reports whether s names a grantable rung (any real role).
func ValidGrant(s string) bool { return Parse(s) != None }
