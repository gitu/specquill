package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"specquill/server/internal/ai"
	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
	"specquill/server/internal/store"
)

func TestResolveDriftScope(t *testing.T) {
	files := map[string]string{
		"specs/a.md":            "a",
		"specs/deep/b.md":       "b",
		"requirements/r.md":     "r",
		"specs/index.md":        "generated",
		"uploads/pic.md":        "asset",
		".specquill/config.yml": "cfg",
		"readme.txt":            "not md",
	}
	// folder expansion + explicit file, deduped and sorted
	got := resolveDriftScope(files, []string{"specs/", "requirements/r.md", "specs/a.md"}, nil)
	want := []string{"requirements/r.md", "specs/a.md", "specs/deep/b.md"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("scope = %v, want %v", got, want)
	}
	// no request → config default paths
	got = resolveDriftScope(files, nil, []string{"requirements/"})
	if fmt.Sprint(got) != fmt.Sprint([]string{"requirements/r.md"}) {
		t.Fatalf("config-scoped = %v", got)
	}
	// nothing at all → every candidate doc (no generated/uploads/dotfiles)
	got = resolveDriftScope(files, nil, nil)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("full scope = %v, want %v", got, want)
	}
}

func TestDriftFingerprintStability(t *testing.T) {
	a := driftFingerprint("specs/a.md", "reg", "contradiction", "REQ-1")
	b := driftFingerprint("specs/a.md", "reg", "contradiction", "  req-1  ")
	if a != b {
		t.Fatal("fingerprint must normalize anchor whitespace/case")
	}
	if a == driftFingerprint("specs/a.md", "reg", "contradiction", "REQ-2") {
		t.Fatal("different anchors must differ")
	}
}

func TestVerifyEvidence(t *testing.T) {
	sources := []ai.GroundingSource{{Name: "reg", Files: map[string]string{
		"rules.md": "Timestamps  shall use\nmicrosecond precision.",
	}}}
	ok := func(f modelFinding) bool { return verifyEvidence(f, sources) }
	base := modelFinding{Source: "reg", Evidence: []driftEvidence{{Path: "rules.md", Quote: "microsecond precision"}}}
	if !ok(base) {
		t.Fatal("verbatim quote must verify")
	}
	ws := base
	ws.Evidence = []driftEvidence{{Path: "~reg/rules.md", Quote: "shall use microsecond"}}
	if !ok(ws) {
		t.Fatal("whitespace-normalized quote with ~source path must verify")
	}
	for name, f := range map[string]modelFinding{
		"hallucinated quote": {Source: "reg", Evidence: []driftEvidence{{Path: "rules.md", Quote: "nanosecond"}}},
		"unknown file":       {Source: "reg", Evidence: []driftEvidence{{Path: "other.md", Quote: "microsecond"}}},
		"unknown source":     {Source: "ghost", Evidence: []driftEvidence{{Path: "rules.md", Quote: "microsecond"}}},
		"no evidence":        {Source: "reg"},
		"empty quote":        {Source: "reg", Evidence: []driftEvidence{{Path: "rules.md", Quote: "  "}}},
	} {
		if ok(f) {
			t.Fatalf("%s must not verify", name)
		}
	}
}

