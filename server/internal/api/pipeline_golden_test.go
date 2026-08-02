package api

import (
	"strings"
	"testing"

	"specquill/server/internal/ai"
	"specquill/server/internal/recipe"
)

// The safety net for converting drift/gaps/extract from hardcoded Go pipelines
// into built-in recipes: the recipe-rendered conversation must be what the old
// prompt builders produced, BYTE FOR BYTE.
//
// These are not "the prompts look reasonable" tests. A converted built-in that
// says something subtly different to the model is a behaviour change nobody
// asked for, and it would show up as findings quietly getting better or worse
// months later. The expectations below are transcribed from ai/ground.go as it
// stood before the conversion (git show HEAD~1:server/internal/ai/ground.go).
//
// A deliberate prompt change means updating BOTH the recipe and the expectation
// here, in the same commit, on purpose.

func goldenContext(t *testing.T, slug string, files map[string]string, focus, instructions string) *runContext {
	t.Helper()
	rec, ok := recipe.Builtin(slug)
	if !ok {
		t.Fatalf("built-in %s missing", slug)
	}
	rec = rec.Clone()
	rec.Instructions = instructions
	return &runContext{
		rec:     rec,
		files:   files,
		focus:   focus,
		sources: []ai.GroundingSource{{Name: "platform-api"}, {Name: "regulations"}},
		note:    func(string) {},
	}
}

func stageOf(t *testing.T, rc *runContext, id string) *recipe.Stage {
	t.Helper()
	st, ok := rc.rec.Stage(id)
	if !ok {
		t.Fatalf("stage %s missing from %s", id, rc.rec.Slug)
	}
	return st
}

func assertSame(t *testing.T, what, got, want string) {
	t.Helper()
	if got == want {
		return
	}
	// point at the first divergence — a whole prompt diff is unreadable
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	from := max(0, i-60)
	t.Errorf("%s diverges at byte %d\n--- recipe ---\n%q\n--- expected ---\n%q",
		what, i, snippet(got, from), snippet(want, from))
}

func snippet(s string, from int) string {
	return s[from:min(len(s), from+160)]
}

// ---------------------------------------------------------------- drift

func TestGoldenDriftPrompt(t *testing.T) {
	const doc = "specs/txn-report.md"
	const content = "---\ntype: Spec\n---\n\n# Transaction reporting\n\nREQ-012 requires microsecond precision."
	const linked = "## requirements/REQ-012.md\nDrivers: MiFID II\n"
	const extracted = "### Transaction reporting\n- Reports SHALL be submitted daily.\n"

	for _, tc := range []struct {
		name                                string
		linked, extracted, focus, instructs string
	}{
		{name: "bare"},
		{name: "linked", linked: linked},
		{name: "extracted", extracted: extracted},
		{name: "linked+extracted", linked: linked, extracted: extracted},
		{name: "focused", focus: "data retention"},
		{name: "instructed", instructs: "Ignore the legacy endpoints."},
		{
			name: "everything", linked: linked, extracted: extracted,
			focus: "data retention", instructs: "Ignore the legacy endpoints.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc := goldenContext(t, "drift", map[string]string{doc: content}, tc.focus, tc.instructs)
			// the golden builder took these as arguments; the runner derives
			// them, so inject them at the same seam
			rc.linkedOverride, rc.extractedOverride = tc.linked, tc.extracted
			got := rc.buildMessages(doc, stageOf(t, rc, "verify"), nil, -1)

			want := legacyDriftPrompt(doc, content, tc.linked, tc.extracted,
				tc.focus, tc.instructs, []string{"platform-api", "regulations"})
			assertSame(t, "drift system", got[0].Content, want[0].Content)
			assertSame(t, "drift user", got[1].Content, want[1].Content)
		})
	}
}

// ---------------------------------------------------------------- gaps

func TestGoldenGapPrompt(t *testing.T) {
	const docIndex = "requirements/REQ-012.md\nspecs/txn-report.md"
	const extracted = "### Reporting\n- Reports SHALL be submitted daily.\n"

	for _, tc := range []struct{ name, extracted, focus, instructs string }{
		{name: "bare"},
		{name: "extracted", extracted: extracted},
		{name: "focused", focus: "data retention"},
		{name: "everything", extracted: extracted, focus: "data retention", instructs: "Only the public API."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc := goldenContext(t, "gaps", nil, tc.focus, tc.instructs)
			rc.docIndex = docIndex
			rc.extractedOverride = tc.extracted
			got := rc.buildMessages("platform-api", stageOf(t, rc, "sweep"), nil, -1)

			want := legacyGapPrompt("platform-api", docIndex, tc.extracted, tc.focus, tc.instructs)
			assertSame(t, "gaps system", got[0].Content, want[0].Content)
			assertSame(t, "gaps user", got[1].Content, want[1].Content)
		})
	}
}

