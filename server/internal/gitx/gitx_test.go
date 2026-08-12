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

// pushOrigin commits every pending change in the fixture's src clone and
// pushes refspec to origin.
func pushOrigin(t *testing.T, origin, refspec string) {
	t.Helper()
	src := filepath.Join(filepath.Dir(origin), "src")
	mustRun(t, src, "add", "-A")
	mustRun(t, src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "remote change")
	mustRun(t, src, "push", "-q", origin, refspec)
}

func TestBranchesListsRemoteOnly(t *testing.T) {
	m, origin := fixture(t)
	repo, _ := m.Repo("default/w")

	// a branch born on origin after the clone is remote-only until switched to
	pushOrigin(t, origin, "main:feature/remote-only")
	if err := repo.Fetch(); err != nil {
		t.Fatal(err)
	}
	branches, err := repo.Branches()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Branch{}
	for _, b := range branches {
		byName[b.Name] = b
	}
	if b, ok := byName["feature/remote-only"]; !ok || !b.IsRemote {
		t.Fatalf("want remote-only feature/remote-only, got %v", branches)
	}
	if byName["main"].IsRemote {
		t.Fatal("local main must not be marked remote")
	}

	// materializing the local branch hides the remote-only entry
	if err := repo.CreateBranch("feature/remote-only", "origin/feature/remote-only"); err != nil {
		t.Fatal(err)
	}
	branches, err = repo.Branches()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range branches {
		if b.Name == "feature/remote-only" && b.IsRemote {
			t.Fatalf("materialized branch still listed as remote: %v", branches)
		}
	}
}

func TestFFBranches(t *testing.T) {
	m, origin := fixture(t)
	repo, _ := m.Repo("default/w")
	src := filepath.Join(filepath.Dir(origin), "src")

	mustWrite(t, filepath.Join(src, "notes.txt"), "hello v2\n")
	pushOrigin(t, origin, "main")
	if err := repo.Fetch(); err != nil {
		t.Fatal(err)
	}

	// a hold veto keeps the branch where it is
	if updated := repo.FFBranches(func(branch string) bool { return branch == "main" }); len(updated) != 0 {
		t.Fatalf("held branch was moved: %v", updated)
	}

	updated := repo.FFBranches(nil)
	if len(updated) != 1 || updated[0] != "main" {
		t.Fatalf("want main ff'd, got %v", updated)
	}
	content, _, err := repo.File("main", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "v2") {
		t.Fatalf("worktree not synced to origin state: %q", content)
	}
	if updated := repo.FFBranches(nil); len(updated) != 0 {
		t.Fatalf("second run must be a no-op, got %v", updated)
	}
}

func TestFFBranchesSkipsDivergedAndDirty(t *testing.T) {
	m, origin := fixture(t)
	repo, _ := m.Repo("default/w")
	src := filepath.Join(filepath.Dir(origin), "src")

	// diverged: local commit on main while origin moves too
	if _, err := repo.SaveFile("main", "local.md", "# local\n", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("main", "local work", "Jane", "j@t", nil); err != nil {
		t.Fatal(err)
	}
	localHead, _ := repo.Head("main")
	mustWrite(t, filepath.Join(src, "notes.txt"), "remote v2\n")
	pushOrigin(t, origin, "main")
	if err := repo.Fetch(); err != nil {
		t.Fatal(err)
	}
	if updated := repo.FFBranches(nil); len(updated) != 0 {
		t.Fatalf("diverged branch was moved: %v", updated)
	}
	if head, _ := repo.Head("main"); head != localHead {
		t.Fatalf("diverged main moved from %s to %s", localHead, head)
	}

	// dirty: uncommitted worktree change on a strictly-behind branch — wip
	// starts at origin's main head, then origin's wip moves one commit ahead
	if err := repo.CreateBranch("wip", "origin/main"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(src, "notes.txt"), "remote v3\n")
	pushOrigin(t, origin, "main:wip")
	if _, err := repo.SaveFile("wip", "draft.md", "# draft\n", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.Fetch(); err != nil {
		t.Fatal(err)
	}
	if updated := repo.FFBranches(nil); len(updated) != 0 {
		t.Fatalf("dirty branch was moved: %v", updated)
	}
	content, _, err := repo.File("wip", "draft.md")
	if err != nil || !strings.Contains(content, "# draft") {
		t.Fatalf("uncommitted draft lost: %q %v", content, err)
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
