package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"specquill/server/internal/ai"
	"specquill/server/internal/config"
	"specquill/server/internal/project"
	"specquill/server/internal/sketch"
)

// toolboxFixture boots the api test server and returns a writable toolbox on
// the "w" repo with one committed spec, plus the project for assertions.
func toolboxFixture(t *testing.T, protectMain bool) (*speccyToolbox, *project.Project) {
	t.Helper()
	_, _, gitm := testServerCfg(t, protectMain, func(cfg *config.Config) {
		if protectMain {
			cfg.Repos[0].ProtectedBranches = []string{"main"}
		}
	})
	repo, ok := gitm.Repo("w")
	if !ok {
		t.Fatal("fixture repo missing")
	}
	proj := project.New(repo, "w", "", false)
	seed := "---\ntitle: A\nstatus: draft\nupdated: 2026-01-01\n---\n\n# A\n\nretained for 5 years\n"
	branch := "ws/test"
	if err := proj.Repo.CreateBranch(branch, "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := proj.SaveFile(branch, "specs/a.md", seed, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := proj.Commit(branch, "seed", "t", "t@t", nil); err != nil {
		t.Fatal(err)
	}
	snap, err := proj.Snapshot(branch)
	if err != nil {
		t.Fatal(err)
	}
	tb := &speccyToolbox{repo: proj, branch: branch, writable: true, publish: func() {}, files: snap,
		sources: []ai.GroundingSource{{Name: "reg", Files: map[string]string{"mifid.md": "RTS 22 text\nretention: 7 years"}}}}
	return tb, proj
}

func TestSpeccyEditFileValidatesAndTouches(t *testing.T) {
	tb, proj := toolboxFixture(t, false)

	out, halt, err := tb.exec("edit_file", `{"path":"specs/a.md","search":"retained for 5 years","replace":"retained for 7 years"}`)
	if err != nil || halt {
		t.Fatalf("edit failed: %v halt=%v", err, halt)
	}
	if !strings.Contains(out, "edited specs/a.md") {
		t.Fatalf("result %q", out)
	}
	content, _, _ := proj.File(tb.branch, "specs/a.md")
	if !strings.Contains(content, "retained for 7 years") {
		t.Fatal("edit not applied")
	}
	if strings.Contains(content, "updated: 2026-01-01") {
		t.Fatal("updated date not bumped server-side")
	}
	if !strings.Contains(content, "title: A") {
		t.Fatal("frontmatter mangled")
	}

	// non-unique and missing search strings bounce back as tool errors
	if _, _, err := tb.exec("edit_file", `{"path":"specs/a.md","search":"e","replace":"E"}`); err == nil {
		t.Fatal("non-unique search accepted")
	}
	if _, _, err := tb.exec("edit_file", `{"path":"specs/a.md","search":"no such text","replace":"x"}`); err == nil {
		t.Fatal("missing search accepted")
	}
}

func TestSpeccyEditRejectsBrokenFrontmatter(t *testing.T) {
	tb, proj := toolboxFixture(t, false)
	_, _, err := tb.exec("edit_file", `{"path":"specs/a.md","search":"title: A","replace":"title: [broken"}`)
	if err == nil || !strings.Contains(err.Error(), "frontmatter") {
		t.Fatalf("broken frontmatter accepted: %v", err)
	}
	content, _, _ := proj.File(tb.branch, "specs/a.md")
	if !strings.Contains(content, "title: A") {
		t.Fatal("file must be untouched after a rejected edit")
	}
}

func TestSpeccyCreateFileSetsDatesAndRefusesOverwrite(t *testing.T) {
	tb, proj := toolboxFixture(t, false)
	doc := "---\ntitle: New req\nstatus: draft\n---\n\n# New req\n"
	if _, _, err := tb.exec("create_file", fmt.Sprintf(`{"path":"requirements/REQ-100.md","content":%q}`, doc)); err != nil {
		t.Fatal(err)
	}
	content, _, _ := proj.File(tb.branch, "requirements/REQ-100.md")
	if !strings.Contains(content, "created: ") || !strings.Contains(content, "updated: ") {
		t.Fatalf("dates not stamped:\n%s", content)
	}
	if _, _, err := tb.exec("create_file", `{"path":"specs/a.md","content":"x"}`); err == nil {
		t.Fatal("overwrite accepted")
	}
}

