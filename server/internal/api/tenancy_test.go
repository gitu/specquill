package api

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"specquill/server/internal/config"
)

// Cross-tenant isolation: a second tenant's repos are invisible and
// unreachable without membership; membership + the X-SpecQuill-Tenant header
// selects them; the default tenant stays the implicit single choice.
func TestTenantIsolation(t *testing.T) {
	h, st, git := testServerFull(t, false)
	cookie := login(t, h)

	// second tenant with its own repo, registered at runtime
	acme, err := st.EnsureTenant("acme", "github", 42, "Acme Corp")
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "acme-src")
	for _, args := range [][]string{
		{"init", "-b", "main", src},
		{"-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if _, err := git.AddRepo("acme", config.RepoConfig{ID: "specs", Mode: config.Writable, Remote: src, DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}

	// without membership: acme is unreachable, even named explicitly
	req := httptest.NewRequest("GET", "/api/repos/specs/tree", nil)
	req.Header.Set("X-SpecQuill", "1")
	req.Header.Set("X-SpecQuill-Tenant", "acme")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-member acme access: want 403, got %d %s", rec.Code, rec.Body.String())
	}

	// default tenant (sole membership) does not list or resolve acme's repo
	code, _ := doJSON(t, h, cookie, "GET", "/api/repos/specs/tree", nil)
	if code != http.StatusNotFound {
		t.Fatalf("acme repo via default tenant: want 404, got %d", code)
	}

	// membership + header reaches the acme repo…
	u, err := st.UpsertUser("local", "flo", "Flo Test", "flo@test.local")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureMember(acme.ID, u.ID, "member"); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("GET", "/api/repos/specs/tree", nil)
	req.Header.Set("X-SpecQuill", "1")
	req.Header.Set("X-SpecQuill-Tenant", "acme")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("member acme access: want 200, got %d %s", rec.Code, rec.Body.String())
	}

	// …and with two memberships an unqualified request must name the tenant
	code, out := doJSON(t, h, cookie, "GET", "/api/repos/w/tree", nil)
	if code != http.StatusBadRequest || out["code"] != "tenant_required" {
		t.Fatalf("ambiguous tenant: want 400 tenant_required, got %d %v", code, out)
	}

	// disk isolation: the two tenants' clones live under separate roots
	r1, _ := git.Repo("default/w")
	r2, _ := git.Repo("acme/specs")
	if r1.Tenant() == r2.Tenant() || r1.Key() == r2.Key() {
		t.Fatalf("tenant separation broken: %s vs %s", r1.Key(), r2.Key())
	}
}
