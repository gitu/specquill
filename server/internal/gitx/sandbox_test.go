package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTenantRootFor(t *testing.T) {
	s := &sandbox{tenantsRoot: filepath.FromSlash("/data/tenants")}
	cases := map[string]string{
		"/data/tenants/acme/repo1/git":             "/data/tenants/acme",
		"/data/tenants/acme/repo1/worktrees/ws__x": "/data/tenants/acme",
		"/data/tenants/acme":                       "/data/tenants/acme",
		"/data/tenants/beta/r/git":                 "/data/tenants/beta",
		"":                                         "", // version check
		"/tmp/external-origin":                     "", // outside the tree
		"/data/tenants":                            "", // the tenants root itself
		"/data/other/x":                            "", // sibling of tenants
	}
	for in, want := range cases {
		got := s.tenantRootFor(filepath.FromSlash(in))
		if want != "" {
			want = filepath.FromSlash(want)
		}
		if got != want {
			t.Errorf("tenantRootFor(%q) = %q, want %q", in, got, want)
		}
	}
}

// requireSandbox skips when bubblewrap or unprivileged user namespaces are
// unavailable (CI without the package, restricted kernels).
func requireSandbox(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bubblewrap not installed")
	}
	// probe: a trivial jailed command must succeed, else userns is disabled
	if err := exec.Command("bwrap", "--dev-bind", "/", "/", "--die-with-parent", "--", "true").Run(); err != nil {
		t.Skipf("bubblewrap cannot create a namespace here: %v", err)
	}
}

// With the sandbox on, a git process for tenant A must not be able to reach
// tenant B's files. git hash-object reads an arbitrary path and prints its
// hash — jailed, B is masked and the read fails; unjailed, it succeeds. This
// asserts both directions so the test proves the jail is what blocks it.
func TestSandboxMasksSiblingTenant(t *testing.T) {
	requireSandbox(t)
	dataDir := t.TempDir()
	tenants := filepath.Join(dataDir, "tenants")
	aGit := filepath.Join(tenants, "a", "repo", "git")
	bDir := filepath.Join(tenants, "b")
	if err := os.MkdirAll(aGit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bSecret := filepath.Join(bDir, "secret.txt")
	if err := os.WriteFile(bSecret, []byte("tenant B private\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// baseline: no sandbox → A's git can hash B's file
	activeSandbox = nil
	if _, err := run(aGit, nil, "hash-object", bSecret); err != nil {
		t.Fatalf("baseline (unjailed) hash-object of B should succeed: %v", err)
	}

	// jailed: B is masked by the tmpfs, the read must fail
	activeSandbox = &sandbox{mode: "bwrap", tenantsRoot: tenants}
	defer func() { activeSandbox = nil }()
	if _, err := run(aGit, nil, "hash-object", bSecret); err == nil {
		t.Fatal("sandboxed git for tenant A could read tenant B's file — isolation breach")
	}

	// same-tenant ops still work inside the jail: A's own file is reachable
	aFile := filepath.Join(tenants, "a", "repo", "own.txt")
	if err := os.WriteFile(aFile, []byte("tenant A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(aGit, nil, "hash-object", aFile); err != nil {
		t.Fatalf("sandboxed git for tenant A should read its OWN file: %v", err)
	}
}

// A full clone→commit→worktree→read cycle must still work with the sandbox on,
// proving the jail doesn't break normal same-tenant git operations, worktree
// path pointers, or GIT_INDEX_FILE.
func TestSandboxPreservesGitOps(t *testing.T) {
	requireSandbox(t)
	activeSandbox = nil
	m, _ := fixture(t) // clones a writable repo "w" under <dataDir>/tenants/default
	// turn the jail on for the manager's data dir and re-run real operations
	activeSandbox = &sandbox{mode: "bwrap", tenantsRoot: filepath.Join(m.dataDir, "tenants")}
	defer func() { activeSandbox = nil }()

	repo, ok := m.Repo("default/w")
	if !ok {
		t.Fatal("fixture repo missing")
	}
	// a worktree write + commit exercises Worktree(), SaveFile, and Commit
	// all under the jail
	if _, err := repo.SaveFile("main", "specs/sandboxed.md", "# jailed\n", ""); err != nil {
		t.Fatalf("SaveFile under sandbox: %v", err)
	}
	sha, err := repo.Commit("main", "add under sandbox", "T", "t@t", nil)
	if err != nil {
		t.Fatalf("Commit under sandbox: %v", err)
	}
	if sha == "" {
		t.Fatal("empty commit sha under sandbox")
	}
	content, _, err := repo.File("main", "specs/sandboxed.md")
	if err != nil || content != "# jailed\n" {
		t.Fatalf("read back under sandbox: %q %v", content, err)
	}
}
