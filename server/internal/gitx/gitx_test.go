package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"specquill/server/internal/config"
)

// fixture creates a bare origin with two files on main and returns a Manager
// with one writable and one readonly repo cloned from it.
func fixture(t *testing.T) (*Manager, string) {
	t.Helper()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	mustRun(t, "", "init", "-b", "main", src)
	mustWrite(t, filepath.Join(src, "specs", "a.md"), "---\ntitle: A\n---\n\n# A\n")
	mustWrite(t, filepath.Join(src, "notes.txt"), "hello\n")
	mustRun(t, src, "add", "-A")
	mustRun(t, src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init")
	origin := filepath.Join(tmp, "origin.git")
	mustRun(t, "", "init", "--bare", origin)
	mustRun(t, src, "push", "-q", origin, "main")

	cfg := &config.Config{
		DataDir: filepath.Join(tmp, "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Repos: []config.RepoConfig{
			{ID: "w", Mode: config.Writable, Remote: origin, DefaultBranch: "main"},
			{ID: "ro", Mode: config.ReadOnly, Remote: origin, DefaultBranch: "main"},
		},
	}
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	return m, origin
}

func mustRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := run(dir, nil, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTreeAndFileReads(t *testing.T) {
	m, _ := fixture(t)
	for _, id := range []string{"default/w", "default/ro"} {
		repo, _ := m.Repo(id)
		entries, err := repo.Tree("")
		if err != nil {
			t.Fatalf("%s tree: %v", id, err)
		}
		if len(entries) != 2 {
			t.Fatalf("%s: want 2 entries, got %v", id, entries)
		}
		content, sha, err := repo.File("", "specs/a.md")
		if err != nil {
			t.Fatalf("%s file: %v", id, err)
		}
		if !strings.Contains(content, "# A") || len(sha) < 40 {
			t.Fatalf("%s: bad content/sha %q %q", id, content, sha)
		}
	}
}

func TestSnapshot(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/ro")
	files, err := repo.Snapshot("")
	if err != nil {
		t.Fatal(err)
	}
	if files["notes.txt"] != "hello\n" {
		t.Fatalf("snapshot mismatch: %#v", files)
	}
}

func TestWorktreeReflectsSavedChanges(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/w")
	wt, err := repo.Worktree("main")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(wt, "specs", "a.md"), "---\ntitle: A2\n---\n\n# A2\n")
	content, _, err := repo.File("main", "specs/a.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "A2") {
		t.Fatalf("worktree read should see uncommitted save, got %q", content)
	}
	// the bare object db still has the committed version
	ro, _ := m.Repo("default/ro")
	content, _, _ = ro.File("main", "specs/a.md")
	if strings.Contains(content, "A2") {
		t.Fatal("readonly clone must not see the writable worktree state")
	}
}

func TestPathTraversalRejected(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/w")
	for _, p := range []string{"../etc/passwd", "/etc/passwd", "a/../../x", ".git/config"} {
		if _, _, err := repo.File("", p); err == nil {
			t.Fatalf("path %q should be rejected", p)
		}
	}
}

func TestReadOnlyRefusesWorktree(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/ro")
	if _, err := repo.Worktree("main"); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("want read-only error, got %v", err)
	}
}

func TestBranches(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/w")
	if err := repo.CreateBranch("feature/x", "main"); err != nil {
		t.Fatal(err)
	}
	branches, err := repo.Branches()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, b := range branches {
		names[b.Name] = true
	}
	if !names["main"] || !names["feature/x"] {
		t.Fatalf("branches: %v", branches)
	}
}
