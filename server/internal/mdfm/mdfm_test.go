package mdfm

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

func TestSplitJoinRoundTrip(t *testing.T) {
	for _, raw := range []string{
		"---\ntitle: A\nstatus: draft\n---\n\n# A\n",
		"---\n# a comment\nlist: [a, b]\n---\nbody\n",
		"# no frontmatter\n\nbody\n",
	} {
		fm, body, _ := Split(raw)
		if got := Join(fm, body); got != raw {
			t.Errorf("round-trip mismatch:\nwant %q\ngot  %q", raw, got)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := Validate("---\ntitle: A\n---\n\nbody\n"); err != nil {
		t.Errorf("valid fm rejected: %v", err)
	}
	if err := Validate("# just a doc\n"); err != nil {
		t.Errorf("fm-less doc rejected: %v", err)
	}
	if err := Validate("---\ntitle: [broken\n---\n\nbody\n"); err == nil {
		t.Error("broken YAML accepted")
	}
	if err := Validate("---\ntitle: A\nno closing fence\n"); err == nil {
		t.Error("unclosed fence accepted")
	}
}

func TestTouchBumpsUpdatedPreservingRest(t *testing.T) {
	in := "---\n# keep me\nid: REQ-001\ntitle: A\nimplements: [specs/a.md, specs/b.md]\nupdated: 2026-01-01\n---\n\n# A\nbody\n"
	out, err := Touch(in, false, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"updated: 2026-07-29", "# keep me", "implements: [specs/a.md, specs/b.md]", "id: REQ-001", "\n---\n\n# A\nbody\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "created:") {
		t.Error("Touch(isNew=false) must not add created")
	}
	if strings.Contains(out, `"2026-07-29"`) {
		t.Error("date must stay an unquoted plain scalar")
	}
}

func TestTouchAddsMissingKeysOnNewDocs(t *testing.T) {
	out, err := Touch("---\ntitle: A\nstatus: draft\n---\n\n# A\n", true, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"created: 2026-07-29", "updated: 2026-07-29"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestTouchKeepsExistingCreated(t *testing.T) {
	out, err := Touch("---\ntitle: A\ncreated: 2025-01-01\n---\nbody\n", true, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "created: 2025-01-01") {
		t.Errorf("existing created clobbered:\n%s", out)
	}
}

func TestTouchLeavesFmLessDocsAlone(t *testing.T) {
	in := "# plain doc\n\nbody\n"
	out, err := Touch(in, true, now)
	if err != nil || out != in {
		t.Errorf("fm-less doc changed: %q err=%v", out, err)
	}
}

// Dates land in git, so they are the SAME date for everyone: Touch reads the
// clock in UTC, never in whatever zone the server happens to run in.
func TestTouchDatesAreUTC(t *testing.T) {
	// 00:30 in Tokyo is still the previous day in UTC
	tokyo := time.FixedZone("JST", 9*3600)
	out, err := Touch("---\ntitle: A\n---\n\nbody\n", true,
		time.Date(2026, 7, 30, 0, 30, 0, 0, tokyo))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"created: 2026-07-29", "updated: 2026-07-29"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q — dates must be UTC, not the server's local day:\n%s", want, out)
		}
	}
}
