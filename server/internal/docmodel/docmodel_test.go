package docmodel

import (
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	_ = os.MkdirAll(filepath.Dir(abs), 0o755)
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanAndBrokenLinks(t *testing.T) {
	root := t.TempDir()
	put(t, root, "requirements/REQ-001.md", `---
id: REQ-001
type: Requirement
title: Transaction Reporting
status: in_review
drivers:
  - type: regulatory
    ref: regulations/mifid.md#art-26
  - type: product
    ref: "Ops T+1 settlement SLA"
implements:
  - specs/report.md
verifies: [tests/report_spec.py]
---

Body links [the spec](../specs/report.md) and [gone](/missing.md).

`+"```md\n[fenced](/never.md)\n```\n")
	put(t, root, "specs/report.md", "---\ntype: Specification\ntitle: Reporting Spec\n---\n\nx\n")
	put(t, root, "regulations/mifid.md", "---\ntype: Regulation\ntitle: MiFID\n---\n\nx\n")
	put(t, root, "requirements/index.md", "# requirements\n\n- [x](/nowhere.md)\n") // reserved: skipped

	docs, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 3 {
		t.Fatalf("want 3 docs, got %d: %+v", len(docs), docs)
	}
	var req Doc
	for _, d := range docs {
		if d.Path == "requirements/REQ-001.md" {
			req = d
		}
	}
	if req.ID != "REQ-001" || req.Type != "Requirement" || req.Status != "in_review" {
		t.Fatalf("frontmatter parse: %+v", req)
	}
	if got := req.Links["implements"]; len(got) != 1 || got[0] != "specs/report.md" {
		t.Fatalf("implements: %v", req.Links)
	}
	if got := req.Links["verifies"]; len(got) != 1 || got[0] != "tests/report_spec.py" {
		t.Fatalf("verifies (inline list): %v", req.Links)
	}
	if got := req.Links["drivers"]; len(got) != 2 || got[0] != "regulations/mifid.md#art-26" || got[1] != "Ops T+1 settlement SLA" {
		t.Fatalf("drivers refs: %v", req.Links)
	}
	// body refs: relative resolved, fenced ignored, broken still listed
	if len(req.References) != 2 || req.References[0] != "specs/report.md" || req.References[1] != "missing.md" {
		t.Fatalf("references: %v", req.References)
	}

	broken := BrokenLinks(root, docs)
	// missing.md is the only broken .md target: the prose driver ref is not a
	// file, tests/report_spec.py is not .md, the anchor ref resolves
	if len(broken) != 1 || broken[0] != "requirements/REQ-001.md: reference -> missing.md" {
		t.Fatalf("broken: %v", broken)
	}
}
