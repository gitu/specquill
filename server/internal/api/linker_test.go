package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDocLinksAndFields(t *testing.T) {
	files := map[string]string{".specquill/config.yml": "link_types:\n  needs: {from: spec, to: requirement}\n"}
	if got := linkFieldNames(files); len(got) != 1 || got[0] != "needs" {
		t.Fatalf("declared link_types must replace defaults, got %v", got)
	}
	if got := linkFieldNames(map[string]string{}); fmt.Sprint(got) != "[delivers drivers implements maps_to verifies]" {
		t.Fatalf("default link fields wrong: %v", got)
	}

	doc := "---\nimplements:\n  - requirements/REQ-1.md\n  - requirements/REQ-2.md#sec\nmaps_to: data/trade.md\n---\nbody"
	links := docLinks(doc, []string{"implements", "maps_to"})
	if len(links["implements"]) != 2 || links["implements"][1] != "requirements/REQ-2.md" {
		t.Fatalf("fragments must strip: %v", links)
	}
	if len(links["maps_to"]) != 1 || links["maps_to"][0] != "data/trade.md" {
		t.Fatalf("scalar link values must parse: %v", links)
	}
}

func TestWorkspaceModelAndLinkBetween(t *testing.T) {
	entities, links := workspaceModel(map[string]string{})
	if entities["work_item"].Folder != "work-items/" || entities["change"].Group != "why" {
		t.Fatalf("default entities wrong: %+v", entities)
	}
	// work_item delivers → spec: the NEW document carries the link
	if field, onFrom := linkBetween(links, "work_item", "spec"); field != "delivers" || !onFrom {
		t.Fatalf("work_item→spec = (%q,%v), want (delivers,true)", field, onFrom)
	}
	// a change is pointed AT by requirements — the target carries the link
	if field, onFrom := linkBetween(links, "change", "requirement"); field != "drivers" || onFrom {
		t.Fatalf("change→requirement = (%q,%v), want (drivers,false)", field, onFrom)
	}
	// no link type joins a change and a spec directly
	if field, _ := linkBetween(links, "change", "spec"); field != "" {
		t.Fatalf("change→spec should have no link, got %q", field)
	}

	// config overrides folders and replaces link types wholesale
	cfg := map[string]string{".specquill/config.yml": "entities:\n  work_item: {folder: tasks/}\n" +
		"link_types:\n  handles: {from: work_item, to: spec}\n"}
	entities, links = workspaceModel(cfg)
	if entities["work_item"].Folder != "tasks/" {
		t.Fatalf("configured folder ignored: %+v", entities["work_item"])
	}
	if field, onFrom := linkBetween(links, "work_item", "spec"); field != "handles" || !onFrom {
		t.Fatalf("configured link ignored: (%q,%v)", field, onFrom)
	}
}

func TestDocKind(t *testing.T) {
	entities, _ := workspaceModel(map[string]string{})
	// folder wins
	if k := docKind("specs/txn.md", "---\ntype: Whatever\n---\n", entities); k != "spec" {
		t.Fatalf("folder classification = %q", k)
	}
	// frontmatter type is the fallback, matched loosely ("Change Record")
	if k := docKind("misc/x.md", "---\ntype: Change Record\n---\n", entities); k != "change" {
		t.Fatalf("frontmatter classification = %q", k)
	}
	if k := docKind("misc/x.md", "no frontmatter", entities); k != "" {
		t.Fatalf("unclassifiable doc = %q", k)
	}
}

func TestBuildLinkIndexBothDirections(t *testing.T) {
	files := map[string]string{
		"specs/a.md":        "---\nimplements: [requirements/r.md]\n---\n",
		"requirements/r.md": "---\ntitle: R\n---\n",
		"specs/ghost.md":    "---\nimplements: [requirements/missing.md]\n---\n",
	}
	idx := buildLinkIndex(files, []string{"implements"})
	if len(idx.outbound["specs/a.md"]) != 1 || idx.outbound["specs/a.md"][0] != "requirements/r.md" {
		t.Fatalf("outbound wrong: %v", idx.outbound)
	}
	if len(idx.inbound["requirements/r.md"]) != 1 || idx.inbound["requirements/r.md"][0] != "specs/a.md" {
		t.Fatalf("inbound wrong: %v", idx.inbound)
	}
	if len(idx.outbound["specs/ghost.md"]) != 0 {
		t.Fatal("targets missing from the snapshot must not resolve")
	}
	block := idx.linkedBlock(files, "specs/a.md")
	if !strings.Contains(block, "## requirements/r.md") || !strings.Contains(block, "title: R") {
		t.Fatalf("linked block missing content:\n%s", block)
	}
}

