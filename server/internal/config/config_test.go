package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func load(t *testing.T, yml string) *Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "specquill.yml")
	if err := os.WriteFile(p, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

const commonTail = `
git: { committer_name: svc, committer_email: svc@t }
auth: { local: { enabled: true } }
database: { url: "postgres://x" }
data_dir: ./data
`

// Golden: the legacy `repos:` shape must normalize to the identical runtime
// model — writable → project (root content), readonly → git source, and the
// rebuilt clone registry must match what gitx consumed before config v2.
func TestLegacyReposNormalize(t *testing.T) {
	cfg := load(t, `
repos:
  - { id: trading-specs, mode: writable, remote: "https://x/specs.git" }
  - { id: regulations, mode: readonly, remote: "https://x/reg.git", sync_interval: 1m }
`+commonTail)

	if len(cfg.Projects) != 1 || cfg.Projects[0].ID != "trading-specs" || cfg.Projects[0].ContentRoot != "" {
		t.Fatalf("projects: %+v", cfg.Projects)
	}
	p := cfg.Projects[0]
	if p.DefaultBranch != "main" || p.SyncInterval != 2*time.Minute || len(p.ProtectedBranches) != 1 || p.ProtectedBranches[0] != "main" {
		t.Fatalf("project defaults: %+v", p)
	}
	if len(cfg.Sources) != 1 || cfg.Sources[0].Name != "regulations" || cfg.Sources[0].Kind != "git" || cfg.Sources[0].SyncInterval != time.Minute {
		t.Fatalf("sources: %+v", cfg.Sources)
	}
	// clone registry identical to the pre-v2 shape
	if len(cfg.Repos) != 2 || cfg.Repos[0].ID != "trading-specs" || cfg.Repos[0].Mode != Writable ||
		cfg.Repos[1].ID != "regulations" || cfg.Repos[1].Mode != ReadOnly {
		t.Fatalf("clone registry: %+v", cfg.Repos)
	}
}

// The two yml files shipped in the repo must keep loading.
func TestShippedConfigsLoad(t *testing.T) {
	cases := map[string]struct{ projects, sources int }{
		"specquill.dev.yml":     {2, 2}, // trading-specs + specquill-docs; regulations (git) + platform-api (openapi)
		"specquill.example.yml": {1, 1},
	}
	for f, want := range cases {
		cfg, err := Load(filepath.Join("..", "..", "..", f))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if len(cfg.Projects) != want.projects || len(cfg.Sources) != want.sources {
			t.Fatalf("%s: projects=%d sources=%d", f, len(cfg.Projects), len(cfg.Sources))
		}
	}
}

func TestV2ConfigShape(t *testing.T) {
	cfg := load(t, `
projects:
  - { id: specs, remote: "https://x/mono.git", content_root: "/docs/specs/" }
sources:
  - { name: reg, kind: git, remote: "https://x/reg.git" }
  - { name: api, kind: openapi, remote: "https://x/openapi.yaml", sync_interval: 6h }
grants: [reg]
`+commonTail)
	if cfg.Projects[0].ContentRoot != "docs/specs" {
		t.Fatalf("content_root not cleaned: %q", cfg.Projects[0].ContentRoot)
	}
	// clone registry: the project, the git source, and the openapi source as a
	// remote-less mirror repo (populated by the importer.Runner)
	if len(cfg.Repos) != 3 || cfg.Repos[0].ID != "specs" || cfg.Repos[0].ContentRoot != "docs/specs" || cfg.Repos[1].ID != "reg" {
		t.Fatalf("clone registry: %+v", cfg.Repos)
	}
	if api := cfg.Repos[2]; api.ID != "api" || !api.Mirror || api.Remote != "" || api.Mode != ReadOnly {
		t.Fatalf("openapi source should materialize as a remote-less mirror: %+v", api)
	}
	if len(cfg.Grants) != 1 || cfg.Grants[0] != "reg" {
		t.Fatalf("grants: %v", cfg.Grants)
	}
}

func TestValidationErrors(t *testing.T) {
	cases := []struct{ yml, want string }{
		{`projects: []` + commonTail, "at least one project"},
		{`projects: [{id: a, remote: r}, {id: a, remote: r}]` + commonTail, "duplicate"},
		{`projects: [{id: a, remote: r}]
sources: [{name: a, kind: git, remote: r}]` + commonTail, "duplicate"},
		{`projects: [{id: a, remote: r, content_root: "../up"}]` + commonTail, "traverse"},
		{`projects: [{id: a, remote: r}]
sources: [{name: s, kind: ftp, remote: r}]` + commonTail, "kind"},
		{`projects: [{id: a, remote: r}]
grants: [nope]` + commonTail, "unknown source"},
	}
	for i, c := range cases {
		p := filepath.Join(t.TempDir(), "c.yml")
		_ = os.WriteFile(p, []byte(c.yml), 0o644)
		_, err := Load(p)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("case %d: want error containing %q, got %v", i, c.want, err)
		}
	}
}

func TestNormalizeIdempotent(t *testing.T) {
	cfg := &Config{Repos: []RepoConfig{{ID: "w", Mode: Writable, Remote: "r"}}}
	cfg.Normalize()
	cfg.Normalize()
	if len(cfg.Projects) != 1 || len(cfg.Repos) != 1 {
		t.Fatalf("not idempotent: projects=%d repos=%d", len(cfg.Projects), len(cfg.Repos))
	}
}
