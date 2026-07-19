package api

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/githubapp"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

// ghAppFixture fakes github.com end to end for the App flow: OAuth login,
// installation token minting, installation repo listing, and per-user
// collaborator permissions (mutable per test).
type ghAppFixture struct {
	permissions map[string]string // "<login>" → permission on every repo
	repos       []map[string]any
	srv         *httptest.Server
}

func newGHAppFixture(t *testing.T, cloneURL string) *ghAppFixture {
	t.Helper()
	f := &ghAppFixture{permissions: map[string]string{}}
	f.repos = []map[string]any{{
		"full_name": "acme/specs", "private": true, "default_branch": "main", "clone_url": cloneURL,
	}}
	mux := http.NewServeMux()
	// --- OAuth login (user identity) ---
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "gho_test"})
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 4242, "login": "flo", "name": "Flo", "email": "flo@example.com"})
	})
	// --- App API ---
	mux.HandleFunc("POST /app/installations/7/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_inst", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
	})
	mux.HandleFunc("GET /installation/repositories", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(f.repos), "repositories": f.repos})
	})
	mux.HandleFunc("GET /repos/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		login := parts[len(parts)-2] // .../collaborators/{login}/permission
		perm, ok := f.permissions[login]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"permission": perm})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func ghAppTestServer(t *testing.T) (http.Handler, *ghAppFixture, string) {
	h, fx, work, _, _ := ghAppTestServerFull(t)
	return h, fx, work
}