// testDriftServer wires a writable workspace (one spec doc), a cataloged
// read-only source, an AI fake scripted per request (replies are plain
// content — the fake wraps them as SSE for streaming calls and as a JSON
// completion for one-shot calls; prompts sees each request's user message),
// and a fake forge target.
func testDriftServer(t *testing.T, aiResponses []string) (h http.Handler, st *store.Store, forgeSrv *httptest.Server, issuePosts *int, prompts *[]string) {
	t.Helper()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	gitRun(t, "init", "-b", "main", src)
	if err := os.MkdirAll(filepath.Join(src, ".specquill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgYML := "version: 2\nproject: w\nreferences:\n  - source: reg\n    grounding: true\ndrift:\n  targets: [board]\n"
	if err := os.WriteFile(filepath.Join(src, ".specquill", "config.yml"), []byte(cfgYML), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := "---\nid: REQ-1\ntitle: Timestamps\nstatus: approved\n---\n\n# Timestamps\n\nMillisecond precision suffices.\n"
	if err := os.WriteFile(filepath.Join(src, "specs", "txn.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "add", "-A")
	gitRun(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "init")

	reg := filepath.Join(tmp, "reg-src")
	gitRun(t, "init", "-b", "main", reg)
	if err := os.WriteFile(filepath.Join(reg, "rules.md"), []byte("RTS 22 requires microsecond timestamps for reports."), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", reg, "-c", "user.name=t", "-c", "user.email=t@t", "add", "-A")
	gitRun(t, "-C", reg, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "reg")

	// scripted AI fake (OpenAI-compatible; streaming and one-shot)
	aiCalls := 0
	prompts = &[]string{}
	aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if aiCalls >= len(aiResponses) {
			t.Errorf("unexpected AI request #%d", aiCalls+1)
			return
		}
		var req struct {
			Stream   bool `json:"stream"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, m := range req.Messages {
			if m.Role == "user" {
				*prompts = append(*prompts, m.Content)
			}
		}
		content := aiResponses[aiCalls]
		aiCalls++
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": content}}}})
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}})
		_, _ = w.Write(raw)
	}))
	t.Cleanup(aiSrv.Close)

	// fake forge: marker search + issue creation (github shape under /api/v3)
	posts := 0
	var issueBodies []string
	forgeSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/issues"):
			// marker search: return previously created issues
			out := "["
			for i, b := range issueBodies {
				if i > 0 {
					out += ","
				}
				raw, _ := json.Marshal(b)
				out += fmt.Sprintf(`{"number":%d,"title":"t","state":"open","body":%s,"html_url":"https://forge.test/acme/specs/issues/%d"}`, i+1, raw, i+1)
			}
			_, _ = w.Write([]byte(out + "]"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues"):
			posts++
			var body struct{ Body string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			issueBodies = append(issueBodies, body.Body)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"number":%d,"title":"t","state":"open","html_url":"https://forge.test/acme/specs/issues/%d"}`, len(issueBodies), len(issueBodies))))
		default:
			t.Errorf("unexpected forge request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(forgeSrv.Close)

	cfg := &config.Config{
		DataDir: filepath.Join(tmp, "data"),
		BaseURL: "http://spec.test",
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth:    config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Repos: []config.RepoConfig{
			{ID: "w", Mode: config.Writable, Remote: src, DefaultBranch: "main"},
			{ID: "reg", Mode: config.ReadOnly, Remote: reg, DefaultBranch: "main"},
		},
		WorkItemTargets: []config.TargetConfig{{
			Name: "board", Kind: "github", BaseURL: forgeSrv.URL, Project: "acme/specs",
			TokenEnv: "SPECQUILL_TEST_DRIFT_TOKEN", Labels: []string{"from-specquill"},
		}},
	}
	st = store.OpenTest(t)
	if err := st.SyncProjects([]store.Project{{ProjectID: "w", RepoID: "w"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SyncSources([]store.Source{{Name: "reg", Kind: "git", Remote: reg, DefaultBranch: "main", SyncInterval: 300}}); err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword("hunter2secret")
	if err := st.AddLocalUser("flo", "Flo Test", "flo@test.local", hash); err != nil {
		t.Fatal(err)
	}
	git, err := gitx.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.Init(); err != nil {
		t.Fatal(err)
	}
	h = New(cfg, git, Options{
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		AI:       ai.New(config.AIConfig{Enabled: true, BaseURL: aiSrv.URL, Model: "test-1"}),
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	return h, st, forgeSrv, &posts, prompts
}

// waitDrift polls until the latest run leaves `running`, returning the drift payload.
func waitDrift(t *testing.T, h http.Handler, cookie *http.Cookie) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		code, out := doJSON(t, h, cookie, "GET", "/api/repos/w/drift?branch=main", nil)
		if code != http.StatusOK {
			t.Fatalf("get drift: %d %v", code, out)
		}
		run, _ := out["run"].(map[string]any)
		if run != nil && run["status"] != "running" {
			return out
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("drift run never finished")
	return nil
}

func TestDriftRunVerifiesEvidenceAndKeepsDismissals(t *testing.T) {
	findings := func(title string) string {
		return `{"findings":[
			{"anchor":"REQ-1","source":"reg","kind":"contradiction","severity":"high","title":"` + title + `",
			 "detail":"spec says ms, regulation says µs","evidence":[{"path":"rules.md","quote":"microsecond timestamps"}]},
			{"anchor":"REQ-9","source":"reg","kind":"contradiction","severity":"low","title":"bogus",
			 "detail":"made up","evidence":[{"path":"rules.md","quote":"THIS IS NOT IN THE FILE"}]}
		]}`
	}
	h, _, _, _, _ := testDriftServer(t, []string{findings("first title"), findings("reworded title")})
	cookie := login(t, h)

	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("run: %d %v", code, out)
	}
	if out["docsTotal"].(float64) != 1 {
		t.Fatalf("docsTotal = %v", out["docsTotal"])
	}
	drift := waitDrift(t, h, cookie)
	run := drift["run"].(map[string]any)
	if run["status"] != "ok" {
		t.Fatalf("run = %v", run)
	}
	if run["droppedUnverified"].(float64) != 1 {
		t.Fatalf("hallucinated evidence must be dropped: %v", run)
	}
	// live feedback: the feed narrates the run, each unit, every finding it
	// kept and every one it dropped
	activity, _ := run["activity"].([]any)
	feed := ""
	for _, l := range activity {
		feed += l.(string) + "\n"
	}
	for _, want := range []string{
		"drift check of 1 document against ~reg", // what the run is doing
		"[1/1] specs/txn.md",                     // progress per unit
		"⚠ high contradiction @ REQ-1",           // the finding it kept
		"evidence not found in the source",       // and the one it dropped
		"▪ ok —",                                 // closing summary
	} {
		if !strings.Contains(feed, want) {
			t.Fatalf("activity feed missing %q:\n%s", want, feed)
		}
	}
	// the git-native report landed in the repo (main is unprotected here)
	if run["reportPath"] != "reports/source-alignment.md" || run["reportBranch"] != "main" {
		t.Fatalf("report target wrong: %v", run)
	}
	code, report := doJSON(t, h, cookie, "GET", "/api/repos/w/files/reports/source-alignment.md?ref=main", nil)
	if code != http.StatusOK {
		t.Fatalf("report not written: %d", code)
	}
	rep := report["content"].(string)
	for _, want := range []string{"# Source Alignment", "<!-- specquill:alignment:begin",
		"first title", "## Run activity", "1 finding(s) whose evidence did not verify"} {
		if !strings.Contains(rep, want) {
			t.Fatalf("report missing %q:\n%s", want, rep)
		}
	}
	list := drift["findings"].([]any)
	if len(list) != 1 {
		t.Fatalf("want 1 verified finding, got %v", list)
	}
	f := list[0].(map[string]any)
	if f["anchor"] != "REQ-1" || f["severity"] != "high" || f["title"] != "first title" {
		t.Fatalf("unexpected finding: %v", f)
	}
	fp := f["fingerprint"].(string)

	// dismiss, re-run with a REWORDED title → same fingerprint, still dismissed
	if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/"+fp+"/dismiss?branch=main", nil); code != http.StatusOK {
		t.Fatalf("dismiss: %d %v", code, out)
	}
	if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main", map[string]any{}); code != http.StatusOK {
		t.Fatalf("re-run: %d %v", code, out)
	}
	drift = waitDrift(t, h, cookie)
	list = drift["findings"].([]any)
	if len(list) != 1 {
		t.Fatalf("want 1 finding after re-run, got %v", list)
	}
	f = list[0].(map[string]any)
	if f["fingerprint"] != fp {
		t.Fatal("reworded title must not mint a new fingerprint")
	}
	if f["status"] != "dismissed" || f["title"] != "reworded title" {
		t.Fatalf("dismissal must stick while display refreshes: %v", f)
	}

	// available targets: the in-repo selection ∩ catalog (no implicit — no forge on w)
	targets := drift["targets"].([]any)
	if len(targets) != 1 || targets[0].(map[string]any)["name"] != "board" {
		t.Fatalf("targets = %v", targets)
	}

	// the second run appended to the report's accumulated run log
	_, report = doJSON(t, h, cookie, "GET", "/api/repos/w/files/reports/source-alignment.md?ref=main", nil)
	if got := strings.Count(report["content"].(string), "\n- 20"); got != 2 {
		t.Fatalf("run log must accumulate one line per run, got %d:\n%s", got, report["content"])
	}
}

func TestDriftRemedyCreatesLinkedWorkItemAndChange(t *testing.T) {
	workItem := `{"path":"work-items/WI-timestamps.md","content":"---\ntitle: Fix timestamp precision\ntype: Work Item\nstatus: backlog\n---\n\n# Fix timestamp precision\n\nRaise the reported precision to microseconds.\n"}`
	change := `{"path":"changes/2026-08-timestamps.md","content":"---\ntitle: RTS 22 precision amendment\ntype: Change Record\nstatus: triage\n---\n\n# RTS 22 precision amendment\n\nThe regulation now demands microsecond timestamps.\n"}`
	h, st, _, _, _ := testDriftServer(t, []string{workItem, change})
	cookie := login(t, h)

	f := store.DriftFinding{
		RepoKey: "w", Branch: "main", Fingerprint: "fp-remedy", RunID: 1,
		DocPath: "specs/txn.md", Anchor: "REQ-1", Source: "reg", Kind: "contradiction",
		Severity: "high", Title: "Timestamp precision drift", Detail: "ms vs µs",
		EvidenceJSON: `[{"path":"rules.md","quote":"microsecond timestamps"}]`,
	}
	if err := st.UpsertDriftFinding(f); err != nil {
		t.Fatal(err)
	}

	// work item: the NEW document carries `delivers:` → the drifted spec
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/fp-remedy/remedy?branch=main",
		map[string]string{"kind": "work_item"})
	if code != http.StatusOK {
		t.Fatalf("remedy: %d %v", code, out)
	}
	if out["path"] != "work-items/WI-timestamps.md" || out["kind"] != "work_item" {
		t.Fatalf("unexpected remedy: %v", out)
	}
	_, file := doJSON(t, h, cookie, "GET", "/api/repos/w/files/work-items/WI-timestamps.md?ref=main", nil)
	content := file["content"].(string)
	if !strings.Contains(content, "delivers:") || !strings.Contains(content, "specs/txn.md") {
		t.Fatalf("work item missing typed link:\n%s", content)
	}
	if !strings.Contains(content, "created:") {
		t.Fatalf("dates not maintained:\n%s", content)
	}
	got, _ := st.DriftFinding("w", "main", "fp-remedy")
	if got.RemedyPath != "work-items/WI-timestamps.md" || got.RemedyKind != "work_item" {
		t.Fatalf("remedy not recorded: %+v", got)
	}
	// idempotent: the existing doc comes back instead of a second draft
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/fp-remedy/remedy?branch=main",
		map[string]string{"kind": "work_item"})
	if code != http.StatusOK || out["existing"] != true {
		t.Fatalf("re-remedy must return the existing doc: %d %v", code, out)
	}

	// a change record for a REQUIREMENT: no change→requirement link exists in
	// that direction, so the requirement points UP at the change instead
	req := "---\nid: REQ-9\ntitle: Reporting\ntype: Requirement\nstatus: approved\n---\n\n# Reporting\n"
	if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/requirements/REQ-9.md?branch=main",
		map[string]string{"content": req, "baseSha": ""}); code != http.StatusOK {
		t.Fatalf("put requirement: %d %v", code, out)
	}
	f2 := f
	f2.Fingerprint, f2.DocPath = "fp-change", "requirements/REQ-9.md"
	if err := st.UpsertDriftFinding(f2); err != nil {
		t.Fatal(err)
	}
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/fp-change/remedy?branch=main",
		map[string]string{"kind": "change"})
	if code != http.StatusOK {
		t.Fatalf("change remedy: %d %v", code, out)
	}
	if out["path"] != "changes/2026-08-timestamps.md" {
		t.Fatalf("unexpected change path: %v", out)
	}
	_, file = doJSON(t, h, cookie, "GET", "/api/repos/w/files/requirements/REQ-9.md?ref=main", nil)
	if content := file["content"].(string); !strings.Contains(content, "drivers:") ||
		!strings.Contains(content, "changes/2026-08-timestamps.md") {
		t.Fatalf("requirement should point up at the change:\n%s", content)
	}

	// the report's findings table carries the remedy documents
	live, err := st.DriftFindings("w", "main")
	if err != nil {
		t.Fatal(err)
	}
	block := driftReportBlock(&store.DriftRun{Mode: "drift", Status: "ok", ScopeJSON: "[]"}, live, nil)
	if !strings.Contains(block, "work item: work-items/WI-timestamps.md") ||
		!strings.Contains(block, "change: changes/2026-08-timestamps.md") {
		t.Fatalf("report must list remedies:\n%s", block)
	}
}

func TestDriftRemedyRejectsUnknownKind(t *testing.T) {
	h, st, _, _, _ := testDriftServer(t, nil)
	cookie := login(t, h)
	if err := st.UpsertDriftFinding(store.DriftFinding{RepoKey: "w", Branch: "main",
		Fingerprint: "fp", RunID: 1, DocPath: "specs/txn.md"}); err != nil {
		t.Fatal(err)
	}
	if code, _ := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/fp/remedy?branch=main",
		map[string]string{"kind": "regulation"}); code != http.StatusBadRequest {
		t.Fatalf("unknown kind must 400, got %d", code)
	}
	if code, _ := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/nope/remedy?branch=main",
		map[string]string{"kind": "change"}); code != http.StatusNotFound {
		t.Fatalf("unknown finding must 404")
	}
}

func TestDriftReportPathComesFromProjectConfig(t *testing.T) {
	if got := driftReportPath(project.DriftConfig{}); got != "reports/source-alignment.md" {
		t.Fatalf("default = %q", got)
	}
	if got := driftReportPath(project.DriftConfig{Report: "docs/alignment/state.md"}); got != "docs/alignment/state.md" {
		t.Fatalf("configured = %q", got)
	}
	// a leading slash is tolerated and normalized (paths are project-relative)
	if got := driftReportPath(project.DriftConfig{Report: "/audits/state.md"}); got != "audits/state.md" {
		t.Fatalf("leading slash = %q", got)
	}
	// junk configuration degrades to the default instead of failing runs
	for _, bad := range []string{"../escape.md", "~src/x.md", "notmarkdown", "index.md"} {
		if got := driftReportPath(project.DriftConfig{Report: bad}); got != "reports/source-alignment.md" {
			t.Fatalf("%q should fall back, got %q", bad, got)
		}
	}
}

func TestDriftReportHonorsConfiguredLocation(t *testing.T) {
	empty := `{"findings": []}`
	h, _, _, _, _ := testDriftServer(t, []string{empty})
	cookie := login(t, h)

	// the project declares where its alignment docs live
	cfgYML := "version: 2\nproject: w\nreferences:\n  - source: reg\n    grounding: true\n" +
		"drift:\n  targets: [board]\n  report: audits/alignment-state.md\n"
	_, file := doJSON(t, h, cookie, "GET", "/api/repos/w/files/.specquill/config.yml?ref=main", nil)
	if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/.specquill/config.yml?branch=main",
		map[string]string{"content": cfgYML, "baseSha": file["sha"].(string)}); code != http.StatusOK {
		t.Fatalf("put config: %d %v", code, out)
	}

	// GET offers the configured report as the standing one
	code, out := doJSON(t, h, cookie, "GET", "/api/repos/w/drift?branch=main", nil)
	if code != http.StatusOK {
		t.Fatalf("get drift: %d %v", code, out)
	}
	reports, _ := out["reports"].([]any)
	if len(reports) != 1 || reports[0] != "audits/alignment-state.md" {
		t.Fatalf("reports = %v", reports)
	}
	// stated explicitly: the client must not have to infer it from the list
	// (sort order is no guarantee) nor fall back to a path of its own
	if out["defaultReport"] != "audits/alignment-state.md" {
		t.Fatalf("defaultReport = %v", out["defaultReport"])
	}

	// and a run with no explicit target writes exactly there
	if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main", map[string]any{}); code != http.StatusOK {
		t.Fatalf("run: %d %v", code, out)
	}
	drift := waitDrift(t, h, cookie)
	if drift["run"].(map[string]any)["reportPath"] != "audits/alignment-state.md" {
		t.Fatalf("run = %v", drift["run"])
	}
	if code, _ := doJSON(t, h, cookie, "GET", "/api/repos/w/files/audits/alignment-state.md?ref=main", nil); code != http.StatusOK {
		t.Fatalf("report not written at the configured path: %d", code)
	}
}

