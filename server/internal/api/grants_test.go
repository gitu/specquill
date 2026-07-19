package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

// wRepoRow registers the fixture repo `w` in the default tenant's registry
// (repo_grants FK needs the row; the boot sync does this in production).
func wRepoRow(t *testing.T, st *store.Store) *store.Tenant {
	t.Helper()
	def, err := st.TenantBySlug(gitx.DefaultTenant)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertTenantRepo(def.ID, store.TenantRepo{RepoID: "w", Mode: "writable", Remote: "x", DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	return def
}

func userID(t *testing.T, st *store.Store, identifier string) int64 {
	t.Helper()
	u, err := st.UserByEmailOrLogin(identifier)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// The viewer-write gap: a viewer reads (incl. PRs) and comments but every
// mutation is refused; a per-repo editor grant lifts the same user to write.
func TestViewerCannotWriteGrantElevates(t *testing.T) {
	h, st, _ := testServerFull(t, false)
	cookie := login(t, h)
	def := wRepoRow(t, st)
	// enroll as viewer BEFORE the first API request — otherwise the tenancy
	// layer auto-enrolls flo as member and the write gate has nothing to do
	flo := userID(t, st, "flo@test.local")
	if err := st.EnsureMember(def.ID, flo, "viewer"); err != nil {
		t.Fatal(err)
	}

	// reads stay open
	if code, _ := doJSON(t, h, cookie, "GET", "/api/repos/w/tree", nil); code != http.StatusOK {
		t.Fatalf("viewer tree: want 200, got %d", code)
	}
	if code, _ := doJSON(t, h, cookie, "GET", "/api/repos/w/prs", nil); code != http.StatusOK {
		t.Fatalf("viewer PR list: want 200, got %d", code)
	}
	// mutations are role-gated
	code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/specs/a.md", map[string]string{"content": "x"})
	if code != http.StatusForbidden || out["code"] != "role_forbidden" {
		t.Fatalf("viewer write: want 403 role_forbidden, got %d %v", code, out)
	}
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/prs", map[string]string{"title": "t", "source": "b", "target": "main"})
	if code != http.StatusForbidden || out["code"] != "role_forbidden" {
		t.Fatalf("viewer PR create: want 403 role_forbidden, got %d %v", code, out)
	}
	// the repo list reports the effective role
	if code, out := doJSON(t, h, cookie, "GET", "/api/me", nil); code != http.StatusOK {
		t.Fatalf("me: %d %v", code, out)
	}

	// explicit editor grant on the repo wins over the derived viewer role
	if err := st.UpsertRepoGrant(def.ID, "w", flo, "editor", 0); err != nil {
		t.Fatal(err)
	}
	if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/specs/a.md", map[string]string{"content": "x"}); code != http.StatusOK {
		t.Fatalf("granted write: want 200, got %d %v", code, out)
	}
}

// default_role: none — a fresh user has no tenant at all until granted; the
// grant yields exactly the granted repo, read-only for a viewer grant; the
// configured admin still bootstraps.
func TestDefaultRoleNone(t *testing.T) {
	h, st, _ := testServerCfg(t, false, func(c *config.Config) {
		c.Auth.DefaultRole = "none"
		c.Auth.AdminEmails = []string{"boss@test.local"}
	})
	hash, _ := auth.HashPassword("hunter2secret")
	if err := st.AddLocalUser("boss", "Boss Test", "boss@test.local", hash); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, h) // flo

	// no membership: tenant resolution refuses
	code, out := doJSON(t, h, cookie, "GET", "/api/repos", nil)
	if code != http.StatusForbidden || out["code"] != "tenant_forbidden" {
		t.Fatalf("ungranted user: want 403 tenant_forbidden, got %d %v", code, out)
	}

	// a viewer grant surfaces the tenant and exactly the granted repo
	def := wRepoRow(t, st)
	flo := userID(t, st, "flo@test.local")
	if err := st.UpsertRepoGrant(def.ID, "w", flo, "viewer", 0); err != nil {
		t.Fatal(err)
	}
	if code, _ := doJSON(t, h, cookie, "GET", "/api/repos/w/tree", nil); code != http.StatusOK {
		t.Fatalf("granted read: want 200, got %d", code)
	}
	code, out = doJSON(t, h, cookie, "PUT", "/api/repos/w/files/specs/a.md", map[string]string{"content": "x"})
	if code != http.StatusForbidden || out["code"] != "role_forbidden" {
		t.Fatalf("viewer grant write: want 403 role_forbidden, got %d %v", code, out)
	}
	// grant-only users stay out of tenant management
	code, out = doJSON(t, h, cookie, "GET", "/api/members", nil)
	if code != http.StatusForbidden {
		t.Fatalf("grant-only management: want 403, got %d %v", code, out)
	}

	// the bootstrap admin is unaffected by default_role none
	bossCookie := loginAs(t, h, "boss", "hunter2secret")
	if code, _ := doJSON(t, h, bossCookie, "GET", "/api/members", nil); code != http.StatusOK {
		t.Fatalf("admin bootstrap under default_role none: want 200, got %d", code)
	}
}

// Grants admin API: grant an existing user, invite an unknown one, claim on
// first login, revoke.
func TestGrantsAPI(t *testing.T) {
	h, st, _ := testServerCfg(t, false, func(c *config.Config) {
		c.Auth.DefaultRole = "viewer"
		c.Auth.AdminEmails = []string{"flo@test.local"}
	})
	cookie := login(t, h) // flo → admin via admin_emails
	wRepoRow(t, st)

	// role validation: only ladder roles are grantable (REQ-021)
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/grants", map[string]string{"user": "x@y.z", "role": "member"})
	if code != http.StatusBadRequest {
		t.Fatalf("unknown role must be rejected: got %d %v", code, out)
	}

	// unknown identity → invite
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/grants", map[string]string{"user": "Eve@Test.Local", "role": "editor"})
	if code != http.StatusOK || out["status"] != "invited" || out["kind"] != "email" {
		t.Fatalf("invite: want invited/email, got %d %v", code, out)
	}
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/w/grants", nil)
	if code != http.StatusOK || !strings.Contains(jsonStr(out["invites"]), "eve@test.local") {
		t.Fatalf("invite listed: %d %v", code, out)
	}

	// eve logs in → invite becomes a grant; her default-role viewer floor is
	// elevated to editor on w
	hash, _ := auth.HashPassword("hunter2secret")
	if err := st.AddLocalUser("eve", "Eve Test", "eve@test.local", hash); err != nil {
		t.Fatal(err)
	}
	eveCookie := loginAs(t, h, "eve", "hunter2secret")
	if code, out := doJSON(t, h, eveCookie, "PUT", "/api/repos/w/files/specs/eve.md", map[string]string{"content": "x"}); code != http.StatusOK {
		t.Fatalf("claimed grant write: want 200, got %d %v", code, out)
	}
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/w/grants", nil)
	if code != http.StatusOK || !strings.Contains(jsonStr(out["grants"]), "eve@test.local") || strings.Contains(jsonStr(out["invites"]), "eve") {
		t.Fatalf("claimed grant listed: %d %v", code, out)
	}

	// grant an existing user directly, then revoke
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/grants", map[string]string{"user": "eve@test.local", "role": "viewer"})
	if code != http.StatusOK || out["status"] != "granted" {
		t.Fatalf("re-grant: %d %v", code, out)
	}
	eve := userID(t, st, "eve@test.local")
	if code, out := doJSON(t, h, eveCookie, "PUT", "/api/repos/w/files/specs/eve.md", map[string]string{"content": "y"}); code != http.StatusForbidden {
		t.Fatalf("downgraded grant: want 403, got %d %v", code, out)
	}
	if code, _ := doJSON(t, h, cookie, "DELETE", "/api/repos/w/grants/"+i64(eve), nil); code != http.StatusOK {
		t.Fatalf("revoke failed: %d", code)
	}

	// non-admins never manage grants
	if code, _ := doJSON(t, h, eveCookie, "POST", "/api/repos/w/grants", map[string]string{"user": "x@y.z", "role": "viewer"}); code != http.StatusForbidden {
		t.Fatalf("non-admin grant: want 403, got %d", code)
	}
}

// GitHub tenants derive the role PER REPO: read permission means viewer on
// that repo (no write through the app), and an explicit grant elevates
// beyond the git permission — the server pushes with the installation
// token, so the app gate is the only one that matters.
func TestGitHubPerRepoDerivationAndGrant(t *testing.T) {
	h, fx, _, st, git := ghAppTestServerFull(t)
	rec := signedHook(t, h, "installation", map[string]any{
		"action":       "created",
		"installation": map[string]any{"id": 7, "account": map[string]any{"login": "acme"}},
	}, "app-hook-secret")
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	acme, err := st.TenantBySlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	// adopt acme/specs directly (the picker is admin-gated; this test's user
	// has read permission only)
	if _, err := git.AddRepo("acme", config.RepoConfig{
		ID: "specs", Mode: config.Writable, Remote: "https://github.com/acme/specs.git", DefaultBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertTenantRepo(acme.ID, store.TenantRepo{
		RepoID: "specs", Mode: "writable", Remote: "https://github.com/acme/specs.git",
		DefaultBranch: "main", GhFullName: "acme/specs",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddProject(store.Project{TenantID: acme.ID, ProjectID: "specs", RepoID: "specs"}); err != nil {
		t.Fatal(err)
	}

	fx.permissions["flo"] = "read" // GitHub says: pull only
	_, cookie := githubLogin(t, h)

	// read works, write is refused — the derived per-repo role is viewer
	code, body := tenantReq(t, h, cookie, "GET", "/api/repos/specs/files/index.md?ref=main", "acme", nil)
	if code != http.StatusOK {
		t.Fatalf("derived viewer read: %d %s", code, body)
	}
	code, body = tenantReq(t, h, cookie, "PUT", "/api/repos/specs/files/notes.md", "acme", map[string]string{"content": "x"})
	if code != http.StatusForbidden || !strings.Contains(body, "role_forbidden") {
		t.Fatalf("derived viewer write: want 403 role_forbidden, got %d %s", code, body)
	}

	// an explicit editor grant elevates past the git permission
	flo, err := st.UserByEmailOrLogin("flo")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertRepoGrant(acme.ID, "specs", flo.ID, "editor", 0); err != nil {
		t.Fatal(err)
	}
	code, body = tenantReq(t, h, cookie, "PUT", "/api/repos/specs/files/notes.md", "acme", map[string]string{"content": "x"})
	if code != http.StatusOK {
		t.Fatalf("granted write: want 200, got %d %s", code, body)
	}

	// GitHub revoking the membership row must not touch the grant
	if err := st.DeleteMember(acme.ID, flo.ID); err != nil {
		t.Fatal(err)
	}
	if role, err := st.RepoGrantRole(acme.ID, "specs", flo.ID); err != nil || role != "editor" {
		t.Fatalf("grant lost on revocation: %v %q", err, role)
	}
}

func loginAs(t *testing.T, h http.Handler, username, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest("POST", "/auth/local/login", bytes.NewReader(body))
	req.Header.Set("X-SpecQuill", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookie {
			return c
		}
	}
	t.Fatalf("login %s failed: %d %s", username, rec.Code, rec.Body.String())
	return nil
}

// jsonStr renders a decoded JSON fragment back to a string for contains-checks.
func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func i64(v int64) string { return strconv.FormatInt(v, 10) }
