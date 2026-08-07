package ai

import (
	"strings"
	"testing"
)

func TestSectionsForFallsBackToGeneric(t *testing.T) {
	if got := SectionsFor("spec"); got[0] != "Overview" {
		t.Fatalf("spec outline = %v", got)
	}
	// an unknown (custom-entity) family still gets a usable outline
	got := SectionsFor("bikeshed")
	if len(got) == 0 || got[len(got)-1] != "Open questions" {
		t.Fatalf("generic outline = %v", got)
	}
	// callers mutate the returned slice (the SPA reorders) — never hand out
	// the package's own backing array
	got[0] = "mutated"
	if SectionsFor("bikeshed")[0] == "mutated" {
		t.Fatal("SectionsFor leaked its backing array")
	}
}

func TestWizardContextBriefCarriesIntentAndAltitude(t *testing.T) {
	c := WizardContext{Intent: "rate-limit the token endpoint", Family: "spec", Folder: "specs/", Altitude: "business"}
	b := c.brief()
	for _, want := range []string{"rate-limit the token endpoint", "specs/", "Altitude: BUSINESS", "spec"} {
		if !strings.Contains(b, want) {
			t.Fatalf("brief missing %q:\n%s", want, b)
		}
	}
	// no altitude → no altitude line at all (the family's skill sets register)
	if strings.Contains(WizardContext{Family: "spec", Intent: "x"}.brief(), "Altitude:") {
		t.Fatal("empty altitude still emitted a rule")
	}
}

func TestInterviewRulesNameTheOutline(t *testing.T) {
	rules := InterviewRules(WizardContext{Intent: "x", Family: "spec"}, []string{"Overview", "Edge cases"})
	if !strings.Contains(rules, "Overview, Edge cases") {
		t.Fatal("interview prompt does not mention the outline it interviews towards")
	}
	if !strings.Contains(rules, "readyToDraft") {
		t.Fatal("interview prompt does not state the JSON contract")
	}
}

func TestTranscriptMessagesShapesTheConversation(t *testing.T) {
	msgs := TranscriptMessages("do the thing", []Message{
		{Role: "assistant", Content: "what scope?"},
		{Role: "tool", Content: "should be dropped"},
		{Role: "user", Content: "  "},
		{Role: "user", Content: "all of it"},
	}, "Write it now.")
	if len(msgs) != 4 {
		t.Fatalf("got %d messages: %+v", len(msgs), msgs)
	}
	if msgs[0].Role != "user" || !strings.HasPrefix(msgs[0].Content, "Intent: do the thing") {
		t.Fatalf("first message = %+v", msgs[0])
	}
	if msgs[1].Content != "what scope?" || msgs[2].Content != "all of it" {
		t.Fatalf("transcript not preserved: %+v", msgs)
	}
	if msgs[3].Content != "Write it now." {
		t.Fatalf("final instruction missing: %+v", msgs[3])
	}
	// an empty everything still yields a valid conversation (providers reject
	// a messages array with only a system turn)
	if len(TranscriptMessages("", nil, "")) == 0 {
		t.Fatal("empty input produced no user turn")
	}
}

func TestSortSectionsLikeNormalizesModelOutput(t *testing.T) {
	want := []string{"Overview", "Behaviour", "Edge cases"}
	got := SortSectionsLike(want, []Section{
		{Name: "edge cases", Content: "E"},  // re-cased
		{Name: "Overview", Content: "O"},    // out of order
		{Name: "Assumptions", Content: "A"}, // unrequested extra
	})
	if len(got) != 4 {
		t.Fatalf("got %d sections: %+v", len(got), got)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("section %d = %q, want %q", i, got[i].Name, name)
		}
	}
	if got[0].Content != "O" || got[2].Content != "E" {
		t.Fatalf("content not matched to the outline: %+v", got)
	}
	// a section the model skipped is present but empty — the UI shows the gap
	if got[1].Name != "Behaviour" || got[1].Content != "" {
		t.Fatalf("missing section not stubbed: %+v", got[1])
	}
	// an unprompted extra is kept, not silently dropped
	if got[3].Name != "Assumptions" {
		t.Fatalf("extra section dropped: %+v", got)
	}
}

func TestAssembleDocument(t *testing.T) {
	md := AssembleDocument("Rate-limit the token endpoint", []Section{
		{Name: "Overview", Content: "It limits.\n"},
		{Name: "Edge cases", Content: ""},
		{Name: "  ", Content: "orphan"},
	})
	if !strings.HasPrefix(md, "# Rate-limit the token endpoint\n") {
		t.Fatalf("no H1:\n%s", md)
	}
	if !strings.Contains(md, "\n## Overview\n\nIt limits.\n") {
		t.Fatalf("section body not rendered:\n%s", md)
	}
	// an empty section keeps its heading — the human sees what is unwritten
	if !strings.Contains(md, "## Edge cases") {
		t.Fatalf("empty section dropped:\n%s", md)
	}
	if strings.Contains(md, "orphan") {
		t.Fatalf("nameless section emitted:\n%s", md)
	}
}
