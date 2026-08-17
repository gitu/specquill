package gitx

import (
	"strings"
	"testing"
)

// history builds a small branch on the writable fixture: a rename, an
// addition, a deletion and a plain modification, each its own commit.
func history(t *testing.T, repo *Repo) {
	t.Helper()
	if _, err := repo.SaveFile("main", "specs/a.md", "---\ntitle: A2\n---\n\n# A2\n", mustSha(t, repo, "main", "specs/a.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("main", "edit a", "J", "j@t", []string{"specs/a.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveFile("main", "specs/b.md", "---\ntitle: B\n---\n\n# B\n", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("main", "add b", "J", "j@t", []string{"specs/b.md"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.MoveFile("main", "specs/b.md", "specs/c.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("main", "rename b", "J", "j@t", nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteFile("main", "notes.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("main", "drop notes", "J", "j@t", []string{"notes.txt"}); err != nil {
		t.Fatal(err)
	}
}

func TestLogReportsStatusesAndParents(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("w")
	history(t, repo)

	commits, err := repo.Log("main", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 5 { // init + 4
		t.Fatalf("want 5 commits, got %d: %+v", len(commits), commits)
	}
	newest := commits[0]
	if newest.Subject != "drop notes" {
		t.Fatalf("newest subject = %q", newest.Subject)
	}
	if newest.Parent == "" || newest.Parent == newest.SHA {
		t.Fatalf("parent not reported: %+v", newest)
	}
	if newest.Author != "J" || newest.Email != "j@t" || !strings.HasPrefix(newest.Date, "20") {
		t.Fatalf("identity/date: %+v", newest)
	}
	if len(newest.Files) != 1 || newest.Files[0].Status != "D" || newest.Files[0].Path != "notes.txt" {
		t.Fatalf("delete entry: %+v", newest.Files)
	}
	// renames come back as R with both sides (a repo-wide log cannot --follow)
	ren := commits[1]
	if len(ren.Files) != 1 || ren.Files[0].Status != "R" {
		t.Fatalf("rename entry: %+v", ren.Files)
	}
	if ren.Files[0].OldPath != "specs/b.md" || ren.Files[0].Path != "specs/c.md" {
		t.Fatalf("rename paths: %+v", ren.Files[0])
	}
	if commits[2].Files[0].Status != "A" || commits[3].Files[0].Status != "M" {
		t.Fatalf("add/modify statuses: %+v %+v", commits[2].Files, commits[3].Files)
	}
	// the root commit has no parent — the empty-tree baseline case
	if commits[4].Parent != "" {
		t.Fatalf("root commit parent = %q", commits[4].Parent)
	}
}

func TestLogLimitPathspecAndSince(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("w")
	history(t, repo)

	if c, err := repo.Log("main", "", 2, ""); err != nil || len(c) != 2 {
		t.Fatalf("limit: %d commits, err %v", len(c), err)
	}
	// an out-of-range limit falls back to the default rather than erroring
	if c, err := repo.Log("main", "", 9999, ""); err != nil || len(c) != 5 {
		t.Fatalf("limit clamp: %d commits, err %v", len(c), err)
	}
	// pathspec: only commits touching specs/
	c, err := repo.Log("main", "", 0, "specs")
	if err != nil {
		t.Fatal(err)
	}
	for _, cm := range c {
		if cm.Subject == "drop notes" {
			t.Fatalf("pathspec leaked a commit outside it: %+v", cm)
		}
	}
	// free-form dates never reach git's parser
	if _, err := repo.Log("main", "yesterday", 0, ""); err == nil {
		t.Fatal("want error for non-ISO since")
	}
	if _, err := repo.Log("main", "2020-01-01", 0, ""); err != nil {
		t.Fatalf("ISO since: %v", err)
	}
}

func TestDiffCommitIsTwoDotAndHandlesRoot(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("w")
	history(t, repo)
	commits, err := repo.Log("main", "", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	edit := commits[3] // "edit a"
	files, err := repo.DiffCommit(edit.Parent, edit.SHA)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "specs/a.md" || files[0].Status != "M" {
		t.Fatalf("commit diff: %+v", files)
	}
	if files[0].Additions == 0 || len(files[0].Hunks) == 0 {
		t.Fatalf("numstat/hunks missing: %+v", files[0])
	}
	// root commit: everything is an addition against the empty tree
	root := commits[len(commits)-1]
	rootFiles, err := repo.DiffCommit(root.Parent, root.SHA)
	if err != nil {
		t.Fatal(err)
	}
	if len(rootFiles) != 2 {
		t.Fatalf("root commit files: %+v", rootFiles)
	}
	for _, f := range rootFiles {
		if f.Status != "A" {
			t.Fatalf("root commit file not an addition: %+v", f)
		}
	}
}
