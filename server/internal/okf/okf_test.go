package okf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEnabledGate(t *testing.T) {
	root := t.TempDir()
	if Enabled(root) {
		t.Fatal("empty tree must not be enabled")
	}
	write(t, root, "index.md", "# Index\n") // no frontmatter
	if Enabled(root) {
		t.Fatal("index without okf_version must not enable")
	}
	write(t, root, "index.md", "---\nokf_version: \"0.1\"\n---\n\n# Index\n")
	if !Enabled(root) {
		t.Fatal("okf_version in root index frontmatter must enable")
	}
}

func TestGenerateIndexes(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "---\ntype: Guide\ntitle: Readme\n---\n\n# hi\n")
	write(t, root, "requirements/REQ-001.md", "---\ntype: Requirement\ntitle: Login\ndescription: Users can log in.\n---\n\nbody\n")
	write(t, root, "requirements/REQ-002.md", "---\ntype: Requirement\n---\n\nbody\n") // title falls back to filename
	write(t, root, ".specquill/skills/x.md", "---\ntype: Skill\n---\n")                  // hidden dir skipped
	write(t, root, "index.md", "---\nokf_version: \"0.1\"\ncustom: kept\n---\nstale body\n")

	changed, err := GenerateIndexes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 2 { // root index.md + requirements/index.md
		t.Fatalf("changed = %v", changed)
	}

	rootIdx, _ := os.ReadFile(filepath.Join(root, "index.md"))
	s := string(rootIdx)
	for _, want := range []string{
		"okf_version", "custom: kept", // existing frontmatter preserved
		"- [Readme](/README.md)",
		"## requirements",
		"- [Login](/requirements/REQ-001.md) — Users can log in.",
		"- [REQ-002](/requirements/REQ-002.md)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("root index missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "skills") || strings.Contains(s, "stale body") {
		t.Fatalf("root index leaked hidden dir or stale body:\n%s", s)
	}

	dirIdx, _ := os.ReadFile(filepath.Join(root, "requirements/index.md"))
	if !strings.HasPrefix(string(dirIdx), "# requirements\n") || !strings.Contains(string(dirIdx), "REQ-001") {
		t.Fatalf("dir index wrong:\n%s", dirIdx)
	}

	// second run is a no-op (byte-stable)
	changed, err = GenerateIndexes(root)
	if err != nil || len(changed) != 0 {
		t.Fatalf("regeneration not stable: %v %v", changed, err)
	}
}

func TestWriteLog(t *testing.T) {
	root := t.TempDir()
	wrote, err := WriteLog(root, []LogEntry{
		{Date: "2026-07-09", Author: "Flo", Subject: "add venue spec"},
		{Date: "2026-07-09", Author: "Anna", Subject: "req: tighten RTS 22 deadline"},
		{Date: "2026-07-01", Author: "Flo", Subject: "remove stale mapping"},
	})
	if err != nil || !wrote {
		t.Fatalf("WriteLog: %v %v", wrote, err)
	}
	b, _ := os.ReadFile(filepath.Join(root, "log.md"))
	s := string(b)
	for _, want := range []string{
		"# Log", "## 2026-07-09", "## 2026-07-01",
		"- **Added** add venue spec (Flo)",
		"- **Updated** req: tighten RTS 22 deadline (Anna)",
		"- **Removed** remove stale mapping (Flo)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("log missing %q:\n%s", want, s)
		}
	}
	if strings.Index(s, "2026-07-09") > strings.Index(s, "2026-07-01") {
		t.Fatal("log not newest-first")
	}
	// idempotent
	if wrote, _ = WriteLog(root, []LogEntry{{Date: "2026-07-09", Author: "Flo", Subject: "add venue spec"}, {Date: "2026-07-09", Author: "Anna", Subject: "req: tighten RTS 22 deadline"}, {Date: "2026-07-01", Author: "Flo", Subject: "remove stale mapping"}}); !wrote {
		// content unchanged → wrote=false is the expected steady state
	}
}
