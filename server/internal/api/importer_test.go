package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/importer"
	"specquill/server/internal/store"
)

// P5 end-to-end: a non-git (openapi) source materializes as a mirror repo, the
// sync endpoint imports it, and the result is browsable — but only when the
// source is granted (stage 2). Revoking the grant refuses both sync and browse.
func TestSourceImportSyncAndBrowse(t *testing.T) {
	spec := `{"openapi":"3.0.0","info":{"title":"Trade API","version":"1.0"},"paths":{"/trades":{"get":{"summary":"List trades"}}}}`
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(spec))
	}))
	defer backend.Close()

	h, st, imp, ten := testImporterServer(t, backend.URL)
	cookie := login(t, h)

	// ungranted: sync is refused (stage-2 gate)
	code, out := doJSON(t, h, cookie, "POST", "/api/sources/api/sync", nil)
	if code != http.StatusForbidden || out["code"] != "source_forbidden" {
		t.Fatalf("ungranted sync: want 403 source_forbidden, got %d %v", code, out)
	}

	// grant → sync imports the spec into the mirror repo
	src, err := st.SourceByName(ten.ID, "api")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.GrantSource(ten.ID, src.ID, 0); err != nil {
		t.Fatal(err)
	}
	code, out = doJSON(t, h, cookie, "POST", "/api/sources/api/sync", nil)
	if code != http.StatusOK || out["status"] != "ok" {
		t.Fatalf("granted sync: %d %v", code, out)
	}
	if fc, _ := out["fileCount"].(float64); fc < 2 {
		t.Fatalf("expected the import to write index.md + openapi.yaml, got fileCount=%v", out["fileCount"])
	}
	_ = imp

	// the imported content is browsable through the normal read path
	code, files := doJSONList(t, h, cookie, "GET", "/api/repos/api/tree")
	if code != http.StatusOK {
		t.Fatalf("browse granted source: %d", code)
	}
	if !contains(files, "index.md") || !contains(files, "openapi.yaml") {
		t.Fatalf("mirror tree missing imported files: %v", files)
	}

	// a status row was recorded
	rec, err := st.SourceSyncStatus(ten.ID, "api")
	if err != nil || rec.Status != "ok" || rec.FileCount < 2 {
		t.Fatalf("sync status not recorded: %+v err=%v", rec, err)
	}

	// revoke → sync and browse both refused again
	if err := st.RevokeGrant(ten.ID, src.ID); err != nil {
		t.Fatal(err)
	}
	code, _ = doJSON(t, h, cookie, "POST", "/api/sources/api/sync", nil)
	if code != http.StatusForbidden {
		t.Fatalf("revoked sync: want 403, got %d", code)
	}
	code, _ = doJSON(t, h, cookie, "GET", "/api/repos/api/tree", nil)
	if code != http.StatusForbidden {
		t.Fatalf("revoked browse: want 403, got %d", code)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// testImporterServer builds a server whose catalog holds one writable project
// and one openapi source (materialized as a mirror repo) backed by remoteURL,
// with the importer.Runner registered for that source.
func testImporterServer(t *testing.T, remoteURL string) (http.Handler, *store.Store, *importer.Runner, *store.Tenant) {
	t.Helper()
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src")
	gitRun(t, "init", "-b", "main", src)
	gitRun(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "init")

	cfg := &config.Config{
		Tenant:   &config.TenantConfig{Slug: "default", DisplayName: "Workspace", DefaultRole: "editor"},
		DataDir:  filepath.Join(tmp, "data"),
		Git:      config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session:  config.SessionConfig{TTL: time.Hour, CookieSecure: false},
		Auth:     config.AuthConfig{Local: config.LocalAuthConfig{Enabled: true}},
		Projects: []config.ProjectConfig{{ID: "w", Remote: src, DefaultBranch: "main"}},
		Sources:  []config.SourceConfig{{Name: "api", Kind: "openapi", Remote: remoteURL, DefaultBranch: "main", SyncInterval: time.Hour}},
	}
	cfg.Normalize() // materializes project "w" + mirror repo "api" into cfg.Repos

	st := store.OpenTest(t)
	ten, err := st.EnsureTenant("default", "config", 0, "Workspace")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SyncTenantProjects(ten.ID, []store.Project{{ProjectID: "w", RepoID: "w"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SyncGlobalSources([]store.Source{{Name: "api", Kind: "openapi", Remote: remoteURL, DefaultBranch: "main", SyncInterval: 3600}}); err != nil {
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
	imp := importer.NewRunner(git, st)
	imp.Register(ten.Slug, ten.ID, cfg.Sources[0])

	h := New(cfg, git, Options{
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		Importer: imp,
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	return h, st, imp, ten
}
