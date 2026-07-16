package githubapp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"specquill/server/internal/config"
)

func testKeyPEM(t *testing.T) (string, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	return pemStr, &key.PublicKey
}

func newTestApp(t *testing.T, apiBase string) (*App, *rsa.PublicKey) {
	t.Helper()
	pemStr, pub := testKeyPEM(t)
	t.Setenv("TEST_APP_KEY", pemStr)
	app, err := New(config.GitHubAppConfig{AppID: 42, PrivateKeyEnv: "TEST_APP_KEY", WebhookSecretEnv: "X", APIBase: apiBase})
	if err != nil {
		t.Fatal(err)
	}
	return app, pub
}

func TestAppJWT(t *testing.T) {
	app, pub := newTestApp(t, "http://unused")
	jwt, err := app.appJWT()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d parts", len(parts))
	}
	// signature verifies against the public key
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	// claims carry the app id and a ~9 minute expiry
	var claims struct{ Iss, Iat, Exp int64 }
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Iss != 42 || claims.Exp <= time.Now().Unix() {
		t.Fatalf("bad claims: %+v", claims)
	}
}

func TestInstallationTokenCaching(t *testing.T) {
	var mints atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/installations/7/access_tokens" {
			mints.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "ghs_test", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	app, _ := newTestApp(t, srv.URL)
	for i := 0; i < 3; i++ {
		tok, err := app.InstallationToken(7)
		if err != nil || tok != "ghs_test" {
			t.Fatalf("token %d: %q %v", i, tok, err)
		}
	}
	if n := mints.Load(); n != 1 {
		t.Fatalf("expected 1 mint (cache), got %d", n)
	}
}

func TestPermissionNotCollaborator(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/access_tokens"):
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
		case strings.Contains(r.URL.Path, "/collaborators/"):
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	app, _ := newTestApp(t, srv.URL)
	perm, err := app.Permission(7, "acme/specs", "stranger")
	if err != nil || perm != "none" {
		t.Fatalf("want none, got %q %v", perm, err)
	}
}

func TestRepoFromRemote(t *testing.T) {
	cases := []struct {
		remote string
		want   string
		ok     bool
	}{
		{"https://github.com/acme/specs.git", "acme/specs", true},
		{"https://github.com/acme/specs", "acme/specs", true},
		{"git@github.com:acme/specs.git", "acme/specs", true},
		{"ssh://git@github.com/acme/specs.git", "acme/specs", true},
		{"https://gitlab.com/acme/specs.git", "", false},
		{"https://github.com/acme", "", false},
		{"/srv/git/specs.git", "", false},
	}
	for _, c := range cases {
		got, ok := RepoFromRemote(c.remote)
		if got != c.want || ok != c.ok {
			t.Errorf("RepoFromRemote(%q) = %q,%v; want %q,%v", c.remote, got, ok, c.want, c.ok)
		}
	}
}

func TestRepoInstallation(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/repos/acme/specs/installation":
			_ = json.NewEncoder(w).Encode(map[string]int64{"id": 7})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	app, _ := newTestApp(t, srv.URL)

	// covered repo resolves and caches
	id, err := app.RepoInstallation("acme/specs")
	if err != nil || id != 7 {
		t.Fatalf("RepoInstallation: %v %d", err, id)
	}
	if id, _ = app.RepoInstallation("acme/specs"); id != 7 || calls.Load() != 1 {
		t.Fatalf("hit not cached: id=%d calls=%d", id, calls.Load())
	}

	// uncovered repo → ErrNotInstalled, negative-cached
	if _, err := app.RepoInstallation("other/repo"); err != ErrNotInstalled {
		t.Fatalf("want ErrNotInstalled, got %v", err)
	}
	n := calls.Load()
	if _, err := app.RepoInstallation("other/repo"); err != ErrNotInstalled || calls.Load() != n {
		t.Fatalf("miss not cached: %v calls=%d", err, calls.Load())
	}
}
