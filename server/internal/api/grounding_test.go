package api

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"specquill/server/internal/ai"
	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

// Trust boundary: the copilot only grounds on sources that are BOTH selected
// in the project's in-repo config AND present in the catalog. Removing the
// catalog entry drops the source from the copilot context even though the
// in-repo selection is unchanged — an in-repo file can never mint access.
func TestGroundingRequiresCatalogEntry(t *testing.T) {
	h, st, git := testGroundingServer(t)
	cookie := login(t, h)

	// register the readonly regulations repo, NOT yet in the catalog
	reg := filepath.Join(t.TempDir(), "reg-src")
	gitRun(t, "init", "-b", "main", reg)
	if err := os.MkdirAll(filepath.Join(reg, "regulations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reg, "regulations", "mifid-ii.md"), []byte("RTS 22: microsecond timestamps."), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", reg, "-c", "user.name=t", "-c", "user.email=t@t", "add", "-A")
	gitRun(t, "-C", reg, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "reg")
	if _, err := git.AddRepo(config.RepoConfig{ID: "reg", Mode: config.ReadOnly, Remote: reg, DefaultBranch: "main"}, ""); err != nil {
		t.Fatal(err)
	}

	grounded := func() []any {
		code, out := doJSON(t, h, cookie, "GET", "/api/copilot/info?repo=w", nil)
		if code != http.StatusOK {
			t.Fatalf("copilot info: %d %v", code, out)
		}
		g, _ := out["groundedSources"].([]any)
		return g
	}

	// selected in-repo but NOT cataloged → not grounded
	if g := grounded(); len(g) != 0 {
		t.Fatalf("uncataloged source leaked into grounding: %v", g)
	}

	// catalog it → the selection now takes effect
	if err := st.SyncSources([]store.Source{{Name: "reg", Kind: "git", Remote: reg, DefaultBranch: "main", SyncInterval: 300}}); err != nil {
		t.Fatal(err)
	}
	if g := grounded(); len(g) != 1 || g[0] != "reg" {
		t.Fatalf("cataloged source not grounded: %v", g)
	}

	// remove the entry → grounding drops it again, in-repo selection untouched
	if err := st.SyncSources(nil); err != nil {
		t.Fatal(err)
	}
	if g := grounded(); len(g) != 0 {
		t.Fatalf("removed source still grounded: %v", g)
	}
}

func gitRun(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// testGroundingServer builds a server whose writable project "w" selects the
// "reg" source (grounding: true) in its in-repo config, with the AI client
// enabled (mock model, never dialed by /api/copilot/info).
func testGroundingServer(t *testing.T) (http.Handler, *store.Store, *gitx.Manager) {
	t.Helper()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	gitRun(t, "init", "-b", "main", src)
	if err := os.MkdirAll(filepath.Join(src, ".specquill"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgYML := "version: 2\nproject: w\nreferences:\n  - source: reg\n    grounding: true\n"
	if err := os.WriteFile(filepath.Join(src, ".specquill", "config.yml"), []byte(cfgYML), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "add", "-A")
	gitRun(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "init")

	cfg := &config.Config{
		DataDir: filepath.Join(tmp, "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth:    config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Repos:   []config.RepoConfig{{ID: "w", Mode: config.Writable, Remote: src, DefaultBranch: "main"}},
	}
	st := store.OpenTest(t)
	if err := st.SyncProjects([]store.Project{{ProjectID: "w", RepoID: "w"}}); err != nil {
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
	return New(cfg, git, Options{
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		AI:       ai.New(config.AIConfig{Enabled: true, Model: "mock-1"}),
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	}), st, git
}
