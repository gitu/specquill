package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

func TestRemoteFullName(t *testing.T) {
	cases := map[string]string{
		"https://github.com/Acme/Specs.git":  "acme/specs",
		"https://github.com/acme/specs":      "acme/specs",
		"http://ghe.corp/org/repo.git":       "org/repo",
		"ssh://git@github.com/acme/specs":    "acme/specs",
		"git@github.com:acme/specs.git":      "acme/specs",
		"/data/origin/trading-specs.git":     "",
		"":                                   "",
		"https://github.com":                 "",
	}
	for remote, want := range cases {
		if got := remoteFullName(remote); got != want {
			t.Errorf("remoteFullName(%q) = %q, want %q", remote, got, want)
		}
	}
}

// webhookTestServer registers a writable repo whose remote LOOKS like a
// GitHub URL but resolves to a local origin via git's env-config insteadOf —
// so the webhook's matching, fetch and default-branch ff run for real.
func webhookTestServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	tmp := t.TempDir()

	// bare origin with one commit on main
	origin := filepath.Join(tmp, "origin.git")
	work := filepath.Join(tmp, "work")
	gitRun(t, "init", "-b", "main", work)
	if err := os.WriteFile(filepath.Join(work, "index.md"), []byte("# v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", work, "-c", "user.name=t", "-c", "user.email=t@t", "add", "-A")
	gitRun(t, "-C", work, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "v1")
	gitRun(t, "init", "--bare", "-b", "main", origin)
	gitRun(t, "-C", work, "push", "-q", origin, "main")

	// the github-shaped remote resolves to the local origin for every git
	// child process (gitx passes os.Environ through)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+origin+".insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/acme/specs.git")
	t.Setenv("TEST_WEBHOOK_SECRET", "hook-secret")

	cfg := &config.Config{
		DataDir:  filepath.Join(tmp, "data"),
		Git:      config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session:  config.SessionConfig{TTL: time.Hour},
		Auth:     config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Webhooks: config.WebhooksConfig{GitHub: config.GitHubWebhookConfig{Enabled: true, SecretEnv: "TEST_WEBHOOK_SECRET"}},
		Repos:    []config.RepoConfig{{ID: "w", Mode: config.Writable, Remote: "https://github.com/acme/specs.git", DefaultBranch: "main"}},
	}
	st := store.OpenTest(t)
	ten, err := st.EnsureTenant(gitx.DefaultTenant, "config", 0, "Workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SyncTenantProjects(ten.ID, []store.Project{{ProjectID: "w", RepoID: "w"}}); err != nil {
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
	h := New(cfg, git, Options{
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	return h, work
}

func signedHook(t *testing.T, h http.Handler, event string, payload any, secret string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	req := httptest.NewRequest("POST", "/hooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGitHubWebhook(t *testing.T) {
	h, work := webhookTestServer(t)
	origin := filepath.Join(filepath.Dir(work), "origin.git")
	cookie := login(t, h)

	// ping with a valid signature (and no CSRF header) → ok
	if rec := signedHook(t, h, "ping", map[string]any{"zen": "keep it simple"}, "hook-secret"); rec.Code != http.StatusOK {
		t.Fatalf("ping: %d %s", rec.Code, rec.Body.String())
	}

	// wrong secret → 401, nothing leaks
	if rec := signedHook(t, h, "push", map[string]any{}, "wrong-secret"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature: want 401, got %d", rec.Code)
	}

	// push a v2 commit to the origin OUTSIDE specquill
	if err := os.WriteFile(filepath.Join(work, "index.md"), []byte("# v2 external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", work, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-am", "v2")
	gitRun(t, "-C", work, "push", "-q", origin, "main")

	// the served main is still v1 (no sync interval in this config)
	code, out := doJSON(t, h, cookie, "GET", "/api/repos/w/files/index.md?ref=main", nil)
	if code != http.StatusOK || out["content"] != "# v1\n" {
		t.Fatalf("precondition: %d %v", code, out)
	}

	// signed push webhook for the matching repo → fetch + ff of main
	push := map[string]any{
		"ref":        "refs/heads/main",
		"repository": map[string]any{"full_name": "acme/specs"},
	}
	rec := signedHook(t, h, "push", push, "hook-secret")
	var resp struct {
		Matched int  `json:"matched"`
		OK      bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusOK || !resp.OK || resp.Matched != 1 {
		t.Fatalf("push hook: %d %s", rec.Code, rec.Body.String())
	}
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/w/files/index.md?ref=main", nil)
	if code != http.StatusOK || out["content"] != "# v2 external\n" {
		t.Fatalf("main not fast-forwarded by webhook: %d %v", code, out)
	}

	// a push for an unknown repository matches nothing
	rec = signedHook(t, h, "push", map[string]any{
		"ref": "refs/heads/main", "repository": map[string]any{"full_name": "acme/other"},
	}, "hook-secret")
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Matched != 0 {
		t.Fatalf("unrelated repo matched: %s", rec.Body.String())
	}
}