func TestSpeccyWriteGuards(t *testing.T) {
	tb, _ := toolboxFixture(t, false)

	// reference sources are read-only, even for a writable toolbox — for
	// move_file on EITHER end
	if _, _, err := tb.exec("edit_file", `{"path":"~reg/mifid.md","search":"a","replace":"b"}`); err == nil {
		t.Fatal("reference edit accepted")
	}
	if _, _, err := tb.exec("move_file", `{"from":"~reg/mifid.md","to":"specs/m.md"}`); err == nil {
		t.Fatal("move FROM a reference accepted")
	}
	if _, _, err := tb.exec("move_file", `{"from":"specs/a.md","to":"~reg/a.md"}`); err == nil {
		t.Fatal("move INTO a reference accepted")
	}
	if _, _, err := tb.exec("delete_file", `{"path":"~reg/mifid.md"}`); err == nil {
		t.Fatal("reference delete accepted")
	}
	// OKF reserved files are regenerated at commit time — never moved/deleted
	if _, _, err := tb.exec("delete_file", `{"path":"specs/index.md"}`); err == nil {
		t.Fatal("reserved-file delete accepted")
	}
	if _, _, err := tb.exec("move_file", `{"from":"specs/a.md","to":"specs/index.md"}`); err == nil {
		t.Fatal("move onto a reserved name accepted")
	}
	// traversal is refused by MapIn/safeRelPath before git sees it
	if _, _, err := tb.exec("create_file", `{"path":"../escape.md","content":"x"}`); err == nil {
		t.Fatal("traversal accepted")
	}
	// a read-only toolbox refuses writes outright (belt to the spec gating)
	tb.writable = false
	if _, _, err := tb.exec("edit_file", `{"path":"specs/a.md","search":"5 years","replace":"6"}`); err == nil {
		t.Fatal("write on read-only conversation accepted")
	}
	if _, _, err := tb.exec("move_file", `{"from":"specs/a.md","to":"specs/b.md"}`); err == nil {
		t.Fatal("move on read-only conversation accepted")
	}
	if _, _, err := tb.exec("delete_file", `{"path":"specs/a.md"}`); err == nil {
		t.Fatal("delete on read-only conversation accepted")
	}
	if got := tb.specs(nil); len(got) != 4 {
		t.Fatalf("read-only toolbox should expose read tools only, got %d", len(got))
	}
	tb.writable = true
	if got := tb.specs(nil); len(got) != 9 {
		t.Fatalf("writable toolbox should add edit/create/move/delete/draw, got %d", len(got))
	}
}

