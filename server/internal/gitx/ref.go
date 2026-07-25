package gitx

import "strings"

// ValidRef reports whether name is safe to hand to git as a ref.
//
// It implements the parts of git-check-ref-format(1) that matter for safety
// rather than for git's own bookkeeping. Two of them are load-bearing:
//
//   - a leading "-" would be read by git as an option, not a ref (argument
//     injection: `git diff --output=…`); CreateBranch is already protected by
//     a "--" separator, but refs reach plenty of commands that have none.
//   - ".." and path separators reach filesystem paths through slug(), which
//     maps worktree directories under the repo's worktree root.
//
// Callers should validate any ref that came from a request before it travels
// further, so the guarantee is local instead of resting on git's own
// refname rules three layers down.
func ValidRef(name string) bool {
	if name == "" || len(name) > 255 {
		return false
	}
	if strings.HasPrefix(name, "-") || name == "@" {
		return false
	}
	if strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.Contains(name, "//") {
		return false
	}
	if strings.Contains(name, "..") || strings.Contains(name, "@{") {
		return false
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, ".lock") {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r == 0x7f { // control characters and space
			return false
		}
		switch r {
		case '~', '^', ':', '?', '*', '[', '\\':
			return false
		}
	}
	// no path component may start with a dot (".git", ".." already covered)
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}