// ---------------------------------------------------------------- extract

func TestGoldenSurveyPrompt(t *testing.T) {
	for _, tc := range []struct{ name, focus, instructs string }{
		{name: "bare"},
		{name: "focused", focus: "data retention"},
		{name: "instructed", instructs: "Skip the vendored code."},
		{name: "everything", focus: "data retention", instructs: "Skip the vendored code."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc := goldenContext(t, "extract", nil, tc.focus, tc.instructs)
			got := rc.buildMessages("platform-api", stageOf(t, rc, "survey"), nil, -1)

			want := legacySurveyPrompt("platform-api", tc.focus, tc.instructs)
			assertSame(t, "survey system", got[0].Content, want[0].Content)
			assertSame(t, "survey user", got[1].Content, want[1].Content)
		})
	}
}

func TestGoldenExtractPrompt(t *testing.T) {
	area := map[string]any{
		"name":    "Transaction reporting",
		"summary": "Submitting executed trades to the competent authority.",
		"paths":   []any{"reporting/submit.go", "openapi.json"},
	}
	bare := map[string]any{"name": "Transaction reporting"}

	for _, tc := range []struct {
		name             string
		item             map[string]any
		focus, instructs string
	}{
		{name: "full", item: area},
		{name: "no summary or paths", item: bare},
		{name: "focused", item: area, focus: "data retention"},
		{name: "everything", item: area, focus: "data retention", instructs: "RFC-2119 only."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rc := goldenContext(t, "extract", nil, tc.focus, tc.instructs)
			got := rc.buildMessages("platform-api", stageOf(t, rc, "area"),
				[]stageItem{{Fields: tc.item}}, 0)

			summary, _ := tc.item["summary"].(string)
			var paths []string
			if raw, ok := tc.item["paths"].([]any); ok {
				for _, p := range raw {
					paths = append(paths, p.(string))
				}
			}
			want := legacyExtractPrompt("platform-api", tc.item["name"].(string), summary,
				paths, tc.focus, tc.instructs)
			assertSame(t, "extract system", got[0].Content, want[0].Content)
			assertSame(t, "extract user", got[1].Content, want[1].Content)
		})
	}
}

func TestGoldenMatchPrompt(t *testing.T) {
	const docIndex = "requirements/REQ-012.md\nspecs/txn-report.md"
	items := []stageItem{
		{Fields: map[string]any{"__group": "Reporting", "statement": "Reports SHALL be submitted daily."}},
		{Fields: map[string]any{"__group": "Reporting", "statement": "Timestamps SHALL be microsecond precise."}},
	}
	rc := goldenContext(t, "extract", nil, "", "")
	rc.docIndex = docIndex
	got := rc.buildMessages("platform-api", stageOf(t, rc, "match"), items, 0)

	legacyItems := "1. [Reporting] Reports SHALL be submitted daily.\n" +
		"2. [Reporting] Timestamps SHALL be microsecond precise.\n"
	want := legacyMatchPrompt(legacyItems, docIndex, "")
	assertSame(t, "match system", got[0].Content, want[0].Content)
	assertSame(t, "match user", got[1].Content, want[1].Content)
}

// The focus note is a HARD constraint in the prompt, not a hint — a recipe
// that drops it silently widens every focused run.
func TestFocusNoteOnlyAppearsWhenAimed(t *testing.T) {
	for _, slug := range []string{"drift", "gaps", "extract"} {
		rec, _ := recipe.Builtin(slug)
		rc := goldenContext(t, slug, map[string]string{"a.md": "x"}, "", "")
		msgs := rc.buildMessages("a.md", &rec.Stages[0], nil, -1)
		if strings.Contains(msgs[0].Content, "# Focus") {
			t.Errorf("%s: unfocused run carries a focus block", slug)
		}
		rc = goldenContext(t, slug, map[string]string{"a.md": "x"}, "retention", "")
		msgs = rc.buildMessages("a.md", &rec.Stages[0], nil, -1)
		if !strings.Contains(msgs[0].Content, "# Focus") ||
			!strings.Contains(msgs[0].Content, "retention") {
			t.Errorf("%s: focused run lost its focus block:\n%s", slug, msgs[0].Content)
		}
	}
}