func TestSpeccyMoveFileRewritesReferences(t *testing.T) {
	tb, proj := toolboxFixture(t, false)
	// a doc referencing the spec in frontmatter (root-relative) and body (relative)
	ref := "---\ntitle: R\nimplements:\n  - specs/a.md\n---\n\nSee [a](../specs/a.md).\n"
	if _, err := proj.SaveFile(tb.branch, "requirements/REQ-1.md", ref, ""); err != nil {
		t.Fatal(err)
	}
	tb.files["requirements/REQ-1.md"] = ref

	out, halt, err := tb.exec("move_file", `{"from":"specs/a.md","to":"specs/b.md"}`)
	if err != nil || halt {
		t.Fatalf("move failed: %v halt=%v", err, halt)
	}
	if !strings.Contains(out, "moved specs/a.md → specs/b.md") || !strings.Contains(out, "1 inbound reference updated") {
		t.Fatalf("result %q", out)
	}
	if _, _, err := proj.File(tb.branch, "specs/a.md"); err == nil {
		t.Fatal("source path still exists after move")
	}
	if content, _, _ := proj.File(tb.branch, "specs/b.md"); !strings.Contains(content, "# A") {
		t.Fatal("moved content lost")
	}
	reqContent, _, _ := proj.File(tb.branch, "requirements/REQ-1.md")
	if !strings.Contains(reqContent, "specs/b.md") || strings.Contains(reqContent, "specs/a.md") {
		t.Fatalf("inbound references not rewritten:\n%s", reqContent)
	}
	// the conversation snapshot follows the move so list_files/search stay truthful
	if _, ok := tb.files["specs/a.md"]; ok {
		t.Fatal("snapshot still lists the old path")
	}
	if !strings.Contains(tb.files["requirements/REQ-1.md"], "specs/b.md") {
		t.Fatal("snapshot missed the rewrite")
	}
	// destination-exists and missing-source surface as tool errors
	if _, _, err := tb.exec("move_file", `{"from":"specs/b.md","to":"requirements/REQ-1.md"}`); err == nil {
		t.Fatal("overwrite-by-move accepted")
	}
	if _, _, err := tb.exec("move_file", `{"from":"specs/gone.md","to":"specs/x.md"}`); err == nil {
		t.Fatal("missing source accepted")
	}
}

