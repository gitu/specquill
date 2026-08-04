package recipe

import (
	"reflect"
	"testing"
)

func TestRenderSubstitutesKnownNames(t *testing.T) {
	vars := map[string]string{"source": "app-src", "focus": "retention", "item.name": "Order"}
	got := Render("Sweep ~{{source}} aimed at {{focus}}, entity {{ item.name }}.", vars)
	want := "Sweep ~app-src aimed at retention, entity Order."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The single most important property: prompt bodies SHOW the model the JSON
// they must return, and those braces must survive rendering untouched.
func TestRenderLeavesUnknownPlaceholdersAlone(t *testing.T) {
	body := `Reply with ONLY a JSON object:

{
  "findings": [
    {"anchor": "REQ-012", "title": "…"}
  ]
}

Aimed at {{focus}}, unknown {{nope}}.`
	got := Render(body, map[string]string{"focus": "retention"})
	if !contains(got, `"findings"`) || !contains(got, `{{nope}}`) {
		t.Fatalf("render mangled the body:\n%s", got)
	}
	if contains(got, "{{focus}}") {
		t.Fatal("known placeholder was not substituted")
	}
}

func TestRenderHandlesUnclosedPlaceholder(t *testing.T) {
	if got := Render("a {{ b", map[string]string{"b": "x"}); got != "a {{ b" {
		t.Fatalf("got %q", got)
	}
	if got := Render("no placeholders", nil); got != "no placeholders" {
		t.Fatalf("got %q", got)
	}
}

func TestPlaceholders(t *testing.T) {
	got := Placeholders("{{source}} and {{focus}} and {{source}} again, plus {}")
	if want := []string{"focus", "source"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestItemVarsFlattensFields(t *testing.T) {
	vars := ItemVars(map[string]any{
		"name":    "Transaction reporting",
		"paths":   []any{"reporting/submit.go", "openapi.json"},
		"count":   float64(3),
		"nested":  map[string]any{"a": 1},
		"missing": nil,
	})
	if vars["item.name"] != "Transaction reporting" {
		t.Errorf("name: %q", vars["item.name"])
	}
	// a list of scalars reads as prose, not as a JSON array — stage prompts
	// say "read {{item.paths}}"
	if vars["item.paths"] != "reporting/submit.go, openapi.json" {
		t.Errorf("paths: %q", vars["item.paths"])
	}
	if vars["item.count"] != "3" {
		t.Errorf("whole numbers must not render as 3.0: %q", vars["item.count"])
	}
	if vars["item.nested"] != `{"a":1}` {
		t.Errorf("nested: %q", vars["item.nested"])
	}
	if vars["item.missing"] != "" {
		t.Errorf("missing: %q", vars["item.missing"])
	}
	if vars["item"] == "" {
		t.Error("the whole item should be available as JSON")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// A kept conditional body is re-rendered, so nesting works to any depth; a
// dropped outer block swallows whatever it nests.
func TestRenderResolvesNestedConditionals(t *testing.T) {
	body := "{{#outer}}a{{#inner}}-x-{{/inner}}{{^inner}}-y-{{/inner}}b{{/outer}}"
	if got := Render(body, map[string]string{"outer": "on", "inner": "on"}); got != "a-x-b" {
		t.Fatalf("kept nested block: got %q", got)
	}
	if got := Render(body, map[string]string{"outer": "on"}); got != "a-y-b" {
		t.Fatalf("negated nested block: got %q", got)
	}
	if got := Render(body, map[string]string{"inner": "on"}); got != "" {
		t.Fatalf("dropped outer must swallow nested: got %q", got)
	}
}