// A monorepo project's alignment docs belong to THAT project: the report path
// is project-relative, so it lands under the project's own content_root —
// beside the .specquill/config.yml that declares it — never at the repo root.
func TestDriftReportStaysInsideTheProjectContentRoot(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	gitRun(t, "init", "-b", "main", src)
	write := func(rel, content string) {
		abs := filepath.Join(src, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/main.go", "package main\n")
	write("docs/specs/.specquill/config.yml",
		"version: 2\nproject: specs\nreferences:\n  - source: reg\n    grounding: true\ndrift:\n  report: audits/state.md\n")
	write("docs/specs/requirements/REQ-001.md", "---\nid: REQ-001\ntype: Requirement\ntitle: Login\n---\n\nbody\n")
	gitRun(t, "-C", src, "add", "-A")
	gitRun(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")

	reg := filepath.Join(tmp, "reg-src")
	gitRun(t, "init", "-b", "main", reg)
	if err := os.WriteFile(filepath.Join(reg, "rules.md"), []byte("microsecond timestamps"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", reg, "add", "-A")
	gitRun(t, "-C", reg, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "reg")

	aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"delta": map[string]any{"content": `{"findings": []}`}}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
	}))
	t.Cleanup(aiSrv.Close)

	cfg := &config.Config{
		DataDir: filepath.Join(tmp, "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth:    config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Projects: []config.ProjectConfig{{
			ID: "specs", Remote: src, ContentRoot: "docs/specs", DefaultBranch: "main",
			ProtectedBranches: []string{"_none"}, // path mapping is the subject here
		}},
		Sources: []config.SourceConfig{{Name: "reg", Kind: "git", Remote: reg, DefaultBranch: "main"}},
	}
	cfg.Normalize()
	st := store.OpenTest(t)
	if err := st.SyncProjects([]store.Project{{ProjectID: "specs", RepoID: "specs", ContentRoot: "docs/specs"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SyncSources([]store.Source{{Name: "reg", Kind: "git", Remote: reg, DefaultBranch: "main", SyncInterval: 300}}); err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword("hunter2secret")
	if err := st.AddLocalUser("flo", "Flo Test", "flo@test.local", hash); err != nil {
		t.Fatal(err)
	}
	git, err := gitx.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.Init(); err != nil {
		t.Fatal(err)
	}
	h := New(cfg, git, Options{
		Store: st, Sessions: auth.NewSessions(st, cfg),
		AI:   ai.New(config.AIConfig{Enabled: true, BaseURL: aiSrv.URL, Model: "test-1"}),
		Dist: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	cookie := login(t, h)

	if code, out := doJSON(t, h, cookie, "POST", "/api/repos/specs/drift/run?branch=main", map[string]any{}); code != http.StatusOK {
		t.Fatalf("run: %d %v", code, out)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, out := doJSON(t, h, cookie, "GET", "/api/repos/specs/drift?branch=main", nil)
		if run, _ := out["run"].(map[string]any); run != nil && run["status"] != "running" {
			// the API speaks project-relative paths
			if run["reportPath"] != "audits/state.md" {
				t.Fatalf("report path = %v", run["reportPath"])
			}
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	// …and on disk it sits under the project's content root, not the repo root
	wt := filepath.Join(cfg.DataDir, "repos", "specs", "worktrees", "main")
	if _, err := os.Stat(filepath.Join(wt, "docs", "specs", "audits", "state.md")); err != nil {
		t.Fatalf("report not under the project content root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "audits")); err == nil {
		t.Fatal("report leaked to the repository root")
	}
}

func TestDriftReportContinueAndPreserveHumanEdits(t *testing.T) {
	empty := `{"findings": []}`
	h, _, _, _, _ := testDriftServer(t, []string{empty, empty})
	cookie := login(t, h)

	// first run maintains a NAMED report (created fresh, scaffolded)
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main",
		map[string]any{"report": "reports/q3-review.md"})
	if code != http.StatusOK {
		t.Fatalf("run: %d %v", code, out)
	}
	drift := waitDrift(t, h, cookie)
	if drift["run"].(map[string]any)["reportPath"] != "reports/q3-review.md" {
		t.Fatalf("run = %v", drift["run"])
	}
	_, file := doJSON(t, h, cookie, "GET", "/api/repos/w/files/reports/q3-review.md?ref=main", nil)
	content := file["content"].(string)
	if !strings.Contains(content, "# Q3 Review") || !strings.Contains(content, "<!-- specquill:alignment:begin") {
		t.Fatalf("scaffold wrong:\n%s", content)
	}

	// the human continues working on the report OUTSIDE the engine block
	sha := file["sha"].(string)
	edited := strings.Replace(content, "# Q3 Review\n", "# Q3 Review\n\nSign-off: reviewed by Flo, ship it.\n", 1) +
		"\n## Conclusions\n\nEverything below the block is ours.\n"
	if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/reports/q3-review.md?branch=main",
		map[string]string{"content": edited, "baseSha": sha}); code != http.StatusOK {
		t.Fatalf("edit report: %d %v", code, out)
	}

	// a second run CONTINUES the same report: human text intact, block fresh
	if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main",
		map[string]any{"report": "reports/q3-review.md"}); code != http.StatusOK {
		t.Fatalf("re-run: %d %v", code, out)
	}
	waitDrift(t, h, cookie)
	_, file = doJSON(t, h, cookie, "GET", "/api/repos/w/files/reports/q3-review.md?ref=main", nil)
	content = file["content"].(string)
	for _, want := range []string{"Sign-off: reviewed by Flo", "## Conclusions", "## Run log"} {
		if !strings.Contains(content, want) {
			t.Fatalf("continued report missing %q:\n%s", want, content)
		}
	}
	if strings.Count(content, "<!-- specquill:alignment:begin") != 1 {
		t.Fatalf("engine block must be replaced, not duplicated:\n%s", content)
	}

	// the report doc never enters a run's scope, even fresh in the worktree
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main",
		map[string]any{"paths": []string{"reports/"}})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("report must be out of scope: %d %v", code, out)
	}
}

func TestDriftRunScopeAndCaps(t *testing.T) {
	h, _, _, _, _ := testDriftServer(t, nil)
	cookie := login(t, h)

	// out-of-scope path → no docs → 422
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main", map[string]any{"paths": []string{"nowhere/"}})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("empty scope: %d %v", code, out)
	}
}

func TestDriftFileFindingCreatesIssueAndBacklinks(t *testing.T) {
	h, st, _, issuePosts, _ := testDriftServer(t, nil)
	cookie := login(t, h)
	t.Setenv("SPECQUILL_TEST_DRIFT_TOKEN", "tok")

	f := store.DriftFinding{
		RepoKey: "w", Branch: "main", Fingerprint: "abc123def4567890", RunID: 1,
		DocPath: "specs/txn.md", Anchor: "REQ-1", Source: "reg", Kind: "contradiction",
		Severity: "high", Title: "Timestamp precision drift",
		Detail:       "spec says ms, regulation says µs",
		EvidenceJSON: `[{"path":"rules.md","quote":"microsecond timestamps"}]`,
	}
	if err := st.UpsertDriftFinding(f); err != nil {
		t.Fatal(err)
	}

	// an unselected target is indistinguishable from a nonexistent one
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/"+f.Fingerprint+"/file?branch=main", map[string]string{"target": "ghost"})
	if code != http.StatusNotFound {
		t.Fatalf("unselected target: %d %v", code, out)
	}

	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/"+f.Fingerprint+"/file?branch=main", map[string]string{"target": "board"})
	if code != http.StatusOK {
		t.Fatalf("file: %d %v", code, out)
	}
	if out["created"] != true || out["url"] != "https://forge.test/acme/specs/issues/1" {
		t.Fatalf("unexpected filing: %v", out)
	}
	if out["backlinked"] != true {
		t.Fatalf("backlink failed: %v", out)
	}

	// the finding records the work item
	got, err := st.DriftFinding("w", "main", f.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "filed" || got.WorkItemTarget != "board" {
		t.Fatalf("finding not filed: %+v", got)
	}

	// the doc's frontmatter carries the backlink as an uncommitted save
	code, file := doJSON(t, h, cookie, "GET", "/api/repos/w/files/specs/txn.md?ref=main", nil)
	if code != http.StatusOK {
		t.Fatalf("read doc: %d", code)
	}
	content := file["content"].(string)
	if !strings.Contains(content, "work-items:") || !strings.Contains(content, "https://forge.test/acme/specs/issues/1") {
		t.Fatalf("doc missing work-items backlink:\n%s", content)
	}

	// re-filing finds the marker instead of duplicating the issue
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/"+f.Fingerprint+"/file?branch=main", map[string]string{"target": "board"})
	if code != http.StatusOK || out["created"] != false {
		t.Fatalf("re-file must re-find: %d %v", code, out)
	}
	if *issuePosts != 1 {
		t.Fatalf("issue created %d times, want 1", *issuePosts)
	}
}

func TestGapRunAndReverseEngineering(t *testing.T) {
	gapFindings := `{"findings":[
		{"anchor":"rules.md#reporting","severity":"medium","title":"Reporting deadline uncovered",
		 "detail":"the source mandates a deadline no document covers",
		 "suggestedPath":"requirements/REQ-deadline.md",
		 "evidence":[{"path":"rules.md","quote":"microsecond timestamps"}]},
		{"anchor":"","severity":"low","title":"anchorless","detail":"d",
		 "evidence":[{"path":"rules.md","quote":"microsecond timestamps"}]},
		{"anchor":"rules.md#bogus","severity":"low","title":"bogus","detail":"d",
		 "evidence":[{"path":"rules.md","quote":"NOT IN THE FILE"}]}
	]}`
	draftReply := `{"path":"requirements/REQ-deadline.md","content":"---\nid: REQ-deadline\ntitle: Reporting deadline\ntype: requirement\nstatus: draft\ndrivers: []\n---\n\n# Reporting deadline\n\nReports carry microsecond timestamps. Derived from ~reg/rules.md.\n"}`
	h, st, _, _, _ := testDriftServer(t, []string{gapFindings, draftReply})
	cookie := login(t, h)

	// gaps mode sweeps sources, not docs
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main", map[string]any{"mode": "gaps"})
	if code != http.StatusOK {
		t.Fatalf("gap run: %d %v", code, out)
	}
	if out["mode"] != "gaps" || out["docsTotal"].(float64) != 1 {
		t.Fatalf("unexpected run: %v", out)
	}
	drift := waitDrift(t, h, cookie)
	run := drift["run"].(map[string]any)
	if run["status"] != "ok" || run["mode"] != "gaps" {
		t.Fatalf("run = %v", run)
	}
	// anchorless + hallucinated evidence both dropped
	if run["droppedUnverified"].(float64) != 2 {
		t.Fatalf("dropped = %v, want 2", run["droppedUnverified"])
	}
	list := drift["findings"].([]any)
	if len(list) != 1 {
		t.Fatalf("want 1 gap finding, got %v", list)
	}
	f := list[0].(map[string]any)
	if f["kind"] != "coverage-gap" || f["docPath"] != "" || f["suggestedPath"] != "requirements/REQ-deadline.md" {
		t.Fatalf("unexpected finding: %v", f)
	}
	fp := f["fingerprint"].(string)

	// reverse-engineer the missing requirement from the gap
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/"+fp+"/draft?branch=main", nil)
	if code != http.StatusOK {
		t.Fatalf("draft: %d %v", code, out)
	}
	if out["path"] != "requirements/REQ-deadline.md" || out["branch"] != "main" {
		t.Fatalf("unexpected draft: %v", out)
	}
	code, file := doJSON(t, h, cookie, "GET", "/api/repos/w/files/requirements/REQ-deadline.md?ref=main", nil)
	if code != http.StatusOK {
		t.Fatalf("read draft: %d", code)
	}
	content := file["content"].(string)
	if !strings.Contains(content, "type: requirement") || !strings.Contains(content, "created:") {
		t.Fatalf("draft missing frontmatter/dates:\n%s", content)
	}
	got, err := st.DriftFinding("w", "main", fp)
	if err != nil || got.DraftPath != "requirements/REQ-deadline.md" {
		t.Fatalf("finding not linked to draft: %+v (%v)", got, err)
	}

	// drafting a finding that has a document is refused
	if err := st.UpsertDriftFinding(store.DriftFinding{RepoKey: "w", Branch: "main",
		Fingerprint: "hasdoc", RunID: 1, DocPath: "specs/txn.md"}); err != nil {
		t.Fatal(err)
	}
	if code, _ := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/findings/hasdoc/draft?branch=main", nil); code != http.StatusBadRequest {
		t.Fatalf("draft of doc-backed finding must 400, got %d", code)
	}
}

func TestExtractDividesConquersAndMatches(t *testing.T) {
	survey := `{"areas":[
		{"name":"Reporting","summary":"submitting trades","paths":["rules.md"]},
		{"name":"","summary":"nameless — dropped"}
	]}`
	area := `{"requirements":[
		{"title":"Precision","statement":"Timestamps SHALL be microsecond precise.",
		 "evidence":[{"path":"rules.md","quote":"microsecond timestamps"}]},
		{"title":"Ghost","statement":"Dropped by verification.",
		 "evidence":[{"path":"rules.md","quote":"NOT IN THE FILE"}]},
		{"title":"Venue","statement":"Reports SHALL name the venue.",
		 "evidence":[{"path":"rules.md","quote":"RTS 22 requires"}]}
	]}`
	// the walk: one covered, one claimed without a document (degrades to none)
	matches := `{"matches":[
		{"index":1,"coverage":"full","document":"specs/txn.md","note":"the spec states it"},
		{"index":2,"coverage":"partial","document":"docs/nope.md","note":"invented document"}
	]}`
	h, _, _, _, prompts := testDriftServer(t, []string{survey, area, matches, `{"findings": []}`})
	cookie := login(t, h)

	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main", map[string]any{"mode": "extract"})
	if code != http.StatusOK || out["mode"] != "extract" {
		t.Fatalf("extract run: %d %v", code, out)
	}
	drift := waitDrift(t, h, cookie)
	run := drift["run"].(map[string]any)
	if run["status"] != "ok" {
		t.Fatalf("run = %v", run)
	}
	if run["droppedUnverified"].(float64) != 1 { // the hallucinated requirement
		t.Fatalf("dropped = %v, want 1", run["droppedUnverified"])
	}
	feed := ""
	for _, l := range run["activity"].([]any) {
		feed += l.(string) + "\n"
	}
	for _, want := range []string{
		"divided ~reg into 1 area",            // divide
		"area 1/1: Reporting",                 // conquer, per area
		"matching 1-2 of 2 against the specs", // the iterative walk
		"matched 1 of 2 requirements to documents",
	} {
		if !strings.Contains(feed, want) {
			t.Fatalf("activity missing %q:\n%s", want, feed)
		}
	}

	code, file := doJSON(t, h, cookie, "GET", "/api/repos/w/files/reports/extracted-reg.md?ref=main", nil)
	if code != http.StatusOK {
		t.Fatalf("inventory not persisted: %d", code)
	}
	doc := file["content"].(string)
	for _, want := range []string{
		"type: extraction",
		"<!-- specquill:extraction:begin",
		"2 requirement(s) across 1 area(s)",
		"Coverage: 1 fully stated by a document, 0 partially, 1 not covered",
		"## Reporting",
		"Timestamps SHALL be microsecond precise.",
		"“microsecond timestamps”",
		"✓ full",
		"specs/txn.md",
		"— *not covered*", // the match naming a nonexistent document
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("inventory missing %q:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, "Dropped by verification") || strings.Contains(doc, "docs/nope.md") {
		t.Fatalf("unverified requirement or invented document leaked:\n%s", doc)
	}

	// the inventory is engine-owned: never an audited document
	files := map[string]string{"reports/extracted-reg.md": doc, "specs/a.md": "x"}
	if got := resolveDriftScope(files, nil, nil); fmt.Sprint(got) != "[specs/a.md]" {
		t.Fatalf("extraction must stay out of run scopes, got %v", got)
	}

	// …and a later drift run starts FROM it rather than re-deriving it
	if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main",
		map[string]any{"paths": []string{"specs/txn.md"}}); code != http.StatusOK {
		t.Fatalf("drift run: %d %v", code, out)
	}
	waitDrift(t, h, cookie)
	last := (*prompts)[len(*prompts)-1]
	if !strings.Contains(last, "What the application requires (extracted earlier") ||
		!strings.Contains(last, "Timestamps SHALL be microsecond precise.") {
		t.Fatalf("drift prompt did not carry the extracted baseline:\n%s", last)
	}
}

func TestGapsRestrictToSourcesAndFocus(t *testing.T) {
	empty := `{"findings": []}`
	h, _, _, _, prompts := testDriftServer(t, []string{empty})
	cookie := login(t, h)

	// an unknown source leaves nothing to sweep
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main",
		map[string]any{"mode": "gaps", "sources": []string{"nope"}})
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown source restriction: %d %v", code, out)
	}

	// a focused sweep over the named source only
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/drift/run?branch=main",
		map[string]any{"mode": "gaps", "sources": []string{"~reg"}, "focus": "  data retention  "})
	if code != http.StatusOK {
		t.Fatalf("focused run: %d %v", code, out)
	}
	if out["sources"].(float64) != 1 || out["focus"] != "data retention" {
		t.Fatalf("run did not record the restriction/focus: %v", out)
	}
	drift := waitDrift(t, h, cookie)
	run := drift["run"].(map[string]any)
	if scope, _ := run["scope"].([]any); len(scope) != 1 || scope[0] != "reg" {
		t.Fatalf("scope = %v", run["scope"])
	}
	feed := ""
	for _, l := range run["activity"].([]any) {
		feed += l.(string) + "\n"
	}
	if !strings.Contains(feed, "focus: data retention") || !strings.Contains(feed, "(~reg)") {
		t.Fatalf("activity should name the restriction and focus:\n%s", feed)
	}
	// …and the focus reaches the model as a hard constraint
	sys := ""
	for _, p := range *prompts {
		sys += p
	}
	if !strings.Contains(sys, "~reg") {
		t.Fatalf("gap prompt did not run on the restricted source:\n%s", sys)
	}
}

func TestFocusProposalsFilterUnknownSources(t *testing.T) {
	focus := `{"areas":[
		{"name":"Data retention","reason":"4 of 6 uncovered","sources":["reg","ghost"]},
		{"name":"","reason":"nameless — dropped","sources":[]}
	]}`
	h, _, _, _, _ := testDriftServer(t, []string{focus})
	cookie := login(t, h)

	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/drift/focus?branch=main", map[string]any{})
	if code != http.StatusOK {
		t.Fatalf("focus: %d %v", code, out)
	}
	areas, _ := out["areas"].([]any)
	if len(areas) != 1 {
		t.Fatalf("nameless area must be dropped: %v", areas)
	}
	a := areas[0].(map[string]any)
	if a["name"] != "Data retention" || a["reason"] != "4 of 6 uncovered" {
		t.Fatalf("unexpected area: %v", a)
	}
	if srcs, _ := a["sources"].([]any); len(srcs) != 1 || srcs[0] != "reg" {
		t.Fatalf("unknown source must be filtered out: %v", a["sources"])
	}
}
