package gitx

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// sandbox jails each git subprocess in a per-tenant mount namespace so a
// tenant's git operation cannot see or touch another tenant's clones. The
// jail root is the tenant dir (<dataDir>/tenants/<slug>): all of a tenant's
// repos share one jail, every sibling tenant is masked.
//
// Isolation is derived from the working directory the git call already runs
// in — every production git invocation has cmd.Dir under
// <dataDir>/tenants/<slug>/... — so no tenant identity has to be threaded
// through the ~65 call sites; the single runFull seam computes it.
type sandbox struct {
	mode        string // "bwrap" (only supported mechanism today)
	tenantsRoot string // <dataDir>/tenants — the dir masked by a tmpfs in the jail
}

// activeSandbox is configured once at Manager construction. nil ⇒ no jailing
// (the default), so dev, CI and self-host run exactly as before.
var activeSandbox *sandbox

// configureSandbox is called by NewManager. It validates availability and,
// unless require is set, degrades to no-op when the mechanism is missing so a
// dev laptop without bubblewrap still runs. It returns an error only when
// require forces a hard failure.
func configureSandbox(dataDir, mode string, require bool) error {
	if mode == "" || mode == "off" {
		activeSandbox = nil
		return nil
	}
	if mode != "bwrap" {
		if require {
			return fmt.Errorf("sandbox mode %q is not implemented", mode)
		}
		activeSandbox = nil
		return nil
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		if require {
			return fmt.Errorf("sandbox mode bwrap requires the bubblewrap binary: %w", err)
		}
		activeSandbox = nil
		return nil
	}
	activeSandbox = &sandbox{mode: "bwrap", tenantsRoot: filepath.Join(dataDir, "tenants")}
	return nil
}

// tenantRootFor returns the jail root (<dataDir>/tenants/<slug>) for a git
// working directory, or "" when dir is empty or outside the tenants tree
// (the version check, and tests that clone from external file: origins) — in
// which case the call runs unjailed.
func (s *sandbox) tenantRootFor(dir string) string {
	if dir == "" {
		return ""
	}
	rel, err := filepath.Rel(s.tenantsRoot, dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	slug := rel
	if i := strings.IndexByte(rel, filepath.Separator); i >= 0 {
		slug = rel[:i]
	}
	return filepath.Join(s.tenantsRoot, slug)
}

// wrapGit turns a plain git invocation into a bubblewrap-jailed one. The mount
// layout binds the whole host read-write for identity paths (git binary, libs,
// /dev, CA certs, network stay reachable), overmounts the tenants tree with a
// tmpfs to hide every tenant, then binds this one tenant back at its real
// absolute path — so worktree pointers and GIT_INDEX_FILE keep resolving
// unchanged. The network namespace is deliberately NOT unshared so fetch/push
// and the credential helper still work.
func (s *sandbox) wrapGit(jailRoot, dir string, gitArgs []string) (name string, argv []string) {
	pre := []string{
		"--dev-bind", "/", "/",
		"--tmpfs", s.tenantsRoot,
		"--bind", jailRoot, jailRoot,
		"--die-with-parent",
	}
	if dir != "" {
		pre = append(pre, "--chdir", dir)
	}
	pre = append(pre, "--", "git")
	return "bwrap", append(pre, gitArgs...)
}
