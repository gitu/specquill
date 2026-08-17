package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadYML(t *testing.T, yml string) (*Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yml")
	if err := os.WriteFile(p, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

func TestDynamicConfigParsesAndDefaults(t *testing.T) {
	cfg, err := loadYML(t, `
data_dir: ./d
git: { committer_name: s, committer_email: s@t }
auth: { forge: { kind: github } }
projects: [ { id: w, remote: "https://github.com/a/w.git" } ]
dynamic:
  enabled: true
  search: true
  user_budget: 2GB
  idle_after: 24h
`)
	if err != nil {
		t.Fatal(err)
	}
	d := cfg.Dynamic
	if !d.Enabled || !d.Search {
		t.Fatalf("flags: %+v", d)
	}
	if d.UserBudget != ByteSize(2e9) {
		t.Fatalf("budget: %d", d.UserBudget)
	}
	if d.IdleAfter != 24*time.Hour {
		t.Fatalf("idle: %v", d.IdleAfter)
	}
	// unset retention defaults, and never below the idle period
	if d.UnsyncedRetention < d.IdleAfter || d.UnsyncedRetention != defaultUnsyncedRetention {
		t.Fatalf("retention: %v", d.UnsyncedRetention)
	}
}

func TestDynamicRequiresForgeAuth(t *testing.T) {
	_, err := loadYML(t, `
data_dir: ./d
git: { committer_name: s, committer_email: s@t }
auth: { local: { enabled: true } }
projects: [ { id: w, remote: "https://github.com/a/w.git" } ]
dynamic: { enabled: true }
`)
	if err == nil || !strings.Contains(err.Error(), "forge") {
		t.Fatalf("dynamic without forge auth must be rejected, got %v", err)
	}
}

func TestByteSizeForms(t *testing.T) {
	cases := map[string]ByteSize{
		"user_budget: 1024":    1024,
		"user_budget: 500MB":   ByteSize(5e8),
		"user_budget: 1GiB":    1 << 30,
		"user_budget: \"2 TB\"": ByteSize(2e12),
	}
	for yml, want := range cases {
		cfg, err := loadYML(t, `
data_dir: ./d
git: { committer_name: s, committer_email: s@t }
auth: { forge: { kind: github } }
projects: [ { id: w, remote: "https://github.com/a/w.git" } ]
dynamic:
  enabled: true
  `+yml+`
`)
		if err != nil {
			t.Fatalf("%s: %v", yml, err)
		}
		if cfg.Dynamic.UserBudget != want {
			t.Errorf("%s: got %d, want %d", yml, cfg.Dynamic.UserBudget, want)
		}
	}
}
