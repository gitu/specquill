package recipe

import (
	"os"
	"testing"
)

// The example recipe shipped in the dev fixture (repo/.specquill/alignment/)
// is the first one anyone reads, and the thing they copy. It has to parse
// cleanly — a broken example teaches the wrong shape.
func TestFixtureRecipeParses(t *testing.T) {
	raw, err := os.ReadFile("../../../repo/.specquill/alignment/deadline-audit.md")
	if err != nil {
		t.Skip("fixture repo not present")
	}
	rec, warnings, err := Parse("deadline-audit", string(raw))
	if err != nil {
		t.Fatalf("the shipped example does not parse: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("the shipped example warns: %v", warnings)
	}
	if len(rec.Stages) != 2 || rec.Units != UnitsSources {
		t.Fatalf("shape changed: %+v", rec)
	}
	for _, st := range rec.Stages {
		if st.Prompt == "" || st.User == "" {
			t.Fatalf("stage %s is missing a prompt or a user template", st.ID)
		}
	}
	// it demonstrates the features it exists to demonstrate
	if len(rec.Findings) != 2 || !rec.Draftable("unstated-deadline") {
		t.Errorf("the example should show custom kinds and draftability: %+v", rec.Findings)
	}
	if len(rec.Files.Include) == 0 {
		t.Error("the example should show a source file filter")
	}
}
