package api

import (
	"net/http"
	"testing"

	"specquill/server/internal/config"
)

// Enrollment: the first authenticated contact enrolls the user with
// auth.default_role; the role is sticky (a later config change does not
// downgrade); admin_emails promotes to admin regardless.
func TestAutoEnrollment(t *testing.T) {
	h, _, _ := testServerFull(t, false)
	cookie := login(t, h)
	code, out := doJSON(t, h, cookie, "GET", "/api/me", nil)
	if code != http.StatusOK || out["role"] != "member" {
		t.Fatalf("default enrollment: want member, got %d %v", code, out)
	}
}

func TestEnrollmentDefaultRoleViewer(t *testing.T) {
	h, st, _ := testServerCfg(t, false, func(c *config.Config) {
		c.Auth.DefaultRole = "viewer"
	})
	cookie := login(t, h)
	code, out := doJSON(t, h, cookie, "GET", "/api/me", nil)
	if code != http.StatusOK || out["role"] != "viewer" {
		t.Fatalf("viewer enrollment: want viewer, got %d %v", code, out)
	}
	// enrolled role is sticky: EnsureUserRole must not overwrite it
	flo := userID(t, st, "flo@test.local")
	if err := st.EnsureUserRole(flo, "member"); err != nil {
		t.Fatal(err)
	}
	if code, out := doJSON(t, h, cookie, "GET", "/api/me", nil); code != http.StatusOK || out["role"] != "viewer" {
		t.Fatalf("role not sticky: %d %v", code, out)
	}
	// viewers never reach the management API
	if code, _ := doJSON(t, h, cookie, "GET", "/api/members", nil); code != http.StatusForbidden {
		t.Fatalf("viewer management access: want 403, got %d", code)
	}
}

func TestAdminEmailBootstrap(t *testing.T) {
	h, _, _ := testServerCfg(t, false, func(c *config.Config) {
		c.Auth.AdminEmails = []string{"FLO@test.local"} // case-insensitive
	})
	cookie := login(t, h)
	code, out := doJSON(t, h, cookie, "GET", "/api/me", nil)
	if code != http.StatusOK || out["role"] != "admin" {
		t.Fatalf("admin bootstrap: want admin, got %d %v", code, out)
	}
	if code, _ := doJSON(t, h, cookie, "GET", "/api/members", nil); code != http.StatusOK {
		t.Fatalf("admin management access: want 200, got %d", code)
	}
}
