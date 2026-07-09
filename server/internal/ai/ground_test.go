package ai

import (
	"strings"
	"testing"
)

func TestGroundingPromptIncludesReferenceSources(t *testing.T) {
	workspace := map[string]string{
		"requirements/REQ-042.md": "# REQ-042\nTransactions reported to the microsecond.",
		"specs/txn-report.md":     "Implements REQ-042 via the reporting pipeline.",
	}
	refs := []GroundingSource{{
		Name: "regulations",
		Files: map[string]string{
			"regulations/mifid-ii.md": "RTS 22 mandates microsecond timestamps.",
		},
	}}

	out := GroundingPrompt(workspace, refs, "specs/txn-report.md", 0)

	// workspace files ground the answer
	if !strings.Contains(out, "## requirements/REQ-042.md") {
		t.Fatal("workspace file heading missing")
	}
	// grounded source appears under a ~source/path heading, marked read-only
	if !strings.Contains(out, "## ~regulations/regulations/mifid-ii.md") {
		t.Fatal("reference heading missing")
	}
	if !strings.Contains(out, "RTS 22 mandates microsecond timestamps.") {
		t.Fatal("reference content missing")
	}
	if !strings.Contains(out, "read-only") {
		t.Fatal("read-only marker missing from reference section")
	}
	// focus doc is pinned before the sibling
	if strings.Index(out, "## specs/txn-report.md") > strings.Index(out, "## requirements/REQ-042.md") {
		t.Fatal("focus document was not pinned first")
	}
}

func TestGroundingPromptWorkspaceFloorUnderPressure(t *testing.T) {
	// a huge source must not starve the workspace: the workspace keeps its 60%
	// floor and the source is capped at its share.
	big := strings.Repeat("x", 40*1024)
	workspace := map[string]string{"requirements/REQ-1.md": strings.Repeat("w", 20*1024)}
	refs := []GroundingSource{{Name: "reg", Files: map[string]string{"a.md": big, "b.md": big}}}

	budget := 32 * 1024
	out := GroundingPrompt(workspace, refs, "", budget)

	if !strings.Contains(out, "## requirements/REQ-1.md") {
		t.Fatal("workspace file dropped despite 60% floor")
	}
	// the source is present but its two big files can't both fit its ~40% share
	if !strings.Contains(out, "# Reference source ~reg") {
		t.Fatal("reference section missing")
	}
	if !strings.Contains(out, "omitted for length") {
		t.Fatal("expected the oversized source to report an omission")
	}
}

func TestGroundingPromptNoReferencesMatchesWorkspaceOnly(t *testing.T) {
	workspace := map[string]string{"specs/a.md": "hello"}
	out := GroundingPrompt(workspace, nil, "", 0)
	if strings.Contains(out, "Reference source") {
		t.Fatal("no references should yield no reference section")
	}
	if !strings.Contains(out, "## specs/a.md") {
		t.Fatal("workspace file missing")
	}
}
