package recipe

import (
	"strings"
	"testing"
)

const minimal = `---
name: Test
units: sources
output: findings
findings:
  - kind: model-gap
    label: Undocumented model field
    draftable: true
stages:
  - id: detect
    over: unit
    produces: findings
    key: findings
---

## instructions

Ignore DTOs.

## stage: detect

Find undocumented model fields.

### focus

Aimed at {{focus}}.

### user

Report {{docIndex}} as JSON.
`

func TestParseSplitsFrontmatterAndBody(t *testing.T) {
	r, warnings, err := Parse("test", minimal)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if r.Name != "Test" || r.Units != UnitsSources || r.Output != OutputFindings {
		t.Fatalf("frontmatter not read: %+v", r)
	}
	if r.Instructions != "Ignore DTOs." {
		t.Errorf("instructions: %q", r.Instructions)
	}
	st := r.Stages[0]
	if st.Prompt != "Find undocumented model fields." {
		t.Errorf("prompt: %q", st.Prompt)
	}
	if st.FocusNote != "Aimed at {{focus}}." {
		t.Errorf("focus note: %q", st.FocusNote)
	}
	if st.User != "Report {{docIndex}} as JSON." {
		t.Errorf("user template: %q", st.User)
	}
	if !r.Draftable("model-gap") || r.Draftable("nope") {
		t.Error("draftable lookup is wrong")
	}
}

