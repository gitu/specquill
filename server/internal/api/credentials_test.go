package api

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/secrets"
)

// Admin can store, list (metadata only), and revoke a tenant credential; the
// secret value is write-only and never returned by a read.
func TestCredentialAdminAPI(t *testing.T) {
	h, st, _ := testServerFull(t, false)
	cookie := login(t, h)

	// enable the encrypted credential store on the shared store handle
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	t.Setenv("API_MK", base64.StdEncoding.EncodeToString(key))
	sl, err := secrets.NewSealer(config.SecretsConfig{MasterKeyEnv: "API_MK"})
	if err != nil {
		t.Fatal(err)
	}
	st.SetSealer(sl)

	// member cannot manage credentials
	if code, _ := doJSON(t, h, cookie, "PUT", "/api/credentials/git_pat/w", map[string]string{"secret": "ghp_x"}); code != http.StatusForbidden {
		t.Fatalf("member put credential: want 403, got %d", code)
	}

	// promote to admin
	u, err := st.UpsertUser("local", "flo", "Flo Test", "flo@test.local")
	if err != nil {
		t.Fatal(err)
	}
	ten, err := st.TenantBySlug(gitx.DefaultTenant)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMemberRole(ten.ID, u.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	// store a credential
	if code, _ := doJSON(t, h, cookie, "PUT", "/api/credentials/git_pat/w", map[string]string{"secret": "ghp_secret"}); code != http.StatusOK {
		t.Fatalf("admin put credential: %d", code)
	}

	// list: metadata present, secret value never returned
	req := httptest.NewRequest("GET", "/api/credentials", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "git_pat") {
		t.Fatalf("list: %d %s", rec.Code, body)
	}
	if strings.Contains(body, "ghp_secret") {
		t.Fatalf("credential list leaked the secret value: %s", body)
	}

	// unknown kind rejected
	if code, _ := doJSON(t, h, cookie, "PUT", "/api/credentials/bogus/w", map[string]string{"secret": "x"}); code != http.StatusBadRequest {
		t.Fatalf("unknown kind: want 400, got %d", code)
	}

	// revoke
	if code, _ := doJSON(t, h, cookie, "DELETE", "/api/credentials/git_pat/w", nil); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
}
