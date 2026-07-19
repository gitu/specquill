package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/secrets"
	"specquill/server/internal/store"
	"testing/fstest"
)

func testServer(t *testing.T) http.Handler { return testServerWith(t, false) }

// testServerProtected marks main as protected on the writable fixture repo.
func testServerProtected(t *testing.T) http.Handler { return testServerWith(t, true) }

func testServerWith(t *testing.T, protectMain bool) http.Handler {
	h, _, _ := testServerFull(t, protectMain)
	return h
}

// testServerFull also exposes the store + git manager for tenancy tests.
func testServerFull(t *testing.T, protectMain bool) (http.Handler, *store.Store, *gitx.Manager) {
	return testServerCfg(t, protectMain, nil)
}

// testServerCfg additionally lets a test mutate the config before boot
// (auth.default_role, admin_emails, …).
func testServerCfg(t *testing.T, protectMain bool, mut func(*config.Config)) (http.Handler, *store.Store, *gitx.Manager) {
	t.Helper()
	tmp := t.TempDir()
	// minimal fixture repo
	src := filepath.Join(tmp, "src")
	run := func(args ...string) {
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main", src)
	run("-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "init")

	cfg := &config.Config{
		Tenant:  &config.TenantConfig{Slug: "default", DisplayName: "Workspace", DefaultRole: "editor"},
		DataDir: filepath.Join(tmp, "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth:    config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Repos:   []config.RepoConfig{{ID: "w", Mode: config.Writable, Remote: src, DefaultBranch: "main"}},
	}
	if protectMain {
		cfg.Repos[0].ProtectedBranches = []string{"main"}
	}
	if mut != nil {
		mut(cfg)
	}
	st := store.OpenTest(t)
	// serve() mirrors the YAML repos into the default tenant at boot; the
	// tenancy layer auto-enrolls users into it on first request
	if _, err := st.EnsureTenant("default", "config", 0, "Workspace"); err != nil {
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
	box, _ := secrets.NewFromEnv() // nil unless the test sets the master key
	return New(cfg, git, Options{
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		Secrets:  box,
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	}), st, git
}

func TestAPIRequiresAuth(t *testing.T) {
	h := testServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", apiURL("/api/repos"), nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestLocalLoginFlow(t *testing.T) {
	h := testServer(t)

	// wrong password rejected
	body, _ := json.Marshal(map[string]string{"username": "flo", "password": "wrong"})
	req := httptest.NewRequest("POST", "/auth/local/login", bytes.NewReader(body))
	req.Header.Set("X-SpecQuill", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: want 401, got %d", rec.Code)
	}

	// correct login issues a session cookie
	body, _ = json.Marshal(map[string]string{"username": "flo", "password": "hunter2secret"})
	req = httptest.NewRequest("POST", "/auth/local/login", bytes.NewReader(body))
	req.Header.Set("X-SpecQuill", "1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	var session *http.Cookie
	for _, c := range cookies {
		if c.Name == auth.SessionCookie {
			session = c
		}
	}
	if session == nil || !session.HttpOnly {
		t.Fatalf("expected HttpOnly session cookie, got %v", cookies)
	}

	// session grants /api/me with git-author identity
	req = httptest.NewRequest("GET", "/api/me", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me: want 200, got %d", rec.Code)
	}
	var me struct{ Name, Email, Initials string }
	_ = json.Unmarshal(rec.Body.Bytes(), &me)
	if me.Name != "Flo Test" || me.Email != "flo@test.local" || me.Initials != "FT" {
		t.Fatalf("me mismatch: %+v", me)
	}
}

func TestCSRFHeaderRequired(t *testing.T) {
	h := testServer(t)
	req := httptest.NewRequest("POST", "/auth/local/login", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403 without X-SpecQuill header, got %d", rec.Code)
	}
}
