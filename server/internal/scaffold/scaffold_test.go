package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"specquill/server/internal/okf"
)

func TestInit(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "acme-req", []string{"specs", "changes", "work-items"}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"README.md", ".specquill/config.yml", ".specquill/skills/authoring.md",
		".specquill/skills/requirements.md", ".specquill/skills/specs.md", ".specquill/skills/changes.md",
		".specquill/skills/work-items.md",
		"requirements/REQ-001.md", "specs/example.md", "changes/example.md", "work-items/WI-001.md",
		"specquill.yml.example",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf("expected a git repo: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "specquill.yml.example"))
	if !strings.Contains(string(raw), "quick_model") {
		t.Error("config stub should document the quick model tier")
	}

	// the combined config starter is valid YAML and spells out the full model
	rawCfg, _ := os.ReadFile(filepath.Join(dir, ".specquill/config.yml"))
	var cfg map[string]any
	if err := yaml.Unmarshal(rawCfg, &cfg); err != nil {
		t.Fatalf("config starter is not valid yaml: %v", err)
	}
	for _, key := range []string{"entities", "drivers", "statuses", "link_types", "ids", "properties"} {
		if _, ok := cfg[key]; !ok {
			t.Errorf("config starter misses section %q", key)
		}
	}
	if cfg["project"] != "acme-req" {
		t.Errorf("project = %v, want acme-req", cfg["project"])
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

// A scaffolded workspace must be a conformant OKF bundle out of the box.
func TestInitProducesConformantOKFBundle(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, "demo", AllTypes()); err != nil {
		t.Fatal(err)
	}
	if !okf.Enabled(dir) {
		t.Fatal("scaffold did not opt into OKF (root index.md missing okf_version)")
	}
	violations, err := okf.Validate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("scaffold not conformant:\n%s", strings.Join(violations, "\n"))
	}
	for _, p := range []string{"index.md", "requirements/index.md"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatalf("missing %s", p)
		}
	}
}