func TestMoveRebasesOwnDiagramLinks(t *testing.T) {
	tb, proj := toolboxFixture(t, false)
	branch := tb.branch
	if _, err := proj.SaveFile(branch, "diagrams/flow.excalidraw.png", "png-bytes", ""); err != nil {
		t.Fatal(err)
	}
	doc := "---\ntitle: D\n---\n\n![flow](../diagrams/flow.excalidraw.png)\n"
	if _, err := proj.SaveFile(branch, "specs/with-sketch.md", doc, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := proj.MoveFileRewriting(branch, "specs/with-sketch.md", "archive/deep/with-sketch.md"); err != nil {
		t.Fatal(err)
	}
	moved, _, _ := proj.File(branch, "archive/deep/with-sketch.md")
	if !strings.Contains(moved, "![flow](../../diagrams/flow.excalidraw.png)") {
		t.Fatalf("embedded sketch link not rebased:\n%s", moved)
	}
}

func TestMoveFolderRewriting(t *testing.T) {
	tb, proj := toolboxFixture(t, false)
	branch := tb.branch
	seed := map[string]string{
		// intra-folder refs in both styles: root-relative fm + relative body,
		// plus an embedded diagram OUTSIDE the moved folder
		"notes/one.md":                 "---\ntitle: One\nimplements:\n  - notes/two.md\n---\n\nSee [two](two.md).\n\n![d](../diagrams/flow.excalidraw.png)\n",
		"notes/two.md":                 "---\ntitle: Two\n---\n\nx\n",
		"diagrams/flow.excalidraw.png": "png-bytes",
		"requirements/REQ-9.md":        "---\ntitle: R\nimplements:\n  - notes/one.md\n---\n\nSee [one](../notes/one.md).\n",
	}
	for p, c := range seed {
		if _, err := proj.SaveFile(branch, p, c, ""); err != nil {
			t.Fatal(err)
		}
	}

	moved, rewritten, err := proj.MoveFolderRewriting(branch, "notes", "archive/notes")
	if err != nil {
		t.Fatal(err)
	}
	if moved != 2 {
		t.Fatalf("moved = %d, want 2", moved)
	}
	// outside references follow, in both styles
	req, _, _ := proj.File(branch, "requirements/REQ-9.md")
	if !strings.Contains(req, "archive/notes/one.md") || !strings.Contains(req, "(../archive/notes/one.md)") {
		t.Fatalf("outside references not rewritten:\n%s", req)
	}
	// intra-folder root-relative refs keep working at the new location
	one, _, _ := proj.File(branch, "archive/notes/one.md")
	if !strings.Contains(one, "- archive/notes/two.md") {
		t.Fatalf("intra-folder frontmatter ref not rewritten:\n%s", one)
	}
	if !strings.Contains(one, "[two](two.md)") {
		t.Fatalf("intra-folder relative body link must stay relative:\n%s", one)
	}
	if !strings.Contains(one, "![d](../../diagrams/flow.excalidraw.png)") {
		t.Fatalf("embedded diagram link not rebased with the folder move:\n%s", one)
	}
	if _, _, err := proj.File(branch, "notes/one.md"); err == nil {
		t.Fatal("old folder still present")
	}
	if got := len(rewritten); got != 2 { // REQ-9 + one.md
		t.Fatalf("rewritten = %v", rewritten)
	}
	// a folder cannot move into itself, and a missing folder errors
	if _, _, err := proj.MoveFolderRewriting(branch, "archive", "archive/notes/deeper"); err == nil {
		t.Fatal("move-into-self accepted")
	}
	if _, _, err := proj.MoveFolderRewriting(branch, "nope", "x"); err == nil {
		t.Fatal("missing folder accepted")
	}
}

func TestSpeccyDrawAndReadSketch(t *testing.T) {
	tb, proj := toolboxFixture(t, false)

	// draw: bare elements array wraps into the standard envelope
	out, halt, err := tb.exec("draw_sketch", `{"path":"diagrams/flow.excalidraw","scene":"[{\"type\":\"rectangle\",\"x\":10,\"y\":10,\"width\":170,\"height\":60},{\"type\":\"text\",\"x\":40,\"y\":30,\"text\":\"OMS\"}]"}`)
	if err != nil || halt {
		t.Fatalf("draw failed: %v halt=%v", err, halt)
	}
	if !strings.Contains(out, "drew diagrams/flow.excalidraw (2 elements") {
		t.Fatalf("result %q", out)
	}
	content, _, _ := proj.File(tb.branch, "diagrams/flow.excalidraw")
	if !strings.Contains(content, `"type": "excalidraw"`) || !strings.Contains(content, `"OMS"`) {
		t.Fatalf("scene not saved:\n%s", content)
	}
	// redraw replaces (create-or-update semantics)
	if _, _, err := tb.exec("draw_sketch", `{"path":"diagrams/flow.excalidraw","scene":"{\"elements\":[]}"}`); err != nil {
		t.Fatalf("redraw failed: %v", err)
	}
	// guards: extension, JSON validity, read-only, text tools refuse sketches
	if _, _, err := tb.exec("draw_sketch", `{"path":"diagrams/x.md","scene":"[]"}`); err == nil {
		t.Fatal("non-sketch path accepted")
	}
	if _, _, err := tb.exec("draw_sketch", `{"path":"diagrams/x.excalidraw","scene":"not json"}`); err == nil {
		t.Fatal("invalid scene accepted")
	}
	if _, _, err := tb.exec("create_file", `{"path":"diagrams/y.excalidraw","content":"{}"}`); err == nil {
		t.Fatal("create_file on a sketch accepted")
	}
	if _, _, err := tb.exec("edit_file", `{"path":"diagrams/z.excalidraw.png","search":"a","replace":"b"}`); err == nil {
		t.Fatal("edit_file on a binary sketch accepted")
	}
	tb.writable = false
	if _, _, err := tb.exec("draw_sketch", `{"path":"diagrams/flow.excalidraw","scene":"[]"}`); err == nil {
		t.Fatal("draw on read-only conversation accepted")
	}
	tb.writable = true

	// read: an embedded-scene PNG returns its scene JSON
	png, err := sketch.EmbedScene(tinyPng(t), `{"type":"excalidraw","version":2,"elements":[{"type":"text","text":"hello-scene"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proj.SaveFile(tb.branch, "diagrams/embedded.excalidraw.png", string(png), ""); err != nil {
		t.Fatal(err)
	}
	out, _, err = tb.exec("read_file", `{"path":"diagrams/embedded.excalidraw.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "embedded excalidraw scene") || !strings.Contains(out, "hello-scene") {
		t.Fatalf("scene not extracted: %q", out)
	}
}

// a real 1x1 transparent PNG for the embed round trip
func tinyPng(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSpeccyDeleteFile(t *testing.T) {
	tb, proj := toolboxFixture(t, false)
	out, halt, err := tb.exec("delete_file", `{"path":"specs/a.md"}`)
	if err != nil || halt {
		t.Fatalf("delete failed: %v halt=%v", err, halt)
	}
	if !strings.Contains(out, "deleted specs/a.md") {
		t.Fatalf("result %q", out)
	}
	if _, _, err := proj.File(tb.branch, "specs/a.md"); err == nil {
		t.Fatal("file still readable after delete")
	}
	if _, ok := tb.files["specs/a.md"]; ok {
		t.Fatal("snapshot still lists the deleted path")
	}
	if _, _, err := tb.exec("delete_file", `{"path":"specs/a.md"}`); err == nil {
		t.Fatal("double delete accepted")
	}
}

func TestSpeccyListAndSearch(t *testing.T) {
	tb, _ := toolboxFixture(t, false)

	// workspace listing, then a source listing under its ~prefix
	out, _, err := tb.exec("list_files", `{}`)
	if err != nil || !strings.Contains(out, "specs/a.md") {
		t.Fatalf("workspace listing: %v %q", err, out)
	}
	out, _, err = tb.exec("list_files", `{"source":"~reg"}`)
	if err != nil || out != "~reg/mifid.md" {
		t.Fatalf("source listing: %v %q", err, out)
	}
	if _, _, err := tb.exec("list_files", `{"source":"nope"}`); err == nil {
		t.Fatal("unknown source accepted")
	}

	// search spans workspace AND sources; hits carry path:line
	out, _, err = tb.exec("search", `{"query":"retained for 5"}`)
	if err != nil || !strings.Contains(out, "specs/a.md:") {
		t.Fatalf("workspace search: %v %q", err, out)
	}
	out, _, err = tb.exec("search", `{"query":"RETENTION"}`)
	if err != nil || !strings.Contains(out, "~reg/mifid.md:2:") {
		t.Fatalf("cross-source case-insensitive search: %v %q", err, out)
	}
	out, _, err = tb.exec("search", `{"query":"zzz-not-there"}`)
	if err != nil || !strings.Contains(out, "no matches") {
		t.Fatalf("empty search: %v %q", err, out)
	}
}

func TestSpeccyProtectedBranchWriteRefused(t *testing.T) {
	tb, _ := toolboxFixture(t, true)
	tb.branch = "main" // simulate a client that lied about the branch
	if _, _, err := tb.exec("create_file", `{"path":"specs/new.md","content":"# x\n"}`); err == nil {
		t.Fatal("protected-branch write accepted")
	}
	if _, _, err := tb.exec("move_file", `{"from":"specs/a.md","to":"specs/b.md"}`); err == nil {
		t.Fatal("protected-branch move accepted")
	}
	if _, _, err := tb.exec("delete_file", `{"path":"specs/a.md"}`); err == nil {
		t.Fatal("protected-branch delete accepted")
	}
}

func TestSpeccyReadFileAndReferences(t *testing.T) {
	tb, _ := toolboxFixture(t, false)
	out, _, err := tb.exec("read_file", `{"path":"specs/a.md"}`)
	if err != nil || !strings.Contains(out, "retained for 5 years") {
		t.Fatalf("read failed: %v %q", err, out)
	}
	out, _, err = tb.exec("read_file", `{"path":"~reg/mifid.md"}`)
	if err != nil || !strings.HasPrefix(out, "RTS 22 text") {
		t.Fatalf("reference read failed: %v %q", err, out)
	}
	if _, _, err := tb.exec("read_file", `{"path":"~nope/x.md"}`); err == nil {
		t.Fatal("unknown source accepted")
	}
}

func TestSpeccyAskUserHalts(t *testing.T) {
	tb, _ := toolboxFixture(t, false)
	_, halt, err := tb.exec("ask_user", `{"question":"Which retention window?","options":["5y","7y"]}`)
	if err != nil || !halt {
		t.Fatalf("ask_user: halt=%v err=%v", halt, err)
	}
	if _, _, err := tb.exec("ask_user", `{"options":["x"]}`); err == nil {
		t.Fatal("question-less ask accepted")
	}
}

// Providers validate tool JSON schemas strictly: a `required: null` (nil
// slice) is rejected with invalid_function_parameters — every spec must
// marshal with either a non-empty required array or none at all.
func TestSpeccyToolSchemasMarshalValid(t *testing.T) {
	tb, _ := toolboxFixture(t, false)
	tb.writable = true
	for _, spec := range tb.specs(nil) {
		raw, err := json.Marshal(spec.Parameters)
		if err != nil {
			t.Fatalf("%s: %v", spec.Name, err)
		}
		if strings.Contains(string(raw), `"required":null`) {
			t.Errorf("%s parameters marshal a null required: %s", spec.Name, raw)
		}
		var parsed struct {
			Type     string         `json:"type"`
			Props    map[string]any `json:"properties"`
			Required []string       `json:"required"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil || parsed.Type != "object" || parsed.Props == nil {
			t.Errorf("%s parameters malformed: %s (%v)", spec.Name, raw, err)
		}
		for _, req := range parsed.Required {
			if _, ok := parsed.Props[req]; !ok {
				t.Errorf("%s requires %q which is not a declared property", spec.Name, req)
			}
		}
	}
}

func TestModelRules(t *testing.T) {
	// no config at all: the built-in chain, folders and invariants
	v := modelRules(map[string]string{})
	for _, want := range []string{
		"WHY ← WHAT ← HOW ← WHEN",
		"`drivers:` on requirements/ (what) → regulations/ (why), changes/ (why)",
		"`implements:` on specs/ (how), data-mappings/ (how) → requirements/ (what)",
		"`delivers:` on work-items/ (when)",
		"never {type, ref}",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("default model rules missing %q in %q", want, v)
		}
	}

	// a workspace config reshapes it: custom WHY entity, hidden family, and a
	// declared link_types section replacing the defaults wholesale
	files := map[string]string{".specquill/config.yml": `
entities:
  decision: { group: why, folder: "decisions/" }
  work_item: { hidden: true }
link_types:
  drivers: { from: requirement, to: [regulation, decision] }
  satisfies: { from: spec, to: requirement }
`}
	v = modelRules(files)
	for _, want := range []string{"decisions/ (why)", "`satisfies:` on specs/ (how)"} {
		if !strings.Contains(v, want) {
			t.Errorf("configured model rules missing %q in %q", want, v)
		}
	}
	if strings.Contains(v, "delivers") {
		t.Errorf("replaced link_types must drop the default delivers: %q", v)
	}
}

// The editing rules carry the upward-link invariants and the move/delete
// tool guidance — the always-on half of the document-model skill.
func TestEditingRulesCarryModelInvariants(t *testing.T) {
	for _, want := range []string{"upward link", "plain path list", "move_file", "delete_file", "ask_user"} {
		if !strings.Contains(ai.EditingRules, want) {
			t.Errorf("EditingRules missing %q", want)
		}
	}
}

func TestWorkspaceVocabulary(t *testing.T) {
	files := map[string]string{
		".specquill/config.yml":  "statuses: [draft, approved]\nids:\n  requirement: { pattern: \"REQ-{seq:3}\" }\n",
		".specquill/schema.json": `{"fields":{"priority":{"values":{"must":"amber","could":"slate"}},"status":{"values":{"draft":"slate"}}}}`,
		"requirements/REQ-1.md":  "x",
		"specs/a.md":             "x",
	}
	v := workspaceVocabulary(files)
	for _, want := range []string{"draft, approved", "priority: could|must", "requirements/ (id pattern REQ-{seq:3})", "specs/"} {
		if !strings.Contains(v, want) {
			t.Errorf("vocabulary missing %q in %q", want, v)
		}
	}
}
