package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "acme-req", []string{"specs", "changes"}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"README.md", ".reqbase/schema.json", ".reqbase/skills/authoring.md",
		".reqbase/skills/requirements.md", ".reqbase/skills/specs.md", ".reqbase/skills/changes.md",
		"requirements/REQ-001.md", "specs/example.md", "changes/example.md", "reqbase.yml.example",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("expected a git repo: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "reqbase.yml.example"))
	if !strings.Contains(string(raw), "quick_model") {
		t.Error("config stub should document the quick model tier")
	}

	// refuses to clobber an existing workspace
	if err := Init(dir, "", []string{"specs"}); err == nil {
		t.Fatal("re-init over existing files must fail")
	}
	// unknown types are rejected
	if err := Init(t.TempDir(), "", []string{"nonsense"}); err == nil || !strings.Contains(err.Error(), "unknown spec type") {
		t.Fatalf("unknown type must be rejected, got %v", err)
	}
}