// ghAppTestServerFull also exposes the store and git manager (grant tests
// adopt repos directly instead of through the admin-gated picker).
func ghAppTestServerFull(t *testing.T) (http.Handler, *ghAppFixture, string, *store.Store, *gitx.Manager) {
	t.Helper()
	tmp := t.TempDir()

	// local origin standing in for github.com/acme/specs
	origin := filepath.Join(tmp, "origin.git")
	work := filepath.Join(tmp, "work")
	gitRun(t, "init", "-b", "main", work)
	if err := os.WriteFile(filepath.Join(work, "index.md"), []byte("# acme specs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", work, "-c", "user.name=t", "-c", "user.email=t@t", "add", "-A")
	gitRun(t, "-C", work, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "init")
	gitRun(t, "init", "--bare", "-b", "main", origin)
	gitRun(t, "-C", work, "push", "-q", origin, "main")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+origin+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/acme/specs.git")

	fx := newGHAppFixture(t, "https://github.com/acme/specs.git")

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_APP_KEY", string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})))
	t.Setenv("TEST_APP_HOOK_SECRET", "app-hook-secret")
	t.Setenv("TEST_GH_SECRET", "oauth-secret")

	cfg := &config.Config{
		Tenant:  &config.TenantConfig{Slug: "default", DisplayName: "Workspace", DefaultRole: "editor"},
		DataDir: filepath.Join(tmp, "data"),
		BaseURL: "http://app.test",
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: time.Hour},
		Auth: config.AuthConfig{
			GitHub: config.GitHubAuthConfig{
				Enabled: true, ClientID: "cid", ClientSecretEnv: "TEST_GH_SECRET",
				WebBase: fx.srv.URL, APIBase: fx.srv.URL,
			},
		},
		GitHubApp: config.GitHubAppConfig{
			AppID: 42, PrivateKeyEnv: "TEST_APP_KEY", WebhookSecretEnv: "TEST_APP_HOOK_SECRET",
			APIBase: fx.srv.URL,
		},
	}
	st := store.OpenTest(t)
	git, err := gitx.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := git.Init(); err != nil {
		t.Fatal(err)
	}
	app, err := githubapp.New(cfg.GitHubApp)
	if err != nil {
		t.Fatal(err)
	}
	git.TokenFor = func(r *gitx.Repo) (string, string, bool) {
		ten, err := st.TenantBySlug(r.Tenant())
		if err != nil || ten.Provider != "github" {
			return "", "", false
		}
		tok, err := app.InstallationToken(ten.Installation)
		if err != nil {
			return "", "", false
		}
		return "x-access-token", tok, true
	}
	h := New(cfg, git, Options{
		Store:     st,
		Sessions:  auth.NewSessions(st, cfg),
		GitHub:    auth.NewGitHub(cfg),
		GitHubApp: app,
		Dist:      fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	return h, fx, work, st, git
}

func tenantReq(t *testing.T, h http.Handler, cookie *http.Cookie, method, url, tenant string, body any) (int, string) {
	t.Helper()
	var payload strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		payload = *strings.NewReader(string(b))
	}
	if bare, _, _ := strings.Cut(url, "?"); bare != "/api/me" && strings.HasPrefix(url, "/api/") {
		url = "/api/t/" + tenant + url[len("/api"):]
	}
	req := httptest.NewRequest(method, url, &payload)
	req.Header.Set("X-SpecQuill", "1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestGitHubAppTenantLifecycle(t *testing.T) {
	h, fx, work := ghAppTestServer(t)

	// 1. installation webhook creates the tenant
	rec := signedHook(t, h, "installation", map[string]any{
		"action":       "created",
		"installation": map[string]any{"id": 7, "account": map[string]any{"login": "Acme"}},
	}, "app-hook-secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("installation hook: %d %s", rec.Code, rec.Body.String())
	}

	// 2. an org admin logs in with GitHub → role sync bootstraps them as
	// tenant admin from the installation candidates
	fx.permissions["flo"] = "admin"
	_, cookie := githubLogin(t, h)
	if cookie == nil {
		t.Fatal("github login failed")
	}
	code, body := tenantReq(t, h, cookie, "GET", "/api/me", "acme", nil)
	if code != http.StatusOK || !strings.Contains(body, `"role":"admin"`) || !strings.Contains(body, `"slug":"acme"`) {
		t.Fatalf("admin bootstrap: %d %s", code, body)
	}

	// 3. the repo picker adopts acme/specs as a workspace (clone runs
	// through the installation-token TokenFor via the insteadOf mapping)
	code, body = tenantReq(t, h, cookie, "GET", "/api/github/repos", "acme", nil)
	if code != http.StatusOK || !strings.Contains(body, "acme/specs") {
		t.Fatalf("candidates: %d %s", code, body)
	}
	code, body = tenantReq(t, h, cookie, "POST", "/api/github/repos", "acme", map[string]string{"fullName": "acme/specs", "mode": "workspace"})
	if code != http.StatusOK {
		t.Fatalf("adopt: %d %s", code, body)
	}

	// 4. the workspace serves tenant-scoped content
	code, body = tenantReq(t, h, cookie, "GET", "/api/repos/specs/files/index.md?ref=main", "acme", nil)
	if code != http.StatusOK || !strings.Contains(body, "# acme specs") {
		t.Fatalf("tenant repo read: %d %s", code, body)
	}

	// 5. a push webhook fast-forwards it (external commit)
	if err := os.WriteFile(filepath.Join(work, "index.md"), []byte("# acme specs v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", work, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-am", "v2")
	gitRun(t, "-C", work, "push", "-q", filepath.Join(filepath.Dir(work), "origin.git"), "main")
	rec = signedHook(t, h, "push", map[string]any{
		"ref": "refs/heads/main", "repository": map[string]any{"full_name": "acme/specs"},
	}, "app-hook-secret")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"matched":1`) {
		t.Fatalf("push hook: %d %s", rec.Code, rec.Body.String())
	}
	code, body = tenantReq(t, h, cookie, "GET", "/api/repos/specs/files/index.md?ref=main", "acme", nil)
	if !strings.Contains(body, "v2") {
		t.Fatalf("push not applied: %d %s", code, body)
	}

	// 6. uninstall revokes access immediately
	rec = signedHook(t, h, "installation", map[string]any{
		"action":       "deleted",
		"installation": map[string]any{"id": 7, "account": map[string]any{"login": "Acme"}},
	}, "app-hook-secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("uninstall hook: %d %s", rec.Code, rec.Body.String())
	}
	code, body = tenantReq(t, h, cookie, "GET", "/api/repos/specs/files/index.md?ref=main", "acme", nil)
	if code == http.StatusOK {
		t.Fatalf("access survived uninstall: %d %s", code, body)
	}
}

func TestGitHubAppRoleMapping(t *testing.T) {
	h, fx, _ := ghAppTestServer(t)
	rec := signedHook(t, h, "installation", map[string]any{
		"action":       "created",
		"installation": map[string]any{"id": 7, "account": map[string]any{"login": "acme"}},
	}, "app-hook-secret")
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	// write → editor
	fx.permissions["flo"] = "write"
	_, cookie := githubLogin(t, h)
	code, body := tenantReq(t, h, cookie, "GET", "/api/me", "acme", nil)
	if code != http.StatusOK || !strings.Contains(body, `"role":"editor"`) {
		t.Fatalf("write → editor: %d %s", code, body)
	}

	// read → viewer, on a fresh server (the role TTL cache pins per instance)
	h2, fx2, _ := ghAppTestServer(t)
	rec = signedHook(t, h2, "installation", map[string]any{
		"action":       "created",
		"installation": map[string]any{"id": 7, "account": map[string]any{"login": "acme"}},
	}, "app-hook-secret")
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	fx2.permissions["flo"] = "read"
	_, cookie2 := githubLogin(t, h2)
	code, body = tenantReq(t, h2, cookie2, "GET", "/api/me", "acme", nil)
	if code != http.StatusOK || !strings.Contains(body, `"role":"viewer"`) {
		t.Fatalf("read → viewer: %d %s", code, body)
	}

	// maintain → maintainer (REQ-021.3), again on a fresh instance
	h3, fx3, _ := ghAppTestServer(t)
	rec = signedHook(t, h3, "installation", map[string]any{
		"action":       "created",
		"installation": map[string]any{"id": 7, "account": map[string]any{"login": "acme"}},
	}, "app-hook-secret")
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	fx3.permissions["flo"] = "maintain"
	_, cookie3 := githubLogin(t, h3)
	code, body = tenantReq(t, h3, cookie3, "GET", "/api/me", "acme", nil)
	if code != http.StatusOK || !strings.Contains(body, `"role":"maintainer"`) {
		t.Fatalf("maintain → maintainer: %d %s", code, body)
	}
}
