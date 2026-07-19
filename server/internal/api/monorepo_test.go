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

// Monorepo project: content lives under docs/specs/ of a bigger repo. The
// API must serve project-relative paths end-to-end — tree, file reads,
// saves, status, commit (incl. OKF regeneration under the subtree) — and
// never leak or touch sibling content.
func TestMonorepoContentRoot(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	run := func(args ...string) {
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main", src)
	write := func(rel, content string) {
		abs := filepath.Join(src, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(abs), 0o755)
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/main.go", "package main\n")
	write("docs/specs/index.md", "---\nokf_version: \"0.1\"\n---\n\n# Index\n")
	write("docs/specs/requirements/REQ-001.md", "---\nid: REQ-001\ntype: Requirement\ntitle: Login\n---\n\nbody\n")
	run("-C", src, "add", "-A")
	run("-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init")

	cfg := &config.Config{
		DataDir: filepath.Join(tmp, "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth:    config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Projects: []config.ProjectConfig{{
			ID: "specs", Remote: src, ContentRoot: "docs/specs", DefaultBranch: "main",
			// direct main writes stay allowed here — protection semantics are
			// covered by workspace_test; this test is about path mapping
			ProtectedBranches: []string{"_none"},
		}},
	}
	cfg.Normalize()
	st := store.OpenTest(t)
	ten, err := st.EnsureTenant(gitx.DefaultTenant, "config", 0, "Workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SyncTenantProjects(ten.ID, []store.Project{{ProjectID: "specs", RepoID: "specs", ContentRoot: "docs/specs"}}); err != nil {
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

	// tree: project-relative paths only, no monorepo siblings
	code, treeBody := doJSONList(t, h, cookie, "GET", "/api/repos/specs/tree")
	if code != http.StatusOK {
		t.Fatalf("tree: %d", code)
	}
	joined := strings.Join(treeBody, " ")
	if !strings.Contains(joined, "requirements/REQ-001.md") || strings.Contains(joined, "docs/specs") || strings.Contains(joined, "main.go") {
		t.Fatalf("tree leaked or unmapped: %v", treeBody)
	}

	// read + save + status, all project-relative
	code, out := doJSON(t, h, cookie, "GET", "/api/repos/specs/files/requirements/REQ-001.md", nil)
	if code != http.StatusOK || !strings.Contains(out["content"].(string), "REQ-001") {
		t.Fatalf("file read: %d %v", code, out)
	}
	sha := out["sha"].(string)
	code, _ = doJSON(t, h, cookie, "PUT", "/api/repos/specs/files/requirements/REQ-001.md?branch=main",
		map[string]string{"content": "---\nid: REQ-001\ntype: Requirement\ntitle: Login v2\n---\n\nbody v2\n", "baseSha": sha})
	if code != http.StatusOK {
		t.Fatalf("save: %d", code)
	}
	code, stat := doJSON(t, h, cookie, "GET", "/api/repos/specs/status?branch=main", nil)
	if code != http.StatusOK {
		t.Fatalf("status: %d", code)
	}
	dirty := stat["dirty"].([]any)
	if len(dirty) != 1 || dirty[0].(map[string]any)["path"] != "requirements/REQ-001.md" {
		t.Fatalf("status paths not project-relative: %v", dirty)
	}

	// escaping the content root is refused
	code, _ = doJSON(t, h, cookie, "PUT", "/api/repos/specs/files/..%2F..%2Fsrc%2Fmain.go?branch=main",
		map[string]string{"content": "x", "baseSha": ""})
	if code == http.StatusOK {
		t.Fatal("traversal write was accepted")
	}
	code, _ = doJSON(t, h, cookie, "GET", "/api/repos/specs/files/..%2F..%2Fsrc%2Fmain.go", nil)
	if code == http.StatusOK {
		t.Fatal("traversal read was accepted")
	}

	// commit regenerates OKF *under the content root* and lands in one commit
	code, _ = doJSON(t, h, cookie, "POST", "/api/repos/specs/commit?branch=main", map[string]any{"message": "update req"})
	if code != http.StatusOK {
		t.Fatalf("commit: %d", code)
	}
	code, stat = doJSON(t, h, cookie, "GET", "/api/repos/specs/status?branch=main", nil)
	if code != http.StatusOK || len(stat["dirty"].([]any)) != 0 {
		t.Fatalf("post-commit not clean: %d %v", code, stat["dirty"])
	}
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/specs/files/log.md", nil)
	if code != http.StatusOK || !strings.Contains(out["content"].(string), "update req") {
		t.Fatalf("okf log under content root: %d %v", code, out)
	}
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/specs/files/index.md", nil)
	if code != http.StatusOK || !strings.Contains(out["content"].(string), "Login v2") {
		t.Fatalf("okf index not refreshed: %d %v", code, out)
	}
}

// doJSONList decodes a JSON array response into its `path` fields (tree).
func doJSONList(t *testing.T, h http.Handler, cookie *http.Cookie, method, url string) (int, []string) {
	t.Helper()
	req := httptest.NewRequest(method, apiURL(url), nil)
	req.Header.Set("X-SpecQuill", "1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var entries []struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &entries)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return rec.Code, out
}
