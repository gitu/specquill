package gitx

import (
	"path/filepath"
	"testing"

	"specquill/server/internal/config"
)

// mirrorManager returns a Manager with a single remote-less mirror repo,
// inited empty (no clone).
func mirrorManager(t *testing.T) *Repo {
	t.Helper()
	cfg := &config.Config{
		DataDir: filepath.Join(t.TempDir(), "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Repos:   []config.RepoConfig{{ID: "mirror", Mode: config.ReadOnly, DefaultBranch: "main", Mirror: true}},
	}
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	r, _ := m.Repo("default/mirror")
	return r
}

func TestSnapshotMirrorCommitsAndReads(t *testing.T) {
	r := mirrorManager(t)

	files := map[string]string{
		"index.md":          "# Mirror\n",
		"pages/mifid-ii.md": "RTS 22: microsecond timestamps.",
	}
	sha, changed, err := r.SnapshotMirror("import: 2 files", files)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || sha == "" {
		t.Fatalf("first snapshot should be a new commit (changed=%v sha=%q)", changed, sha)
	}

	// content is readable through the normal read path on the default branch
	content, _, err := r.File("main", "pages/mifid-ii.md")
	if err != nil {
		t.Fatal(err)
	}
	if content != "RTS 22: microsecond timestamps." {
		t.Fatalf("mirror read mismatch: %q", content)
	}
	entries, err := r.Tree("main")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("tree is empty after snapshot")
	}
}

func TestSnapshotMirrorIsIdempotent(t *testing.T) {
	r := mirrorManager(t)
	files := map[string]string{"index.md": "same\n"}

	first, changed, err := r.SnapshotMirror("import", files)
	if err != nil || !changed {
		t.Fatalf("first: changed=%v err=%v", changed, err)
	}
	// identical content → no new commit
	second, changed, err := r.SnapshotMirror("import", files)
	if err != nil {
		t.Fatal(err)
	}
	if changed || second != first {
		t.Fatalf("unchanged content should not commit: changed=%v %s→%s", changed, first, second)
	}
	// changed content → new commit, old head becomes the parent
	files["index.md"] = "different\n"
	third, changed, err := r.SnapshotMirror("import", files)
	if err != nil || !changed {
		t.Fatalf("changed content should commit: changed=%v err=%v", changed, err)
	}
	if third == first {
		t.Fatal("changed snapshot reused the old commit")
	}
}

func TestSnapshotMirrorRejectsEmptyAndNonMirror(t *testing.T) {
	r := mirrorManager(t)
	if _, _, err := r.SnapshotMirror("x", nil); err == nil {
		t.Fatal("empty import should be refused")
	}

	// a normal (non-mirror) repo refuses SnapshotMirror
	m, _ := fixture(t)
	w, _ := m.Repo("default/w")
	if _, _, err := w.SnapshotMirror("x", map[string]string{"a.md": "b"}); err == nil {
		t.Fatal("non-mirror repo should refuse SnapshotMirror")
	}
}
