package api

// Forge-PAT mode end to end: login against a mock forge, per-user lazy
// clones, in-repo reference sources bounded by each user's own token, the
// vault-loss 401, the propose (push + MR) flow, and the merge gate.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/forge"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
	"testing/fstest"
)

// mockGitLab serves the endpoints PAT mode touches: identity, project role,
// and merge requests. Tokens in roles{} log in; the level decides the role.
func mockGitLab(t *testing.T, roles map[string]int, mrCreated *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("PRIVATE-TOKEN")
		level, ok := roles[tok]
		if !ok {
			http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v4/user":
			// identity derived from the token so each token is its own user
			fmt.Fprintf(w, `{"id":%d,"username":"user-%s","name":"User %s","email":"%s@test.local","commit_email":""}`,
				100+level, tok, tok, tok)
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodGet:
			fmt.Fprint(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodPost:
			if mrCreated != nil {
				*mrCreated = true
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"iid":3,"title":"proposed","state":"opened","web_url":"https://forge.test/mr/3","author":{"username":"flo"}}`)
		case strings.Contains(r.URL.Path, "/projects/"):
			fmt.Fprintf(w, `{"permissions":{"project_access":{"access_level":%d},"group_access":null}}`, level)
		default:
			t.Errorf("mock forge: unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
}

// dumbGitServer serves a bare repo over git's dumb HTTP protocol, requiring
// HTTP basic auth whose password is in accept{} — the reference-repo stand-in
// that only some tokens may clone.
func dumbGitServer(t *testing.T, dir string, accept map[string]bool) *httptest.Server {
	t.Helper()
	fs := http.StripPrefix("/ref.git/", http.FileServer(http.Dir(dir)))
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pass, ok := r.BasicAuth()
		if !ok || !accept[pass] {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "auth required", http.StatusUnauthorized)
			return
		}
		fs.ServeHTTP(w, r)
	}))
}

func gitOut(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

type patEnv struct {
	h         http.Handler
	st        *store.Store
	dataDir   string
	mainSrc   string // the project's origin (push target)
	mrCreated *bool
}

// patServer boots a forge-PAT-mode server: mock forge, a main project whose
// in-repo config defines a token-gated reference source, and no boot clone.
func patServer(t *testing.T) patEnv {
	t.Helper()
	tmp := t.TempDir()

	// reference source repo, served over authenticated dumb HTTP: tok-a may
	// clone it, tok-b may not
	refSrc := filepath.Join(tmp, "refsrc")
	gitOut(t, "init", "-b", "main", refSrc)
	if err := os.WriteFile(filepath.Join(refSrc, "law.md"), []byte("# Law 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, "-C", refSrc, "add", ".")
	gitOut(t, "-C", refSrc, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "law")
	refBare := filepath.Join(tmp, "ref.git")
	gitOut(t, "clone", "--bare", refSrc, refBare)
	gitOut(t, "-C", refBare, "update-server-info")
	refSrv := dumbGitServer(t, refBare, map[string]bool{"tok-a": true})
	t.Cleanup(refSrv.Close)

	// main project with the in-repo config defining that source
	src := filepath.Join(tmp, "src")
	gitOut(t, "init", "-b", "main", src)
	if err := os.MkdirAll(filepath.Join(src, ".specquill"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgYml := "version: 2\nsources:\n  - name: refsrc\n    remote: " + refSrv.URL + "/ref.git\n" +
		"references:\n  - source: refsrc\n    grounding: true\n"
	for p, c := range map[string]string{
		".specquill/config.yml": cfgYml,
		"index.md":              "# Specs\n",
	} {
		if err := os.WriteFile(filepath.Join(src, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitOut(t, "-C", src, "add", ".")
	gitOut(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "init")

	var mrCreated bool
	// tok-a is a developer (30 → editor); tok-b only a reporter (20 → viewer)
	forgeSrv := mockGitLab(t, map[string]int{"tok-a": 30, "tok-b": 20}, &mrCreated)
	t.Cleanup(forgeSrv.Close)

	cfg := &config.Config{
		DataDir: filepath.Join(tmp, "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth: config.AuthConfig{
			Forge: config.ForgeAuthConfig{Kind: forge.KindGitLab, BaseURL: forgeSrv.URL},
		},
		Projects: []config.ProjectConfig{{
			ID: "w", Remote: src, DefaultBranch: "main",
			// the remote is a local path, so the forge project must be named
			Forge: forge.Config{Project: "acme/specs"},
		}},
	}
	cfg.Normalize() // fills forge kind/base from auth.forge, builds cfg.Repos

	st := store.OpenTest(t)
	if err := st.SyncProjects([]store.Project{{ProjectID: "w", RepoID: "w"}}); err != nil {
		t.Fatal(err)
	}
	git, err := gitx.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := New(cfg, git, Options{
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	return patEnv{h: h, st: st, dataDir: cfg.DataDir, mainSrc: src, mrCreated: &mrCreated}
}

func patLogin(t *testing.T, h http.Handler, token string) (*http.Cookie, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"token": token})
	req := httptest.NewRequest("POST", "/auth/pat/login", bytes.NewReader(body))
	req.Header.Set("X-SpecQuill", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pat login (%s): want 200, got %d: %s", token, rec.Code, rec.Body.String())
	}
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookie {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie issued")
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return session, resp
}

func patDo(h http.Handler, method, path string, session *http.Cookie, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("X-SpecQuill", "1")
	if session != nil {
		req.AddCookie(session)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPatLoginRoleAndMergeMode(t *testing.T) {
	env := patServer(t)
	session, resp := patLogin(t, env.h, "tok-a")
	if resp["role"] != "editor" {
		t.Fatalf("developer access should map to editor, got %v", resp["role"])
	}
	rec := patDo(env.h, "GET", "/api/me", session, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("me: %d %s", rec.Code, rec.Body.String())
	}
	var me struct {
		Provider  string `json:"provider"`
		MergeMode string `json:"mergeMode"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.Provider != "gitlab" || me.MergeMode != "forge" {
		t.Fatalf("me: %+v", me)
	}
}

func TestPatLoginRejectsBadTokenAndNoAccess(t *testing.T) {
	env := patServer(t)
	body, _ := json.Marshal(map[string]string{"token": "tok-unknown"})
	req := httptest.NewRequest("POST", "/auth/pat/login", bytes.NewReader(body))
	req.Header.Set("X-SpecQuill", "1")
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token: want 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPatVaultLossForcesRelogin(t *testing.T) {
	env := patServer(t)
	session, resp := patLogin(t, env.h, "tok-a")

	// a session created outside the login path (= surviving a restart in
	// SQLite while the RAM vault emptied) must 401 so the SPA re-logs-in
	uid := int64(resp["id"].(float64))
	orphan, err := env.st.CreateSession(uid, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rec := patDo(env.h, "GET", "/api/me", &http.Cookie{Name: auth.SessionCookie, Value: orphan}, "")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "token_gone") {
		t.Fatalf("vault-less session: want 401 token_gone, got %d: %s", rec.Code, rec.Body.String())
	}

	// the real session still works
	if rec := patDo(env.h, "GET", "/api/me", session, ""); rec.Code != http.StatusOK {
		t.Fatalf("live session: %d", rec.Code)
	}
}

func TestPatPerUserSourceIsolation(t *testing.T) {
	env := patServer(t)
	sessA, _ := patLogin(t, env.h, "tok-a")
	sessB, _ := patLogin(t, env.h, "tok-b")

	// both users see the project and the in-repo source listed
	for _, sess := range []*http.Cookie{sessA, sessB} {
		rec := patDo(env.h, "GET", "/api/repos", sess, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("repos: %d %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), `"refsrc"`) {
			t.Fatalf("repo list should include the in-repo source: %s", rec.Body.String())
		}
	}

	// A's token can clone the source — browsing works
	rec := patDo(env.h, "GET", "/api/repos/refsrc/tree", sessA, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "law.md") {
		t.Fatalf("user A tree: %d %s", rec.Code, rec.Body.String())
	}

	// B's token cannot — the clone fails and NOTHING of the source lands in
	// B's scope (per-user isolation: A's clone must not serve B)
	rec = patDo(env.h, "GET", "/api/repos/refsrc/tree", sessB, "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "clone_failed") {
		t.Fatalf("user B tree: want 502 clone_failed, got %d: %s", rec.Code, rec.Body.String())
	}
	matches, _ := filepath.Glob(filepath.Join(env.dataDir, "repos", "u*", "refsrc", "git", "HEAD"))
	if len(matches) != 1 {
		t.Fatalf("exactly one user (A) should hold a refsrc clone, found %d: %v", len(matches), matches)
	}
}

func TestPatProposeFlow(t *testing.T) {
	env := patServer(t)
	session, _ := patLogin(t, env.h, "tok-a")

	// claim the workspace branch
	rec := patDo(env.h, "POST", "/api/repos/w/workspace", session, "{}")
	if rec.Code != http.StatusOK {
		t.Fatalf("workspace: %d %s", rec.Code, rec.Body.String())
	}
	var ws struct{ Branch string }
	_ = json.Unmarshal(rec.Body.Bytes(), &ws)
	if !strings.HasPrefix(ws.Branch, "ws/") {
		t.Fatalf("workspace branch: %q", ws.Branch)
	}

	// edit + commit on it
	rec = patDo(env.h, "PUT", "/api/repos/w/files/notes.md?branch="+ws.Branch, session,
		`{"content":"# Notes\n","baseSha":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}

	// dirty worktree → propose refuses with the commit-first contract
	rec = patDo(env.h, "POST", "/api/repos/w/propose", session, `{"source":"`+ws.Branch+`"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"dirty"`) {
		t.Fatalf("dirty propose: want 409 dirty, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = patDo(env.h, "POST", "/api/repos/w/commit?branch="+ws.Branch, session, `{"message":"add notes"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body.String())
	}

	// propose: pushes the branch to origin and opens the MR on the forge
	rec = patDo(env.h, "POST", "/api/repos/w/propose", session, `{"source":"`+ws.Branch+`","title":"Notes"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("propose: %d %s", rec.Code, rec.Body.String())
	}
	var prop struct {
		Number  int
		URL     string
		Created bool
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &prop)
	if prop.Number != 3 || !prop.Created || prop.URL == "" {
		t.Fatalf("propose result: %+v", prop)
	}
	if !*env.mrCreated {
		t.Fatal("forge never saw the MR creation")
	}
	// the branch really reached the origin
	if out := gitOut(t, "-C", env.mainSrc, "show-ref", ws.Branch); !strings.Contains(out, "refs/heads/"+ws.Branch) {
		t.Fatalf("branch not pushed to origin: %s", out)
	}

	// the in-app merge is off in this mode
	rec = patDo(env.h, "POST", "/api/repos/w/merge", session, `{"source":"`+ws.Branch+`"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "merge_via_forge") {
		t.Fatalf("merge gate: want 403 merge_via_forge, got %d: %s", rec.Code, rec.Body.String())
	}
}
