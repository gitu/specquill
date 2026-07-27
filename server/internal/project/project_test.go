package project

import (
	"strings"
	"testing"
)

func TestMapInRejectsTraversal(t *testing.T) {
	p := &Project{ID: "specs", ContentRoot: "docs/specs"}
	bad := []string{
		"", "/abs", "../up", "a/../../up", "..", "a/..", ".git/config", "./", "a\\..\\b/../..",
	}
	for _, rel := range bad {
		if full, err := p.MapIn(rel); err == nil {
			// a/.. collapsing to nothing must fail; collapsing WITHIN the rel is fine
			if !strings.HasPrefix(full, "docs/specs/") {
				t.Fatalf("MapIn(%q) escaped: %q", rel, full)
			}
			if strings.Contains(full, "..") {
				t.Fatalf("MapIn(%q) kept traversal: %q", rel, full)
			}
		}
	}
	// traversal segments are rejected outright, even non-escaping ones
	if _, err := p.MapIn("a/../b.md"); err == nil {
		t.Fatal("MapIn accepted a path containing ..")
	}
	full, err := p.MapIn("requirements/REQ-001.md")
	if err != nil || full != "docs/specs/requirements/REQ-001.md" {
		t.Fatalf("MapIn: %q %v", full, err)
	}
}

func TestMapOutFiltersForeignPaths(t *testing.T) {
	p := &Project{ID: "specs", ContentRoot: "docs/specs"}
	if rel, ok := p.MapOut("docs/specs/requirements/REQ-001.md"); !ok || rel != "requirements/REQ-001.md" {
		t.Fatalf("MapOut: %q %v", rel, ok)
	}
	for _, foreign := range []string{"src/main.go", "docs/spec-other/x.md", "docs/specs"} {
		if _, ok := p.MapOut(foreign); ok {
			t.Fatalf("MapOut accepted foreign path %q", foreign)
		}
	}
	// root project is the identity
	root := &Project{ID: "w"}
	if rel, ok := root.MapOut("anything/x.md"); !ok || rel != "anything/x.md" {
		t.Fatal("root MapOut must be identity")
	}
}

func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig(`
version: 2
project: trading-specs
ui: { default_view: editor }   # v1 keys ignored
references:
  - { source: eu-regulations, paths: [mifid-ii/], grounding: true }
  - source: platform-api
`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "trading-specs" || len(cfg.References) != 2 {
		t.Fatalf("%+v", cfg)
	}
	if cfg.References[0].Source != "eu-regulations" || !cfg.References[0].Grounding || cfg.References[0].Paths[0] != "mifid-ii/" {
		t.Fatalf("ref0: %+v", cfg.References[0])
	}
	if cfg.References[1].Grounding {
		t.Fatal("grounding must default false")
	}
}

// The stage-3 grant matrix: granted selections pass, ungranted and unknown
// become warnings, duplicates collapse — never access.
func TestEffectiveReferences(t *testing.T) {
	cfg := &Config{References: []Reference{
		{Source: "eu-regulations", Grounding: true, Paths: []string{"mifid-ii/"}},
		{Source: "eu-regulations"}, // duplicate: ignored
		{Source: "revoked-source", Grounding: true},
		{Source: "never-existed"},
		{Source: ""}, // junk
	}}
	kinds := map[string]string{"eu-regulations": "git", "platform-api": "openapi"}

	refs, warnings := EffectiveReferences(cfg, kinds)
	if len(refs) != 1 || refs[0].Source != "eu-regulations" || refs[0].Kind != "git" || !refs[0].Grounding || refs[0].Paths[0] != "mifid-ii/" {
		t.Fatalf("refs: %+v", refs)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings: %v", warnings)
	}
	for _, w := range warnings {
		if !strings.Contains(w, "not in the source catalog") {
			t.Fatalf("warning wording: %q", w)
		}
	}
	// nil config = no refs, no warnings
	if refs, warnings := EffectiveReferences(nil, kinds); refs != nil || warnings != nil {
		t.Fatal("nil config must resolve to nothing")
	}
}

// The forge-PAT counterpart: references resolve against the config's OWN
// sources: definitions; a reference without a definition warns, a definition
// without name or remote defines nothing.
func TestEffectiveReferencesInRepo(t *testing.T) {
	cfg := &Config{
		Sources: []SourceDef{
			{Name: "regulations", Remote: "https://x/reg.git"},
			{Name: "no-remote"},                               // incomplete: defines nothing
			{Remote: "https://x/anon.git"},                    // incomplete: defines nothing
			{Name: "unreferenced", Remote: "https://x/u.git"}, // browsable, not a reference
		},
		References: []Reference{
			{Source: "regulations", Grounding: true, Paths: []string{"mifid-ii/"}},
			{Source: "regulations"}, // duplicate: ignored
			{Source: "no-remote"},   // defined incompletely → warning
			{Source: "phantom"},     // never defined → warning
			{Source: ""},            // junk
		},
	}
	refs, warnings := EffectiveReferencesInRepo(cfg)
	if len(refs) != 1 || refs[0].Source != "regulations" || refs[0].Kind != "git" || !refs[0].Grounding || refs[0].Paths[0] != "mifid-ii/" {
		t.Fatalf("refs: %+v", refs)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings: %v", warnings)
	}
	for _, w := range warnings {
		if !strings.Contains(w, "no matching source definition") {
			t.Fatalf("warning wording: %q", w)
		}
	}
	if refs, warnings := EffectiveReferencesInRepo(nil); refs != nil || warnings != nil {
		t.Fatal("nil config must resolve to nothing")
	}
}

// sources: parses alongside the existing keys and stays unknown-key-tolerant.
func TestParseConfigSources(t *testing.T) {
	cfg, err := ParseConfig(`
version: 2
sources:
  - { name: regulations, remote: "https://x/reg.git", default_branch: master }
references:
  - { source: regulations, grounding: true }
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sources) != 1 || cfg.Sources[0].Name != "regulations" ||
		cfg.Sources[0].Remote != "https://x/reg.git" || cfg.Sources[0].DefaultBranch != "master" {
		t.Fatalf("sources: %+v", cfg.Sources)
	}
}
