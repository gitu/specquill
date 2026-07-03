package gitx

import (
	"strings"
	"testing"
)

func TestSaveStatusCommitCycle(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/w")

	// clean at start
	st, err := repo.Status("main")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Dirty) != 0 {
		t.Fatalf("expected clean status, got %v", st.Dirty)
	}

	// save (modify) with correct baseSha
	_, sha, err := repo.File("main", "specs/a.md")
	if err != nil {
		t.Fatal(err)
	}
	newSha, err := repo.SaveFile("main", "specs/a.md", "---\ntitle: A\n---\n\n# A changed\n", sha)
	if err != nil {
		t.Fatal(err)
	}
	if newSha == sha {
		t.Fatal("sha should change after save")
	}

	// stale save rejected
	if _, err := repo.SaveFile("main", "specs/a.md", "x", sha); err != ErrStale {
		t.Fatalf("want ErrStale, got %v", err)
	}

	// create a new file (empty baseSha)
	if _, err := repo.SaveFile("main", "specs/new.md", "# New\n", ""); err != nil {
		t.Fatal(err)
	}

	// status shows M + A
	st, _ = repo.Status("main")
	states := map[string]string{}
	for _, f := range st.Dirty {
		states[f.Path] = f.State
	}
	if states["specs/a.md"] != "M" || states["specs/new.md"] != "A" {
		t.Fatalf("unexpected status %v", states)
	}

	// commit as user; the user is author AND committer, the service identity
	// lands as a Co-authored-by trailer
	commitSha, err := repo.Commit("main", "edit spec", "Jane Doe", "jane@corp.example", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(commitSha) < 40 {
		t.Fatalf("bad commit sha %q", commitSha)
	}
	wt, _ := repo.Worktree("main")
	out, err := run(wt, nil, "log", "-1", "--format=%an|%ae|%cn|%ce")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "Jane Doe|jane@corp.example|Jane Doe|jane@corp.example" {
		t.Fatalf("author/committer mismatch: %q", out)
	}
	body, _ := run(wt, nil, "log", "-1", "--format=%B")
	if !strings.Contains(body, "Co-authored-by: svc <svc@t>") {
		t.Fatalf("missing service co-author trailer: %q", body)
	}

	// clean again + ahead of origin by 1
	st, _ = repo.Status("main")
	if len(st.Dirty) != 0 {
		t.Fatalf("expected clean after commit, got %v", st.Dirty)
	}
	if st.Ahead != 1 {
		t.Fatalf("want ahead=1, got %d", st.Ahead)
	}
}

func TestPushFetch(t *testing.T) {
	m, origin := fixture(t)
	repo, _ := m.Repo("default/w")
	_, sha, _ := repo.File("main", "notes.txt")
	if _, err := repo.SaveFile("main", "notes.txt", "hello world\n", sha); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("main", "update notes", "Jane", "j@t", nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push("main"); err != nil {
		t.Fatal(err)
	}
	// origin got the commit
	out, err := run(origin, nil, "log", "-1", "--format=%s", "main")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "update notes" {
		t.Fatalf("origin log: %q", out)
	}
	// after push, ahead/behind is 0/0
	st, _ := repo.Status("main")
	if st.Ahead != 0 || st.Behind != 0 {
		t.Fatalf("want 0/0 after push, got %d/%d", st.Ahead, st.Behind)
	}
	// readonly clone sees it after fetch
	ro, _ := m.Repo("default/ro")
	if err := ro.Fetch(); err != nil {
		t.Fatal(err)
	}
	content, _, _ := ro.File("main", "notes.txt")
	if content != "hello world\n" {
		t.Fatalf("readonly after fetch: %q", content)
	}
}

func TestDeleteFile(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/w")
	if err := repo.DeleteFile("main", "notes.txt"); err != nil {
		t.Fatal(err)
	}
	st, _ := repo.Status("main")
	if len(st.Dirty) != 1 || st.Dirty[0].State != "D" {
		t.Fatalf("want one deleted file, got %v", st.Dirty)
	}
}
