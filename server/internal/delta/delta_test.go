package delta

import (
	"strings"
	"testing"
)

const reqBefore = `---
id: REQ-042
title: Timestamp precision
status: draft
owner: unassigned
drivers: [regulations/mifid-ii.md]
---

# Timestamp precision

Context paragraph.

> **REQ-042.1 · MUST** — Timestamps SHALL be recorded to millisecond precision.
> **REQ-042.2 · SHOULD** — Clocks SHOULD sync hourly.

## Edge cases
`

const reqAfter = `---
id: REQ-042
title: Timestamp precision
status: approved
starts: 2026-09-01
drivers: [regulations/mifid-ii.md, regulations/dora.md]
---

# Timestamp precision

Context paragraph.

> **REQ-042.1 · MUST** — Timestamps SHALL be recorded to microsecond precision.
> **REQ-042.3 · MUST** — Divergence SHALL not exceed 100 microseconds.

## Verification
`

func TestDiffReadsPropertyMoves(t *testing.T) {
	d := Diff("requirements/REQ-042.md", "M", reqBefore, reqAfter)
	got := map[string][2]string{}
	for _, p := range d.Props {
		got[p.Key] = [2]string{p.Before, p.After}
	}
	if v, ok := got["status"]; !ok || v[0] != "draft" || v[1] != "approved" {
		t.Fatalf("status change: %+v", d.Props)
	}
	if v, ok := got["starts"]; !ok || v[0] != "" || v[1] != "2026-09-01" {
		t.Fatalf("added key: %+v", d.Props)
	}
	if v, ok := got["owner"]; !ok || v[1] != "" {
		t.Fatalf("removed key: %+v", d.Props)
	}
	if v, ok := got["drivers"]; !ok || v[1] != "regulations/mifid-ii.md, regulations/dora.md" {
		t.Fatalf("list change: %+v", d.Props)
	}
	if _, unchanged := got["title"]; unchanged {
		t.Fatalf("unchanged key reported: %+v", d.Props)
	}
}

func TestDiffReadsStatementChanges(t *testing.T) {
	d := Diff("requirements/REQ-042.md", "M", reqBefore, reqAfter)
	ops := map[string]StmtChange{}
	for _, s := range d.Statements {
		ops[s.ID] = s
	}
	if s := ops["REQ-042.1"]; s.Op != "modified" || !strings.Contains(s.After, "microsecond") || !strings.Contains(s.Before, "millisecond") {
		t.Fatalf("reworded statement: %+v", s)
	}
	if s := ops["REQ-042.2"]; s.Op != "removed" {
		t.Fatalf("removed statement: %+v", s)
	}
	if s := ops["REQ-042.3"]; s.Op != "added" || !strings.HasPrefix(s.After, "MUST") {
		t.Fatalf("added statement: %+v", s)
	}
	if want := []string{"+ Verification", "- Edge cases"}; strings.Join(d.Sections, "|") != strings.Join(want, "|") {
		t.Fatalf("sections: %+v", d.Sections)
	}
}

func TestDiffHandlesAddedDeletedAndNonMarkdown(t *testing.T) {
	add := Diff("requirements/REQ-050.md", "A", "", reqAfter)
	if len(add.Props) == 0 || add.Props[0].Before != "" {
		t.Fatalf("added file props: %+v", add.Props)
	}
	if len(add.Statements) != 2 || add.Statements[0].Op != "added" {
		t.Fatalf("added file statements: %+v", add.Statements)
	}
	del := Diff("requirements/REQ-042.md", "D", reqBefore, "")
	for _, s := range del.Statements {
		if s.Op != "removed" {
			t.Fatalf("deleted file statement op: %+v", s)
		}
	}
	if img := Diff("diagrams/flow.excalidraw.png", "M", "x", "y"); !img.Plain || !img.Empty() {
		t.Fatalf("non-markdown: %+v", img)
	}
	// prose-only edits have no structure to report — the client shows the diff
	plain := Diff("specs/txn.md", "M", "---\ntitle: T\n---\n\nOne wording.\n", "---\ntitle: T\n---\n\nAnother wording.\n")
	if !plain.Plain || !plain.Empty() {
		t.Fatalf("prose-only: %+v", plain)
	}
	// a document without frontmatter still yields headings/statements
	noFm := Diff("notes/x.md", "M", "# A\n", "# A\n\n## B\n")
	if len(noFm.Sections) != 1 || noFm.Sections[0] != "+ B" {
		t.Fatalf("frontmatter-less: %+v", noFm)
	}
}

func TestSummarizeIsCompact(t *testing.T) {
	out := Diff("requirements/REQ-042.md", "M", reqBefore, reqAfter).Summarize()
	for _, want := range []string{"M requirements/REQ-042.md", "~ status: draft → approved", "+ starts: 2026-09-01", "~ REQ-042.1", "+ REQ-042.3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary missing %q:\n%s", want, out)
		}
	}
}

func TestStatementsSpanWrappedBlockquoteLines(t *testing.T) {
	before := "> **REQ-001.1 · MUST** — Sessions SHALL expire\n> after thirty minutes of inactivity.\n\nProse.\n"
	after := "> **REQ-001.1 · MUST** — Sessions SHALL expire\n> after fifteen minutes of inactivity.\n\nProse.\n"
	d := Diff("requirements/REQ-001.md", "M", before, after)
	if len(d.Statements) != 1 || d.Statements[0].Op != "modified" {
		t.Fatalf("wrapped statement: %+v", d.Statements)
	}
	if !strings.Contains(d.Statements[0].After, "fifteen minutes") || !strings.Contains(d.Statements[0].Before, "thirty minutes") {
		t.Fatalf("continuation line not read: %+v", d.Statements[0])
	}
	// an unchanged wrapped statement stays silent
	if same := Diff("requirements/REQ-001.md", "M", before, before); len(same.Statements) != 0 {
		t.Fatalf("false positive: %+v", same.Statements)
	}
}
