package gitx

import (
	"errors"
	"strings"
	"testing"
)

// protect marks main as protected on the writable fixture repo.
func protect(t *testing.T, m *Manager) *Repo {
	t.Helper()
	repo, _ := m.Repo("w")
	repo.Cfg.ProtectedBranches = []string{"main"}
	return repo
}

func TestProtectedBranchRefusesWrites(t *testing.T) {
	m, _ := fixture(t)
	repo := protect(t, m)
	if _, err := repo.SaveFile("main", "specs/a.md", "x", ""); !errors.Is(err, ErrProtected) {
		t.Fatalf("SaveFile: want ErrProtected, got %v", err)
	}
	if err := repo.DeleteFile("main", "notes.txt"); !errors.Is(err, ErrProtected) {
		t.Fatalf("DeleteFile: want ErrProtected, got %v", err)
	}
	if _, err := repo.Commit("main", "m", "J", "j@t", nil); !errors.Is(err, ErrProtected) {
		t.Fatalf("Commit: want ErrProtected, got %v", err)
	}
	// non-protected branches still writable
	_ = repo.CreateBranch("ws/jane", "main")
	if _, err := repo.SaveFile("ws/jane", "specs/a.md", "x", mustSha(t, repo, "ws/jane", "specs/a.md")); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

func mustSha(t *testing.T, r *Repo, branch, path string) string {
	t.Helper()
	_, sha, err := r.File(branch, path)
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func TestEnsureWorkspaceLifecycle(t *testing.T) {
	m, _ := fixture(t)
	repo := protect(t, m)

	// create
	ws, err := repo.EnsureWorkspace("ws/jane", false)
	if err != nil {
		t.Fatal(err)
	}
	if !ws.Created || ws.State != "current" {
		t.Fatalf("create: %+v", ws)
	}

	// reuse clean+current
	ws, _ = repo.EnsureWorkspace("ws/jane", false)
	if ws.Created || ws.State != "current" {
		t.Fatalf("reuse: %+v", ws)
	}

	// main moves (simulate a merge via commit on unprotected main of a twin repo:
	// here just unprotect temporarily to advance main)
	repo.Cfg.ProtectedBranches = nil
	_, _ = repo.SaveFile("main", "notes.txt", "advance\n", mustSha(t, repo, "main", "notes.txt"))
	if _, err := repo.Commit("main", "advance main", "J", "j@t", nil); err != nil {
		t.Fatal(err)
	}
	repo.Cfg.ProtectedBranches = []string{"main"}

	// clean + behind → fast-forwarded
	ws, err = repo.EnsureWorkspace("ws/jane", false)
	if err != nil {
		t.Fatal(err)
	}
	if ws.State != "current" {
		t.Fatalf("expected ff to current, got %+v", ws)
	}
	content, _, _ := repo.File("ws/jane", "notes.txt")
	if content != "advance\n" {
		t.Fatalf("ws should carry main's advance, got %q", content)
	}

	// ahead (own commit) → never reset
	_, _ = repo.SaveFile("ws/jane", "notes.txt", "mine\n", mustSha(t, repo, "ws/jane", "notes.txt"))
	if _, err := repo.Commit("ws/jane", "own work", "J", "j@t", nil); err != nil {
		t.Fatal(err)
	}
	ws, _ = repo.EnsureWorkspace("ws/jane", false)
	if ws.State != "ahead" {
		t.Fatalf("want ahead, got %+v", ws)
	}

	// diverged (main moves again)
	repo.Cfg.ProtectedBranches = nil
	_, _ = repo.SaveFile("main", "specs/a.md", "---\ntitle: A\n---\n\n# A3\n", mustSha(t, repo, "main", "specs/a.md"))
	_, _ = repo.Commit("main", "more main", "J", "j@t", nil)
	repo.Cfg.ProtectedBranches = []string{"main"}
	ws, _ = repo.EnsureWorkspace("ws/jane", false)
	if ws.State != "diverged" {
		t.Fatalf("want diverged, got %+v", ws)
	}

	// dirty → reused untouched
	_ = repo.CreateBranch("ws/bob", "main")
	_, _ = repo.SaveFile("ws/bob", "notes.txt", "wip\n", mustSha(t, repo, "ws/bob", "notes.txt"))
	ws, _ = repo.EnsureWorkspace("ws/bob", false)
	if ws.State != "dirty" {
		t.Fatalf("want dirty, got %+v", ws)
	}
	content, _, _ = repo.File("ws/bob", "notes.txt")
	if content != "wip\n" {
		t.Fatal("dirty workspace must be reused untouched")
	}
}

func TestEnsureWorkspaceRejectsProtectedName(t *testing.T) {
	m, _ := fixture(t)
	repo := protect(t, m)
	if _, err := repo.EnsureWorkspace("main", false); !errors.Is(err, ErrProtected) {
		t.Fatalf("want ErrProtected, got %v", err)
	}
}

func TestPullFastForwardAndRefusals(t *testing.T) {
	m, origin := fixture(t)
	repo, _ := m.Repo("w")

	// advance origin directly (another clone pushes)
	tmp := t.TempDir()
	mustRun(t, "", "clone", "-b", "main", origin, tmp)
	mustWrite(t, tmp+"/notes.txt", "from origin\n")
	mustRun(t, tmp, "-c", "user.name=o", "-c", "user.email=o@t", "commit", "-am", "origin edit")
	mustRun(t, tmp, "push", "-q", "origin", "main")

	head, updated, err := repo.Pull("main")
	if err != nil || !updated {
		t.Fatalf("pull: %v updated=%v", err, updated)
	}
	content, _, _ := repo.File("main", "notes.txt")
	if content != "from origin\n" {
		t.Fatalf("pull content: %q", content)
	}
	if _, updated, _ = repo.Pull("main"); updated {
		t.Fatal("second pull should be a no-op")
	}
	_ = head

	// dirty worktree refuses
	_, _ = repo.SaveFile("main", "notes.txt", "local wip\n", mustSha(t, repo, "main", "notes.txt"))
	mustWrite(t, tmp+"/notes.txt", "origin again\n")
	mustRun(t, tmp, "-c", "user.name=o", "-c", "user.email=o@t", "commit", "-am", "origin edit 2")
	mustRun(t, tmp, "push", "-q", "origin", "main")
	if _, _, err := repo.Pull("main"); !errors.Is(err, ErrDirtyWorktree) {
		t.Fatalf("want ErrDirtyWorktree, got %v", err)
	}
	// clean up dirt, then diverge locally
	_, _ = repo.SaveFile("main", "notes.txt", "hello\n", mustSha(t, repo, "main", "notes.txt"))
	if _, err := repo.Commit("main", "local commit", "J", "j@t", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.Pull("main"); !errors.Is(err, ErrDiverged) {
		t.Fatalf("want ErrDiverged, got %v", err)
	}
}

func TestDiffWorktreeIncludesUntracked(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("w")
	if _, err := repo.SaveFile("main", "specs/new-doc.md", "# New\n\nline two\n", ""); err != nil {
		t.Fatal(err)
	}
	files, err := repo.DiffWorktree("main")
	if err != nil {
		t.Fatal(err)
	}
	var hit *DiffFile
	for i := range files {
		if files[i].Path == "specs/new-doc.md" {
			hit = &files[i]
		}
	}
	if hit == nil || hit.Status != "A" || hit.Additions != 3 || len(hit.Hunks) != 1 {
		t.Fatalf("untracked diff: %+v", hit)
	}
	if hit.Hunks[0].Lines[0].Op != "+" || !strings.Contains(hit.Hunks[0].Lines[0].Text, "# New") {
		t.Fatalf("untracked hunk lines: %+v", hit.Hunks[0].Lines)
	}
}

func TestStatusBehindDefault(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("w")
	_ = repo.CreateBranch("feature/bd", "main")
	_, _ = repo.SaveFile("main", "notes.txt", "move main\n", mustSha(t, repo, "main", "notes.txt"))
	if _, err := repo.Commit("main", "advance", "J", "j@t", nil); err != nil {
		t.Fatal(err)
	}
	st, err := repo.Status("feature/bd")
	if err != nil {
		t.Fatal(err)
	}
	if st.BehindDefault != 1 {
		t.Fatalf("want behindDefault=1, got %d", st.BehindDefault)
	}
	stMain, _ := repo.Status("main")
	if stMain.BehindDefault != 0 {
		t.Fatalf("main behindDefault must be 0, got %d", stMain.BehindDefault)
	}
}

func TestFileAtIgnoresWorktree(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("w")
	_, _ = repo.SaveFile("main", "notes.txt", "uncommitted\n", mustSha(t, repo, "main", "notes.txt"))
	head, _, err := repo.FileAt("main", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if head != "hello\n" {
		t.Fatalf("FileAt must return committed content, got %q", head)
	}
	wt, _, _ := repo.File("main", "notes.txt")
	if wt != "uncommitted\n" {
		t.Fatalf("File must return worktree content, got %q", wt)
	}
}
