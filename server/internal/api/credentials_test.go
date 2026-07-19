package api

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"specquill/server/internal/config"
	"specquill/server/internal/secrets"
)

func withSecretKey(t *testing.T) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv(secrets.EnvKey, base64.StdEncoding.EncodeToString(key))
}

// REQ-023: credentials are admin-managed, sealed at rest, never echoed, and
// the initial clone of a connected repo already resolves them (clone-last).
func TestCredentialAPI(t *testing.T) {
	withSecretKey(t)
	h, st, _ := testServerCfg(t, false, func(c *config.Config) {
		c.Tenant.AdminEmails = []string{"flo@test.local"}
	})
	_ = st
	cookie := login(t, h)
	token := "ghp_super-secret-value"

	// create
	code, out := doJSON(t, h, cookie, "POST", "/api/credentials", map[string]string{"name": "deploy PAT", "token": token})
	if code != http.StatusOK {
		t.Fatalf("create: %d %v", code, out)
	}
	id := jsonStr(out["id"])

	// list is redacted — the raw response must not contain the token or
	// any sealed material fields
	req := httptest.NewRequest("GET", apiURL("/api/credentials"), nil)
	req.Header.Set("X-SpecQuill", "1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "deploy PAT") {
		t.Fatalf("list: %d %s", rec.Code, body)
	}
	for _, needle := range []string{token, "ciphertext", "nonce", "key_id", "keyId"} {
		if strings.Contains(body, needle) {
			t.Fatalf("credential list leaks %q: %s", needle, body)
		}
	}

	// connect a repo with the credential attached — a local bare remote, so
	// the clone exercises the clone-last ordering end to end
	src := filepath.Join(t.TempDir(), "priv-src")
	for _, args := range [][]string{
		{"init", "-b", "main", src},
		{"-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "init"},
	} {
		if outB, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, outB)
		}
	}
	idNum, _ := strconv.Atoi(id)
	code, out = doJSON(t, h, cookie, "POST", "/api/projects", map[string]any{
		"id": "priv", "remote": src, "credentialId": idNum,
	})
	if code != http.StatusOK {
		t.Fatalf("connect: %d %v", code, out)
	}
	if code, _ := doJSON(t, h, cookie, "GET", "/api/repos/priv/tree", nil); code != http.StatusOK {
		t.Fatalf("connected repo unreadable: %d", code)
	}

	// attached → deletion refuses
	code, out = doJSON(t, h, cookie, "DELETE", "/api/credentials/"+id, nil)
	if code != http.StatusConflict || out["code"] != "credential_in_use" {
		t.Fatalf("delete while attached: want 409, got %d %v", code, out)
	}

	// rotate (token never echoed back)
	code, out = doJSON(t, h, cookie, "PUT", "/api/credentials/"+id, map[string]string{"token": "ghp_rotated"})
	if code != http.StatusOK {
		t.Fatalf("rotate: %d %v", code, out)
	}

	// detach, then delete succeeds
	if code, out = doJSON(t, h, cookie, "PUT", "/api/repos/priv/settings/credential", map[string]int{"credentialId": 0}); code != http.StatusOK {
		t.Fatalf("detach: %d %v", code, out)
	}
	if code, out = doJSON(t, h, cookie, "DELETE", "/api/credentials/"+id, nil); code != http.StatusOK {
		t.Fatalf("delete: %d %v", code, out)
	}
}

// Without the master key the server runs; credential endpoints answer 501.
func TestCredentialsUnconfigured(t *testing.T) {
	t.Setenv(secrets.EnvKey, "")
	h, _, _ := testServerCfg(t, false, func(c *config.Config) {
		c.Tenant.AdminEmails = []string{"flo@test.local"}
	})
	cookie := login(t, h)
	if code, _ := doJSON(t, h, cookie, "GET", "/api/credentials", nil); code != http.StatusOK {
		t.Fatalf("list must still answer (empty), got %d", code)
	}
	code, out := doJSON(t, h, cookie, "POST", "/api/credentials", map[string]string{"name": "x", "token": "y"})
	if code != http.StatusNotImplemented || out["code"] != "secrets_unconfigured" {
		t.Fatalf("create without key: want 501 secrets_unconfigured, got %d %v", code, out)
	}
}