// A body section for a stage that no longer exists is a rename that missed
// the frontmatter — worth saying, not worth failing.
func TestParseWarnsAboutOrphanSections(t *testing.T) {
	src := strings.Replace(minimal, "## stage: detect", "## stage: detekt", 1)
	_, warnings, err := Parse("test", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var sawOrphan, sawMissing bool
	for _, w := range warnings {
		if strings.Contains(w, "matches no declared stage") {
			sawOrphan = true
		}
		if strings.Contains(w, "empty prompt") {
			sawMissing = true
		}
	}
	if !sawOrphan || !sawMissing {
		t.Fatalf("expected both warnings, got %v", warnings)
	}
}

func TestParseRejectsMissingFrontmatter(t *testing.T) {
	if _, _, err := Parse("test", "# just a document\n"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestValidateRejectsBadPipelines(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{"no units", "name: T\noutput: findings\nstages: []", "units is required"},
		{"bad units", "name: T\nunits: files\noutput: findings\nstages: []", "units must be"},
		{"no output", "name: T\nunits: docs\nstages: []", "output is required"},
		{"no stages", "name: T\nunits: docs\noutput: findings\nfindings: [{kind: x}]", "at least one stage"},
		{
			"forward reference",
			"name: T\nunits: docs\noutput: findings\nfindings: [{kind: x}]\n" +
				"stages:\n  - {id: a, over: b, produces: findings, key: findings}\n" +
				"  - {id: b, over: unit, produces: items, key: items}",
			"not an earlier stage",
		},
		{
			"self reference",
			"name: T\nunits: docs\noutput: findings\nfindings: [{kind: x}]\n" +
				"stages:\n  - {id: a, over: a, produces: findings, key: findings}",
			"not an earlier stage",
		},
		{
			"duplicate stage id",
			"name: T\nunits: docs\noutput: findings\nfindings: [{kind: x}]\n" +
				"stages:\n  - {id: a, over: unit, produces: findings, key: findings}\n" +
				"  - {id: a, over: unit, produces: findings, key: findings}",
			"duplicate stage id",
		},
		{
			"findings output with no kinds",
			"name: T\nunits: docs\noutput: findings\n" +
				"stages:\n  - {id: a, over: unit, produces: findings, key: findings}",
			"no finding kinds are declared",
		},
		{
			"findings output with no producing stage",
			"name: T\nunits: docs\noutput: findings\nfindings: [{kind: x}]\n" +
				"stages:\n  - {id: a, over: unit, produces: items, key: items}",
			"no stage produces them",
		},
		{
			// a template naming context the runner never supplies would render
			// a blank block into the prompt — caught at parse time
			"unknown context in the user template",
			"name: T\nunits: docs\noutput: findings\nfindings: [{kind: x}]\n" +
				"stages:\n  - {id: a, over: unit, produces: findings, key: findings}\n" +
				"---\n\n## stage: a\n\nsystem\n\n### user\n\nlook at {{wat}}\n",
			"unknown context",
		},
		{
			"missing key",
			"name: T\nunits: docs\noutput: findings\nfindings: [{kind: x}]\n" +
				"stages:\n  - {id: a, over: unit, produces: findings}",
			"has no `key`",
		},
		{
			"batch over unit",
			"name: T\nunits: docs\noutput: findings\nfindings: [{kind: x}]\n" +
				"stages:\n  - {id: a, over: unit, produces: findings, key: findings, batch: 8}",
			"there is nothing to batch",
		},
		{
			"kind not kebab",
			"name: T\nunits: docs\noutput: findings\nfindings: [{kind: Model Gap}]\n" +
				"stages:\n  - {id: a, over: unit, produces: findings, key: findings}",
			"kebab-case",
		},
		{
			"annotations without a target",
			"name: T\nunits: docs\noutput: findings\nfindings: [{kind: x}]\n" +
				"stages:\n  - {id: a, over: unit, produces: annotations, key: matches}",
			"which is not an earlier stage",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := Parse("test", "---\n"+c.yaml+"\n---\n")
			if err == nil {
				t.Fatalf("expected an error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %q, want it to contain %q", err, c.want)
			}
		})
	}
}

func TestValidateModelsAgainstTheAllowlist(t *testing.T) {
	r, _, err := Parse("test", strings.Replace(minimal,
		"  - id: detect", "  - id: detect\n    model: gpt-5-mini", 1))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := r.ValidateModels(map[string]bool{"gpt-5": true}); err == nil {
		t.Fatal("expected the unlisted model to be rejected")
	} else if !strings.Contains(err.Error(), "ai.models") {
		t.Fatalf("the error should point at the config knob: %v", err)
	}
	if err := r.ValidateModels(map[string]bool{"gpt-5-mini": true}); err != nil {
		t.Fatalf("allowlisted model rejected: %v", err)
	}
	// the tier names are always available — they are not model ids
	r.Stages[0].Model = "quick"
	if err := r.ValidateModels(map[string]bool{}); err != nil {
		t.Fatalf("quick tier rejected: %v", err)
	}
}

// An unrecognised kind must not drop an evidence-verified finding — only its
// label is in doubt.
func TestNormKindFallsBackToTheFirstDeclared(t *testing.T) {
	r, _, err := Parse("test", minimal)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.NormKind("Model Gap"); got != "model-gap" {
		t.Errorf("normalization failed: %q", got)
	}
	if got := r.NormKind("something-else"); got != "model-gap" {
		t.Errorf("unknown kind should fall back to the first declared, got %q", got)
	}
}

func TestLoadAllReadsProjectRecipes(t *testing.T) {
	files := map[string]string{
		Dir + "good.md":        minimal,
		Dir + "broken.md":      "---\nname: Broken\nunits: nope\n---\n",
		Dir + "drift.md":       minimal, // shadows a built-in
		Dir + "nested/deep.md": minimal, // not a recipe: only the top level counts
		"specs/other.md":       minimal,
	}
	recipes, warnings, errs := LoadAll(files)
	if len(recipes) != 1 || recipes[0].Slug != "good" {
		t.Fatalf("expected just `good`, got %d: %+v", len(recipes), recipes)
	}
	if recipes[0].Path != Dir+"good.md" {
		t.Errorf("path not recorded: %q", recipes[0].Path)
	}
	if _, ok := errs["broken"]; !ok {
		t.Error("a broken recipe should be reported, not silently dropped")
	}
	if msg, ok := errs["drift"]; !ok || !strings.Contains(msg, "shadows a built-in") {
		t.Errorf("shadowing a built-in should be an error, got %q", msg)
	}
	_ = warnings
}

func TestBuiltinsParseAndDeclareTheirKinds(t *testing.T) {
	for _, slug := range BuiltinSlugs {
		r, ok := Builtin(slug)
		if !ok {
			t.Fatalf("built-in %s missing", slug)
		}
		if r.Slug != slug || !r.Builtin {
			t.Errorf("%s: slug/builtin flag wrong: %+v", slug, r)
		}
		for _, st := range r.Stages {
			if st.Prompt == "" {
				t.Errorf("%s: stage %s has no prompt", slug, st.ID)
			}
			if st.User == "" {
				t.Errorf("%s: stage %s has no `### user` template", slug, st.ID)
			}
		}
	}
	drift, _ := Builtin("drift")
	for _, want := range []string{"missing-implementation", "undocumented-behavior",
		"contradiction", "outdated-requirement", "new-requirement"} {
		if _, ok := drift.Kind(want); !ok {
			t.Errorf("drift lost the %q kind", want)
		}
	}
	if !drift.Draftable("new-requirement") || drift.Draftable("contradiction") {
		t.Error("drift draftability is wrong")
	}
	gaps, _ := Builtin("gaps")
	if !gaps.Draftable("coverage-gap") {
		t.Error("coverage gaps must stay draftable")
	}
	extract, _ := Builtin("extract")
	if extract.Output != OutputExtraction || len(extract.Stages) != 3 {
		t.Errorf("extract shape changed: %+v", extract)
	}
	if extract.Stages[2].Batch != 8 {
		t.Errorf("the matcher must batch 8 at a time, got %d", extract.Stages[2].Batch)
	}
}

// Clone must not let a run's overrides leak into the shared built-in.
func TestCloneIsolatesOverrides(t *testing.T) {
	orig, _ := Builtin("drift")
	c := orig.Clone()
	c.Sources = append(c.Sources, "app-src")
	c.Findings[0].Label = "changed"
	if len(orig.Sources) != 0 {
		t.Error("clone leaked sources into the built-in")
	}
	if orig.Findings[0].Label == "changed" {
		t.Error("clone leaked a finding label into the built-in")
	}
}