func TestDriftIncludesLinkedDocsAndLoopsWithoutCap(t *testing.T) {
	empty := `{"findings": []}`
	h, _, _, _, prompts := testDriftServer(t, []string{empty, empty, empty})
	cookie := login(t, h)

	// a second doc linking up to the committed one (uncommitted save counts)
	linked := "---\nid: SPEC-9\ntitle: Linked spec\nstatus: draft\nimplements:\n  - specs/txn.md\n---\n\n# Linked spec\n"
	if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/specs/linked.md?branch=main",
		map[string]string{"content": linked, "baseSha": ""}); code != http.StatusOK {
		t.Fatalf("put linked doc: %d %v", code, out)
	}

	// no max_docs configured → the whole 2-doc scope simply loops
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main", map[string]any{})
	if code != http.StatusOK || out["docsTotal"].(float64) != 2 {
		t.Fatalf("uncapped run: %d %v", code, out)
	}
	drift := waitDrift(t, h, cookie)
	if drift["run"].(map[string]any)["status"] != "ok" {
		t.Fatalf("run failed: %v", drift["run"])
	}

	// the audit of specs/linked.md carried its linked doc as context
	found := false
	for _, p := range *prompts {
		if strings.Contains(p, "# Document under audit: specs/linked.md") {
			found = true
			if !strings.Contains(p, "# Linked documents") || !strings.Contains(p, "Millisecond precision suffices") {
				t.Fatalf("linked context missing from prompt:\n%s", p)
			}
		}
	}
	if !found {
		t.Fatal("linked.md was never audited")
	}

	// an EXPLICIT max_docs stays a hard ceiling (config is worktree-aware)
	cfgYML := "version: 2\nproject: w\nreferences:\n  - source: reg\n    grounding: true\ndrift:\n  targets: [board]\n  max_docs: 1\n"
	if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/.specquill/config.yml?branch=main",
		map[string]any{"content": cfgYML, "baseSha": nil}); code != http.StatusOK && code != http.StatusConflict {
		t.Fatalf("put config: %d %v", code, out)
	} else if code == http.StatusConflict {
		// need the current sha for the committed config
		_, file := doJSON(t, h, cookie, "GET", "/api/repos/w/files/.specquill/config.yml?ref=main", nil)
		if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/.specquill/config.yml?branch=main",
			map[string]string{"content": cfgYML, "baseSha": file["sha"].(string)}); code != http.StatusOK {
			t.Fatalf("put config with sha: %d %v", code, out)
		}
	}
	if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main", map[string]any{}); code != http.StatusUnprocessableEntity {
		t.Fatalf("configured cap must refuse: %d %v", code, out)
	}
}

func TestLinkerProposeFiltersAndApplies(t *testing.T) {
	proposals := `{"proposals": [
		{"from": "specs/txn.md", "field": "implements", "to": "requirements/base.md", "reason": "realizes it"},
		{"from": "specs/txn2.md", "field": "implements", "to": "requirements/base.md", "reason": "dup"},
		{"from": "specs/txn.md", "field": "bogus_field", "to": "requirements/base.md", "reason": "bad field"},
		{"from": "specs/txn.md", "field": "implements", "to": "requirements/ghost.md", "reason": "missing"}
	]}`
	h, _, _, _, _ := testDriftServer(t, []string{proposals})
	cookie := login(t, h)

	put := func(path, content string) {
		t.Helper()
		if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/"+path+"?branch=main",
			map[string]string{"content": content, "baseSha": ""}); code != http.StatusOK {
			t.Fatalf("put %s: %d %v", path, code, out)
		}
	}
	put("requirements/base.md", "---\nid: REQ-base\ntitle: Base\ntype: requirement\nstatus: draft\n---\n\n# Base\n")
	put("specs/txn2.md", "---\nid: SPEC-2\ntitle: Two\nstatus: draft\nimplements:\n  - requirements/base.md\n---\n\n# Two\n")

	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/linker/propose?branch=main", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("propose: %d %v", code, out)
	}
	list, _ := out["proposals"].([]any)
	if len(list) != 1 || out["dropped"].(float64) != 3 {
		t.Fatalf("want 1 valid / 3 dropped, got %v", out)
	}
	p := list[0].(map[string]any)
	if p["from"] != "specs/txn.md" || p["field"] != "implements" || p["to"] != "requirements/base.md" {
		t.Fatalf("unexpected proposal: %v", p)
	}

	// apply writes the link into the from-doc's frontmatter (worktree save)
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/linker/apply?branch=main",
		map[string]any{"links": []map[string]string{{"from": "specs/txn.md", "field": "implements", "to": "requirements/base.md"}}})
	if code != http.StatusOK {
		t.Fatalf("apply: %d %v", code, out)
	}
	if applied, _ := out["applied"].([]any); len(applied) != 1 {
		t.Fatalf("apply result: %v", out)
	}
	_, file := doJSON(t, h, cookie, "GET", "/api/repos/w/files/specs/txn.md?ref=main", nil)
	content := file["content"].(string)
	if !strings.Contains(content, "implements:") || !strings.Contains(content, "requirements/base.md") {
		t.Fatalf("link not written:\n%s", content)
	}

	// idempotent re-apply: reported applied, not duplicated
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/linker/apply?branch=main",
		map[string]any{"links": []map[string]string{{"from": "specs/txn.md", "field": "implements", "to": "requirements/base.md"}}})
	if code != http.StatusOK {
		t.Fatalf("re-apply: %d %v", code, out)
	}
	if applied, _ := out["applied"].([]any); len(applied) != 1 {
		t.Fatalf("re-apply result: %v", out)
	}
	_, file = doJSON(t, h, cookie, "GET", "/api/repos/w/files/specs/txn.md?ref=main", nil)
	if strings.Count(file["content"].(string), "requirements/base.md") != 1 {
		t.Fatalf("link duplicated:\n%s", file["content"])
	}
}
