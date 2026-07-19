package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"testing/fstest"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

// fakeGitHub stands in for github.com (authorize/token) + api.github.com.
func fakeGitHub(t *testing.T, login string, id int64, email string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") != "good-code" {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "gho_test"})
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gho_test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "login": login, "name": "Test User", "email": ""})
	})
	mux.HandleFunc("GET /user/emails", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": email, "primary": true, "verified": true},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func githubTestServer(t *testing.T, gh *httptest.Server, allowed, adminEmails []string) http.Handler {
	t.Helper()
	tmp := t.TempDir()
	src := tmp + "/src"
	gitRun(t, "init", "-b", "main", src)
	if err := os.WriteFile(src+"/index.md", []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "add", "-A")
	gitRun(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "init")

	t.Setenv("TEST_GH_SECRET", "shhh")
	cfg := &config.Config{
		DataDir: tmp + "/data",
		BaseURL: "http://app.test",
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: 3600e9, CookieSecure: false},
		Auth: config.AuthConfig{
			GitHub: config.GitHubAuthConfig{
				Enabled: true, ClientID: "cid", ClientSecretEnv: "TEST_GH_SECRET",
				AllowedUsers: allowed, WebBase: gh.URL, APIBase: gh.URL,
			},
			AdminEmails: adminEmails,
		},
		Repos: []config.RepoConfig{{ID: "w", Mode: config.Writable, Remote: src, DefaultBranch: "main"}},
	}
	st := store.OpenTest(t)
	ten, err := st.EnsureTenant(gitx.DefaultTenant, "config", 0, "Workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SyncTenantProjects(ten.ID, []store.Project{{ProjectID: "w", RepoID: "w"}}); err != nil {
		t.Fatal(err)
	}
	git, err := gitx.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.Init(); err != nil {
		t.Fatal(err)
	}
	return New(cfg, git, Options{
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		GitHub:   auth.NewGitHub(cfg),
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
}

// githubLogin walks the whole flow: /auth/github/login (state cookie) →
// callback with the code → session cookie. Returns the final redirect
// location and the session cookie (nil when login was refused).
func githubLogin(t *testing.T, h http.Handler) (string, *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/auth/github/login", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("login redirect: %d", rec.Code)
	}
	var state *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "specquill_oauth_gh" {
			state = c
		}
	}
	if state == nil {
		t.Fatal("no oauth state cookie")
	}
	cb := httptest.NewRequest("GET", fmt.Sprintf("/auth/github/callback?code=good-code&state=%s", state.Value), nil)
	cb.AddCookie(state)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, cb)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback: %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "specquill_session" && c.Value != "" {
			return rec.Header().Get("Location"), c
		}
	}
	return rec.Header().Get("Location"), nil
}

func TestGitHubLoginFlow(t *testing.T) {
	gh := fakeGitHub(t, "flo", 4242, "flo@example.com")
	h := githubTestServer(t, gh, []string{"Flo"}, []string{"FLO@example.com"})

	loc, session := githubLogin(t, h)
	if session == nil || loc != "/" {
		t.Fatalf("expected session + redirect to /, got loc=%s session=%v", loc, session)
	}

	// the session works, identity is the GitHub account, email resolved via
	// /user/emails, and the admin_emails bootstrap granted the admin role
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var me struct {
		Email    string `json:"email"`
		Provider string `json:"provider"`
		Tenants  []struct {
			Role string `json:"role"`
		} `json:"tenants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("/api/me: %d %s", rec.Code, rec.Body.String())
	}
	if me.Provider != "github" || me.Email != "flo@example.com" {
		t.Fatalf("wrong identity: %+v", me)
	}
	if len(me.Tenants) != 1 || me.Tenants[0].Role != "admin" {
		t.Fatalf("admin bootstrap failed: %+v", me.Tenants)
	}
}

func TestGitHubLoginAllowlist(t *testing.T) {
	gh := fakeGitHub(t, "stranger", 777, "stranger@example.com")
	h := githubTestServer(t, gh, []string{"flo"}, nil)

	loc, session := githubLogin(t, h)
	if session != nil {
		t.Fatal("disallowed login minted a session")
	}
	if loc != "/#/login?error=forbidden" {
		t.Fatalf("expected forbidden redirect, got %s", loc)
	}
}

func TestGitHubLoginMemberRole(t *testing.T) {
	gh := fakeGitHub(t, "flo", 4242, "flo@example.com")
	h := githubTestServer(t, gh, nil, nil) // empty allowlist admits; no admins

	_, session := githubLogin(t, h)
	if session == nil {
		t.Fatal("login failed")
	}
	req := httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var me struct {
		Tenants []struct{ Role string } `json:"tenants"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if len(me.Tenants) != 1 || me.Tenants[0].Role != "editor" {
		t.Fatalf("expected plain editor enrollment, got %+v", me.Tenants)
	}
}
