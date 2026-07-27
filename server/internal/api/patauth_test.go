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
	"specquill/server/internal/project"
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
			// main carries a token-marked MR (per-user cache assertions);
			// workspace branches have none (propose creates one)
			if r.URL.Query().Get("source_branch") == "main" {
				fmt.Fprintf(w, `[{"iid":9,"title":"mr-for-%s","state":"opened","web_url":"u","author":{"username":"x"}}]`, tok)
			} else {
				fmt.Fprint(w, `[]`)
			}
		case strings.Contains(r.URL.Path, "/merge_requests/9/notes"):
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
func patServer(t *testing.T) patEnv { return patServerSources(t, "") }

// patServerSources additionally appends raw `sources:` entries (yaml lines)
// to the fixture's in-repo config.
func patServerSources(t *testing.T, extraSources string) patEnv {
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
	// the evil entry is the attack REQ-004.6 forbids: a filesystem remote in
	// user-writable repo content — it must never register
	cfgYml := "version: 2\nsources:\n  - name: refsrc\n    remote: " + refSrv.URL + "/ref.git\n" +
		"  - name: evil\n    remote: " + refBare + "\n" +
		extraSources +
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
	// tok-a is a developer (30 → editor); tok-b only a reporter (20 → viewer);
	// tok-none authenticates but has no access to the project at all
	forgeSrv := mockGitLab(t, map[string]int{"tok-a": 30, "tok-b": 20, "tok-none": 0}, &mrCreated)
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

	// a token the forge does not know: 401
	body, _ := json.Marshal(map[string]string{"token": "tok-unknown"})
	req := httptest.NewRequest("POST", "/auth/pat/login", bytes.NewReader(body))
	req.Header.Set("X-SpecQuill", "1")
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token: want 401, got %d: %s", rec.Code, rec.Body.String())
	}

	// a valid token without access to the main project: 403 (REQ-024.2)
	body, _ = json.Marshal(map[string]string{"token": "tok-none"})
	req = httptest.NewRequest("POST", "/auth/pat/login", bytes.NewReader(body))
	req.Header.Set("X-SpecQuill", "1")
	rec = httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "no_project_access") {
		t.Fatalf("no-access token: want 403 no_project_access, got %d: %s", rec.Code, rec.Body.String())
	}
}

// /auth/providers must carry what the login page needs to guide token
// creation: kind, scopes, and the prefilled deep link (REQ-024.6).
func TestPatProvidersPayload(t *testing.T) {
	env := patServer(t)
	rec := patDo(env.h, "GET", "/auth/providers", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("providers: %d", rec.Code)
	}
	var resp struct {
		Local bool `json:"local"`
		Forge *struct {
			Kind           string   `json:"kind"`
			TokenCreateURL string   `json:"tokenCreateUrl"`
			Scopes         []string `json:"scopes"`
		} `json:"forge"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Forge == nil || resp.Forge.Kind != "gitlab" || len(resp.Forge.Scopes) != 1 || resp.Forge.Scopes[0] != "api" {
		t.Fatalf("forge provider: %+v", resp.Forge)
	}
	if !strings.Contains(resp.Forge.TokenCreateURL, "personal_access_tokens") {
		t.Fatalf("token link: %q", resp.Forge.TokenCreateURL)
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

func TestPatInRepoSourceRemoteMustBeHTTP(t *testing.T) {
	env := patServer(t)
	session, _ := patLogin(t, env.h, "tok-a")

	// the config.yml defines "evil" with a filesystem remote — it must not
	// surface in the repo list and must not resolve
	rec := patDo(env.h, "GET", "/api/repos", session, "")
	if strings.Contains(rec.Body.String(), `"evil"`) {
		t.Fatalf("path-remote source must not be listed: %s", rec.Body.String())
	}
	rec = patDo(env.h, "GET", "/api/repos/evil/tree", session, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("path-remote source: want 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPatProjectsListInRepoReferences(t *testing.T) {
	env := patServer(t)
	session, _ := patLogin(t, env.h, "tok-a")
	rec := patDo(env.h, "GET", "/api/projects", session, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("projects: %d %s", rec.Code, rec.Body.String())
	}
	var infos []struct {
		ID         string `json:"id"`
		References []struct {
			Source    string `json:"source"`
			Kind      string `json:"kind"`
			Grounding bool   `json:"grounding"`
		} `json:"references"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &infos); err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || len(infos[0].References) != 1 {
		t.Fatalf("projects: %+v", infos)
	}
	ref := infos[0].References[0]
	if ref.Source != "refsrc" || ref.Kind != "git" || !ref.Grounding {
		t.Fatalf("reference: %+v", ref)
	}
}

