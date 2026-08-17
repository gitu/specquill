package gitx

import (
	"strings"
	"testing"
)

// Opted-in bundle: a commit regenerates the index.md listings and carries
// them in the SAME commit; log.md is never materialized (it is generated on
// the fly at bundle export) and a stale committed one is retired. A
// workspace without the okf_version marker is untouched.
func TestCommitRegeneratesOKF(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("w")

	// not opted in: commit must not invent reserved files
	_, sha, _ := repo.File("main", "specs/a.md")
	if _, err := repo.SaveFile("main", "specs/a.md", "---\ntitle: A\n---\n\n# A v2\n", sha); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("main", "update a", "Jane", "jane@t", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.File("main", "index.md"); err == nil {
		t.Fatal("index.md generated without opt-in")
	}

	// opt in: root index.md with okf_version — plus a stale log.md as an
	// older producer version would have committed it
	if _, err := repo.SaveFile("main", "index.md", "---\nokf_version: \"0.1\"\n---\n\n# Index\n", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveFile("main", "log.md", "# Log\n\n- stale entry\n", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("main", "adopt OKF", "Jane", "jane@t", nil); err != nil {
		t.Fatal(err)
	}

	// the opt-in commit itself already regenerated the derived files
	idx, _, err := repo.File("main", "index.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(idx, "okf_version") || !strings.Contains(idx, "(specs/a.md)") {
		t.Fatalf("root index not regenerated:\n%s", idx)
	}
	dirIdx, _, err := repo.File("main", "specs/index.md")
	if err != nil || !strings.HasPrefix(dirIdx, "# specs\n") {
		t.Fatalf("specs/index.md missing: %v\n%s", err, dirIdx)
	}
	// log.md is a bundle-export artifact now — the stale one must be gone
	if logMd, _, err := repo.File("main", "log.md"); err == nil {
		t.Fatalf("log.md materialized in the tree:\n%s", logMd)
	}

	// next commit keeps indexes in the same commit, worktree stays clean
	_, sha2, _ := repo.File("main", "specs/a.md")
	if _, err := repo.SaveFile("main", "specs/a.md", "---\ntitle: A\ndescription: The A spec.\n---\n\n# A v3\n", sha2); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("main", "add description to a", "Jane", "jane@t", nil); err != nil {
		t.Fatal(err)
	}
	st, err := repo.Status("main")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Dirty) != 0 {
		t.Fatalf("derived files left uncommitted: %v", st.Dirty)
	}
	idx, _, _ = repo.File("main", "index.md")
	if !strings.Contains(idx, "— The A spec.") {
		t.Fatalf("index description not refreshed:\n%s", idx)
	}

	// the on-the-fly log covers the whole history, newest first
	entries, err := repo.OKFLogEntries("main", "")
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, e := range entries {
		subjects = append(subjects, e.Subject)
	}
	joined := strings.Join(subjects, "\n")
	for _, want := range []string{"add description to a", "adopt OKF", "update a"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("log entries missing %q:\n%s", want, joined)
		}
	}
	if subjects[0] != "add description to a" {
		t.Fatalf("log not newest-first: %v", subjects)
	}
}
