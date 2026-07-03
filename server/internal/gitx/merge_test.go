package gitx

import (
	"strings"
	"testing"
)

func TestMergeCleanAndDiff(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/w")
	if err := repo.CreateBranch("feature/m", "main"); err != nil {
		t.Fatal(err)
	}
	_, sha, _ := repo.File("feature/m", "specs/a.md")
	if _, err := repo.SaveFile("feature/m", "specs/a.md", "---\ntitle: A\n---\n\n# A v2\n", sha); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("feature/m", "spec v2", "Jane", "j@t", nil); err != nil {
		t.Fatal(err)
	}

	// structured three-dot diff
	files, err := repo.DiffRange("main", "feature/m")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "specs/a.md" || files[0].Additions != 1 || files[0].Deletions != 1 {
		t.Fatalf("diff mismatch: %+v", files)
	}
	if len(files[0].Hunks) == 0 || !strings.Contains(files[0].Hunks[0].Header, "@@") {
		t.Fatalf("missing hunks: %+v", files[0])
	}

	// mergeable
	check, err := repo.CheckMerge("main", "feature/m")
	if err != nil {
		t.Fatal(err)
	}
	if !check.Mergeable {
		t.Fatalf("expected mergeable, got %+v", check)
	}

	// merge with the merging user as author AND committer; the service is a
	// Co-authored-by trailer; two parents
	sha2, res, err := repo.Merge("main", "feature/m", "Merge PR #1", "Rev Iewer", "rev@t", "merge")
	if err != nil || !res.Mergeable {
		t.Fatalf("merge failed: %v %+v", err, res)
	}
	out, _ := run(repo.gitDir, nil, "log", "-1", "--format=%an|%cn|%p", sha2)
	parts := strings.Split(strings.TrimSpace(out), "|")
	if parts[0] != "Rev Iewer" || parts[1] != "Rev Iewer" || len(strings.Fields(parts[2])) != 2 {
		t.Fatalf("merge commit meta: %q", out)
	}
	body, _ := run(repo.gitDir, nil, "log", "-1", "--format=%B", sha2)
	if !strings.Contains(body, "Co-authored-by: svc <svc@t>") {
		t.Fatalf("missing service co-author trailer: %q", body)
	}
	// main worktree (if any) and reads reflect the merge
	content, _, _ := repo.File("main", "specs/a.md")
	if !strings.Contains(content, "A v2") {
		t.Fatalf("main should contain merged content, got %q", content)
	}
}

func TestMergeSquash(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/w")
	_ = repo.CreateBranch("feature/sq", "main")
	_, sha, _ := repo.File("feature/sq", "notes.txt")
	_, _ = repo.SaveFile("feature/sq", "notes.txt", "one\n", sha)
	_, _ = repo.Commit("feature/sq", "c1", "J", "j@t", nil)
	_, sha, _ = repo.File("feature/sq", "notes.txt")
	_, _ = repo.SaveFile("feature/sq", "notes.txt", "one\ntwo\n", sha)
	_, _ = repo.Commit("feature/sq", "c2", "J", "j@t", nil)

	sha2, res, err := repo.Merge("main", "feature/sq", "squashed", "J", "j@t", "squash")
	if err != nil || !res.Mergeable {
		t.Fatalf("squash merge failed: %v", err)
	}
	out, _ := run(repo.gitDir, nil, "log", "-1", "--format=%p", sha2)
	if len(strings.Fields(strings.TrimSpace(out))) != 1 {
		t.Fatalf("squash commit should have a single parent, got %q", out)
	}
}

func TestMergeConflictBlocked(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/w")
	_ = repo.CreateBranch("feature/c", "main")
	// conflicting edits to the same line on both branches
	_, sha, _ := repo.File("feature/c", "notes.txt")
	_, _ = repo.SaveFile("feature/c", "notes.txt", "feature version\n", sha)
	_, _ = repo.Commit("feature/c", "feature edit", "J", "j@t", nil)
	_, sha, _ = repo.File("main", "notes.txt")
	_, _ = repo.SaveFile("main", "notes.txt", "main version\n", sha)
	_, _ = repo.Commit("main", "main edit", "J", "j@t", nil)

	check, err := repo.CheckMerge("main", "feature/c")
	if err != nil {
		t.Fatal(err)
	}
	if check.Mergeable || len(check.Conflicts) == 0 || check.Conflicts[0] != "notes.txt" {
		t.Fatalf("expected notes.txt conflict, got %+v", check)
	}
	_, res, err := repo.Merge("main", "feature/c", "m", "J", "j@t", "merge")
	if err != nil {
		t.Fatal(err)
	}
	if res.Mergeable {
		t.Fatal("conflicting merge must be blocked")
	}
}

func TestMergeRefusedWhenTargetDirty(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/w")
	_ = repo.CreateBranch("feature/d", "main")
	_, sha, _ := repo.File("feature/d", "notes.txt")
	_, _ = repo.SaveFile("feature/d", "notes.txt", "x\n", sha)
	_, _ = repo.Commit("feature/d", "x", "J", "j@t", nil)
	// dirty the target worktree
	_, sha, _ = repo.File("main", "notes.txt")
	_, _ = repo.SaveFile("main", "notes.txt", "uncommitted\n", sha)

	_, _, err := repo.Merge("main", "feature/d", "m", "J", "j@t", "merge")
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("want uncommitted-changes refusal, got %v", err)
	}
}