// The forge review cache must be per-user: with per-user tokens, a cached
// entry keyed only by repo+branch would show one user the MR view of another.
func TestPatForgeRequestCachePerUser(t *testing.T) {
	env := patServer(t)
	sessA, _ := patLogin(t, env.h, "tok-a")
	sessB, _ := patLogin(t, env.h, "tok-b")

	recA := patDo(env.h, "GET", "/api/repos/w/forge/request?branch=main", sessA, "")
	if recA.Code != http.StatusOK || !strings.Contains(recA.Body.String(), "mr-for-tok-a") {
		t.Fatalf("user A forge view: %d %s", recA.Code, recA.Body.String())
	}
	// B asks within A's cache TTL — and must still see B's own answer
	recB := patDo(env.h, "GET", "/api/repos/w/forge/request?branch=main", sessB, "")
	if recB.Code != http.StatusOK || !strings.Contains(recB.Body.String(), "mr-for-tok-b") {
		t.Fatalf("user B forge view leaked A's cache: %d %s", recB.Code, recB.Body.String())
	}
}

func TestScopeWarning(t *testing.T) {
	cases := []struct {
		wanted, got []string
		want        string
	}{
		{[]string{"repo"}, nil, ""},                          // forge disclosed nothing: stay quiet
		{[]string{"repo"}, []string{"repo", "read:org"}, ""}, // covered
		{[]string{"api"}, []string{"read_api"}, "missing the api scope"},
	}
	for i, c := range cases {
		w := scopeWarning(c.wanted, c.got)
		if (c.want == "" && w != "") || (c.want != "" && !strings.Contains(w, c.want)) {
			t.Fatalf("case %d: got %q, want %q", i, w, c.want)
		}
	}
}

// A source remote on the deployment's own host that REDIRECTS elsewhere must
// not leak the token: the credential helper is scoped to the remote's exact
// host:port, so the redirect target gets a credential-less request.
func TestPatRedirectNeverLeaksToken(t *testing.T) {
	var leaked []string
	catcher := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a := r.Header.Get("Authorization"); a != "" {
			leaked = append(leaked, a)
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		http.Error(w, "auth required", http.StatusUnauthorized)
	}))
	defer catcher.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, catcher.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer redirector.Close()

	env := patServerSources(t, "  - name: sneaky\n    remote: "+redirector.URL+"/ref.git\n")
	session, _ := patLogin(t, env.h, "tok-a")

	// both servers are 127.0.0.1 (allowed host), so the source registers and
	// the clone is attempted — and must fail without surrendering the token
	rec := patDo(env.h, "GET", "/api/repos/sneaky/tree", session, "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("redirected clone: want 502, got %d: %s", rec.Code, rec.Body.String())
	}
	if len(leaked) != 0 {
		t.Fatalf("redirect target received credentials: %v", leaked)
	}
}

// A source remote naming a host outside the allowlist never registers, and
// /api/projects explains why.
func TestPatForeignHostSourceRejected(t *testing.T) {
	env := patServerSources(t, "  - name: offsite\n    remote: https://evil.example.com/x.git\n")
	session, _ := patLogin(t, env.h, "tok-a")

	rec := patDo(env.h, "GET", "/api/repos", session, "")
	if strings.Contains(rec.Body.String(), `"offsite"`) {
		t.Fatalf("foreign-host source must not be listed: %s", rec.Body.String())
	}
	rec = patDo(env.h, "GET", "/api/repos/offsite/tree", session, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign-host source: want 404, got %d", rec.Code)
	}
	rec = patDo(env.h, "GET", "/api/projects", session, "")
	if !strings.Contains(rec.Body.String(), "evil.example.com is not allowed") {
		t.Fatalf("rejection should surface as a project warning: %s", rec.Body.String())
	}
}

func TestSourceDefValidation(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{Forge: config.ForgeAuthConfig{
			Kind: forge.KindGitLab, BaseURL: "https://gitlab.example.com",
			AllowedSourceHosts: []string{"mirror.example.org"},
		}},
		Projects: []config.ProjectConfig{{ID: "w", Remote: "https://gitlab.example.com/acme/specs.git"}},
	}
	srv := &Server{cfg: cfg}
	cases := []struct {
		name, remote, want string
	}{
		{"ok", "https://gitlab.example.com/acme/reg.git", ""},
		{"mirror", "https://mirror.example.org/reg.git", ""}, // via allowed_source_hosts
		{"Bad Name", "https://gitlab.example.com/x.git", "name"},
		{"creds", "https://user:pass@gitlab.example.com/x.git", "credentials"},
		{"scheme", "ssh://git@gitlab.example.com/x.git", "http(s)"},
		{"path", "/srv/git/x.git", "http(s)"},
		{"offsite", "https://evil.example.com/x.git", "not allowed"},
		{"subdomain", "https://gitlab.example.com.evil.io/x.git", "not allowed"},
	}
	for _, c := range cases {
		err := srv.sourceDefError(project.SourceDef{Name: c.name, Remote: c.remote})
		if c.want == "" && err != "" {
			t.Errorf("%s: unexpected error %q", c.name, err)
		}
		if c.want != "" && !strings.Contains(err, c.want) {
			t.Errorf("%s: got %q, want containing %q", c.name, err, c.want)
		}
	}
}
