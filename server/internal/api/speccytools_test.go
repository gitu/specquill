package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"specquill/server/internal/ai"
	"specquill/server/internal/config"
	"specquill/server/internal/project"
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

	// reference sources are read-only, even for a writable toolbox
	if _, _, err := tb.exec("edit_file", `{"path":"~reg/mifid.md","search":"a","replace":"b"}`); err == nil {
		t.Fatal("reference edit accepted")
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
	if got := tb.specs(nil); len(got) != 4 {
		t.Fatalf("read-only toolbox should expose read tools only, got %d", len(got))
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
	_, _, err := tb.exec("create_file", `{"path":"specs/new.md","content":"# x\n"}`)
	if err == nil {
		t.Fatal("protected-branch write accepted")
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
