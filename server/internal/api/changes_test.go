package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

// doJSONArray reads an endpoint that answers with a JSON array (doJSON only
// decodes objects).
func doJSONArray(t *testing.T, h http.Handler, cookie *http.Cookie, method, url string) (int, []any) {
	t.Helper()
	req := httptest.NewRequest(method, url, nil)
	req.Header.Set("X-SpecQuill", "1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out []any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The change feed over a monorepo project: commits outside the content root
// must not show up, paths inside it come back project-relative, and one
// commit's semantic delta says what moved in the document rather than which
// lines moved.
func TestChangeFeedIsProjectScopedAndSemantic(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	run := func(args ...string) {
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(rel, content string) {
		abs := filepath.Join(src, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) {
		run("-C", src, "add", "-A")
		run("-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", msg)
	}
	run("init", "-b", "main", src)
	write("src/main.go", "package main\n")
	write("docs/specs/requirements/REQ-001.md", "---\nid: REQ-001\ntype: Requirement\ntitle: Login\nstatus: draft\n---\n\n# Login\n\n> **REQ-001.1 · MUST** — Sessions SHALL expire after 30 minutes.\n")
	commit("init")
	write("docs/specs/requirements/REQ-001.md", "---\nid: REQ-001\ntype: Requirement\ntitle: Login\nstatus: approved\nstarts: 2026-09-01\n---\n\n# Login\n\n> **REQ-001.1 · MUST** — Sessions SHALL expire after 15 minutes.\n")
	commit("tighten session expiry")
	write("src/other.go", "package main\n")
	commit("unrelated backend change")

	cfg := &config.Config{
		DataDir: filepath.Join(tmp, "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth:    config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Projects: []config.ProjectConfig{{
			ID: "specs", Remote: src, ContentRoot: "docs/specs", DefaultBranch: "main",
		}},
	}
	cfg.Normalize()
	st := store.OpenTest(t)
	if err := st.SyncProjects([]store.Project{{ProjectID: "specs", RepoID: "specs", ContentRoot: "docs/specs"}}); err != nil {
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
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	cookie := login(t, h)

	code, log := doJSONArray(t, h, cookie, "GET", "/api/repos/specs/log?ref=main")
	if code != http.StatusOK {
		t.Fatalf("log: %d", code)
	}
	if len(log) != 2 {
		t.Fatalf("want the 2 commits touching the workspace, got %d: %v", len(log), log)
	}
	newest := log[0].(map[string]any)
	if newest["subject"] != "tighten session expiry" {
		t.Fatalf("newest commit: %v", newest)
	}
	files := newest["files"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["path"] != "requirements/REQ-001.md" {
		t.Fatalf("paths not project-relative: %v", files)
	}

	// bad since is rejected before it reaches git's date parser
	if code, _ := doJSON(t, h, cookie, "GET", "/api/repos/specs/log?since=yesterday", nil); code != http.StatusBadRequest {
		t.Fatalf("free-form since: %d", code)
	}

	sha, parent := newest["sha"].(string), newest["parent"].(string)
	code, out := doJSON(t, h, cookie, "GET", "/api/repos/specs/commit?sha="+sha+"&parent="+parent, nil)
	if code != http.StatusOK {
		t.Fatalf("commit: %d %v", code, out)
	}
	diffFiles := out["files"].([]any)
	if len(diffFiles) != 1 || diffFiles[0].(map[string]any)["path"] != "requirements/REQ-001.md" {
		t.Fatalf("commit diff: %v", diffFiles)
	}
	deltas := out["deltas"].([]any)
	if len(deltas) != 1 {
		t.Fatalf("deltas: %v", deltas)
	}
	d := deltas[0].(map[string]any)
	props := d["props"].([]any)
	seen := map[string]string{}
	for _, p := range props {
		pm := p.(map[string]any)
		seen[pm["key"].(string)], _ = pm["after"].(string)
	}
	if seen["status"] != "approved" {
		t.Fatalf("status transition missing: %v", props)
	}
	if _, ok := seen["starts"]; !ok {
		t.Fatalf("added validity key missing: %v", props)
	}
	stmts := d["statements"].([]any)
	if len(stmts) != 1 {
		t.Fatalf("statements: %v", stmts)
	}
	s := stmts[0].(map[string]any)
	if s["id"] != "REQ-001.1" || s["op"] != "modified" || !strings.Contains(s["after"].(string), "15 minutes") {
		t.Fatalf("statement delta: %v", s)
	}

	// no ai configured in this harness — the feed still works, summaries 501
	if code, _ := doJSON(t, h, cookie, "GET", "/api/repos/specs/commit/summary?sha="+sha, nil); code != http.StatusNotImplemented {
		t.Fatalf("summary without ai: %d", code)
	}
}
