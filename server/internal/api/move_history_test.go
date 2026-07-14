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

// Moving a file goes through git (staged rename), and file history follows
// the file across commits and renames (--follow).
func TestMoveAndHistory(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	run := func(args ...string) {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
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
	write("requirements/REQ-001.md", "---\ntype: Requirement\ntitle: One\n---\n\nv1\n")
	run("-C", src, "add", "-A")
	run("-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "first")
	write("requirements/REQ-001.md", "---\ntype: Requirement\ntitle: One\n---\n\nv2\n")
	run("-C", src, "add", "-A")
	run("-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "second")

	cfg := &config.Config{
		DataDir:  filepath.Join(tmp, "data"),
		Git:      config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session:  config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth:     config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Projects: []config.ProjectConfig{{ID: "r", Remote: src, DefaultBranch: "main", ProtectedBranches: []string{"_none"}}},
	}
	cfg.Normalize()
	st := store.OpenTest(t)
	ten, err := st.EnsureTenant(gitx.DefaultTenant, "config", 0, "Workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SyncTenantProjects(ten.ID, []store.Project{{ProjectID: "r", RepoID: "r"}}); err != nil {
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

	// history before the move: both commits touching the file
	code, hist := doJSONList2(t, h, cookie, "GET", "/api/repos/r/history?path=requirements/REQ-001.md&ref=main", "subject")
	if code != http.StatusOK || len(hist) != 2 || hist[0] != "second" || hist[1] != "first" {
		t.Fatalf("history: %d %v", code, hist)
	}

	// move: staged git rename in the worktree
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/r/move?branch=main",
		map[string]string{"from": "requirements/REQ-001.md", "to": "requirements/REQ-100.md"})
	if code != http.StatusOK {
		t.Fatalf("move: %d %v", code, out)
	}
	code, treeBody := doJSONList(t, h, cookie, "GET", "/api/repos/r/tree?ref=main")
	joined := strings.Join(treeBody, " ")
	if code != http.StatusOK || !strings.Contains(joined, "REQ-100.md") || strings.Contains(joined, "REQ-001.md") {
		t.Fatalf("tree after move: %d %v", code, treeBody)
	}
	// moving onto an existing path is refused
	write2 := func() { // second file to collide with
		code, _ := doJSON(t, h, cookie, "PUT", "/api/repos/r/files/requirements/REQ-101.md?branch=main",
			map[string]string{"content": "---\ntype: Requirement\n---\n\nx\n", "baseSha": ""})
		if code != http.StatusOK {
			t.Fatalf("seed collide file: %d", code)
		}
	}
	write2()
	code, _ = doJSON(t, h, cookie, "POST", "/api/repos/r/move?branch=main",
		map[string]string{"from": "requirements/REQ-100.md", "to": "requirements/REQ-101.md"})
	if code == http.StatusOK {
		t.Fatal("move onto an existing file was accepted")
	}

	// commit, then history FOLLOWS the rename
	code, _ = doJSON(t, h, cookie, "POST", "/api/repos/r/commit?branch=main", map[string]any{"message": "rename REQ-001 -> REQ-100"})
	if code != http.StatusOK {
		t.Fatalf("commit: %d", code)
	}
	code, hist = doJSONList2(t, h, cookie, "GET", "/api/repos/r/history?path=requirements/REQ-100.md&ref=main", "subject")
	if code != http.StatusOK || len(hist) != 3 {
		t.Fatalf("history after rename should follow: %d %v", code, hist)
	}
	if hist[len(hist)-1] != "first" {
		t.Fatalf("rename history lost the original commits: %v", hist)
	}
}

// doJSONList2 decodes a JSON array and extracts one string field per element.
func doJSONList2(t *testing.T, h http.Handler, cookie *http.Cookie, method, url, field string) (int, []string) {
	t.Helper()
	req := httptest.NewRequest(method, url, nil)
	req.Header.Set("X-SpecQuill", "1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var arr []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &arr)
	out := make([]string, 0, len(arr))
	for _, m := range arr {
		if v, ok := m[field].(string); ok {
			out = append(out, v)
		}
	}
	return rec.Code, out
}
