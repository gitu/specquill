package gitx

import (
	"strings"
	"testing"
)

// Opted-in bundle: a commit regenerates index.md/log.md and carries them in
// the SAME commit; a workspace without the okf_version marker is untouched.
func TestCommitRegeneratesOKF(t *testing.T) {
	m, _ := fixture(t)
	repo, _ := m.Repo("default/w")

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

	// opt in: root index.md with okf_version
	_, err := repo.SaveFile("main", "index.md", "---\nokf_version: \"0.1\"\n---\n\n# Index\n", "")
	if err != nil {
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
	logMd, _, err := repo.File("main", "log.md")
	if err != nil {
		t.Fatal(err)
	}
	// includes the pending commit itself AND prior history
	if !strings.Contains(logMd, "adopt OKF (Jane)") || !strings.Contains(logMd, "update a (Jane)") {
		t.Fatalf("log.md missing entries:\n%s", logMd)
	}

	// next commit refreshes the log and keeps indexes in the same commit
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
	logMd, _, _ = repo.File("main", "log.md")
	if !strings.Contains(logMd, "**Added** add description to a (Jane)") {
		t.Fatalf("log.md missing new entry:\n%s", logMd)
	}
	idx, _, _ = repo.File("main", "index.md")
	if !strings.Contains(idx, "— The A spec.") {
		t.Fatalf("index description not refreshed:\n%s", idx)
	}
}
