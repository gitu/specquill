package api

// Token-scoped dynamic projects (REQ-025) end to end: manifest-gated open,
// per-repo forge permissions, budget/reclaim/janitor, and the commit-time
// push. Rides the forge-PAT harness from patauth_test.go.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/forge"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
	"testing/fstest"
)

// dynMockForge extends the GitLab mock with repository resolution, manifest
// reads and search: acme/dyn is a manifest-carrying repo (id 777), acme/plain
// has no manifest (id 778). Anchor role (acme/specs) and dynamic-repo role
// both derive from the token's level.
func dynMockForge(t *testing.T, roles map[string]int, dynRemote, manifest string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("PRIVATE-TOKEN")
		level, ok := roles[tok]
		if !ok {
			http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		perms := fmt.Sprintf(`"permissions":{"project_access":{"access_level":%d},"group_access":null}`, level)
		switch {
		case r.URL.Path == "/api/v4/user":
			fmt.Fprintf(w, `{"id":%d,"username":"user-%s","name":"User %s","email":"%s@test.local","commit_email":""}`,
				100+level, tok, tok, tok)
		case strings.Contains(r.URL.Path, "/repository/files/"):
			if strings.Contains(r.URL.Path, "/projects/acme/dyn/") {
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, manifest)
				return
			}
			http.Error(w, `{"message":"404 File Not Found"}`, http.StatusNotFound)
		case r.URL.Path == "/api/v4/projects" && r.URL.Query().Get("membership") == "true":
			fmt.Fprintf(w, `[{"id":777,"path_with_namespace":"acme/dyn","http_url_to_repo":"%s","default_branch":"main","web_url":"https://forge.test/acme/dyn"}]`, dynRemote)
		case r.URL.Path == "/api/v4/projects/acme/dyn":
			if level == 0 {
				http.Error(w, `{"message":"404 Project Not Found"}`, http.StatusNotFound)
				return
			}
			fmt.Fprintf(w, `{"id":777,"path_with_namespace":"acme/dyn","http_url_to_repo":"%s","default_branch":"main","web_url":"u",%s}`, dynRemote, perms)
		case r.URL.Path == "/api/v4/projects/acme/plain":
			fmt.Fprintf(w, `{"id":778,"path_with_namespace":"acme/plain","http_url_to_repo":"%s","default_branch":"main","web_url":"u",%s}`, dynRemote, perms)
		case r.URL.Path == "/api/v4/projects/acme/gone":
			http.Error(w, `{"message":"404 Project Not Found"}`, http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodGet:
			fmt.Fprint(w, `[]`)
		case strings.Contains(r.URL.Path, "/projects/"):
			fmt.Fprintf(w, `{%s}`, perms)
		default:
			t.Errorf("dyn mock forge: unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
}

type dynEnv struct {
	patEnv
	dynSrc string // the dynamic repo's source dir (origin working copy)
}

// dynamicServer boots a forge-PAT server with dynamic projects enabled: an
// anchor project plus a manifest-carrying repo (acme/dyn → id 777, workspace
// `specs` rooted at docs/specs) served over authenticated dumb HTTP.
func dynamicServer(t *testing.T, manifest string) dynEnv {
	t.Helper()
	tmp := t.TempDir()

	// the dynamic repo: root manifest + a workspace under docs/specs
	dynSrc := filepath.Join(tmp, "dynsrc")
	gitOut(t, "init", "-b", "main", dynSrc)
	for p, c := range map[string]string{
		".specquill/config.yml": manifest,
		"docs/specs/index.md":   "# Dyn specs\n",
		"README.md":             "code\n",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dynSrc, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dynSrc, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitOut(t, "-C", dynSrc, "add", ".")
	gitOut(t, "-C", dynSrc, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "init")
	dynBare := filepath.Join(tmp, "dyn.git")
	gitOut(t, "clone", "--bare", dynSrc, dynBare)
	gitOut(t, "-C", dynBare, "update-server-info")
	// pushes into the bare need a post-update hook to refresh dumb-HTTP info,
	// but the tests only clone — keep it simple
	dynSrv := dumbGitServer(t, dynBare, map[string]bool{"tok-a": true, "tok-b": true})
	t.Cleanup(dynSrv.Close)

	// anchor project (local path remote, forge project named explicitly)
	src := filepath.Join(tmp, "src")
	gitOut(t, "init", "-b", "main", src)
	if err := os.WriteFile(filepath.Join(src, "index.md"), []byte("# Anchor\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, "-C", src, "add", ".")
	gitOut(t, "-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "init")

	forgeSrv := dynMockForge(t, map[string]int{"tok-a": 30, "tok-b": 20}, dynSrv.URL+"/ref.git", manifest)
	t.Cleanup(forgeSrv.Close)

	cfg := &config.Config{
		DataDir: filepath.Join(tmp, "data"),
		Git:     config.GitConfig{CommitterName: "svc", CommitterEmail: "svc@t"},
		Session: config.SessionConfig{TTL: time.Hour},
		Auth: config.AuthConfig{
			Forge: config.ForgeAuthConfig{Kind: forge.KindGitLab, BaseURL: forgeSrv.URL},
		},
		Dynamic: config.DynamicConfig{Enabled: true, Search: true},
		Projects: []config.ProjectConfig{{
			ID: "w", Remote: src, DefaultBranch: "main",
			Forge: forge.Config{Project: "acme/specs"},
		}},
	}
	cfg.Normalize()

	st := store.OpenTest(t)
	if err := st.SyncProjects([]store.Project{{ProjectID: "w", RepoID: "w"}}); err != nil {
		t.Fatal(err)
	}
	git, err := gitx.NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h, srv := NewServer(cfg, git, Options{
		Store:    st,
		Sessions: auth.NewSessions(st, cfg),
		Dist:     fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}},
	})
	return dynEnv{patEnv: patEnv{h: h, srv: srv, st: st, dataDir: cfg.DataDir, mainSrc: src}, dynSrc: dynSrc}
}

const dynManifest = "version: 2\nprojects:\n  - name: specs\n    root: docs/specs\n"

func TestDynamicOpenAndBrowse(t *testing.T) {
	env := dynamicServer(t, dynManifest)
	session, _ := patLogin(t, env.h, "tok-a")

	// feature discovery
	rec := patDo(env.h, "GET", "/api/dynamic", session, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("dynamic info: %d %s", rec.Code, rec.Body.String())
	}

	// open by spelling — the sole manifest entry resolves without #name too,
	// but the explicit form is the canonical one
	rec = patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn#specs"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}
	var opened struct {
		ID       string `json:"id"`
		Root     string `json:"root"`
		Readonly bool   `json:"readonly"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &opened)
	if opened.ID != "dyn.777.specs" || opened.Root != "docs/specs" || opened.Readonly {
		t.Fatalf("opened: %+v", opened)
	}

	// content-root mapping: the tree is the workspace, not the repo root
	rec = patDo(env.h, "GET", "/api/repos/dyn.777.specs/tree", session, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "index.md") ||
		strings.Contains(rec.Body.String(), "README.md") {
		t.Fatalf("tree: %d %s", rec.Code, rec.Body.String())
	}

	// listed alongside the configured projects, with the spelling
	rec = patDo(env.h, "GET", "/api/repos", session, "")
	if !strings.Contains(rec.Body.String(), `"dyn.777.specs"`) || !strings.Contains(rec.Body.String(), `"acme/dyn#specs"`) {
		t.Fatalf("repo list: %s", rec.Body.String())
	}

	// bare spelling with a single manifest entry auto-resolves to it
	rec = patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"dyn.777.specs"`) {
		t.Fatalf("bare open: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDynamicManifestGates(t *testing.T) {
	env := dynamicServer(t, dynManifest)
	session, _ := patLogin(t, env.h, "tok-a")

	// no manifest → not openable (REQ-025.1), however far the token reaches
	rec := patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/plain"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "no_manifest") {
		t.Fatalf("manifest-less open: want 409 no_manifest, got %d %s", rec.Code, rec.Body.String())
	}

	// unknown subproject name
	rec = patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn#nope"}`)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "unknown_subproject") {
		t.Fatalf("unknown name: %d %s", rec.Code, rec.Body.String())
	}

	// a repo the forge hides from this token
	rec = patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/gone"}`)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "repo_unreachable") {
		t.Fatalf("hidden repo: %d %s", rec.Code, rec.Body.String())
	}

	// foreign hosts never resolve (REQ-025.1: the deployment's forge alone)
	rec = patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"https://evil.example.com/acme/dyn.git"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign host: want 400, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestDynamicChooseAmongDeclared(t *testing.T) {
	env := dynamicServer(t, "version: 2\nprojects:\n  - name: specs\n    root: docs/specs\n  - name: product\n    root: product\n")
	session, _ := patLogin(t, env.h, "tok-a")

	rec := patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "choose_project") {
		t.Fatalf("want 409 choose_project, got %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Choices []struct{ Name, Root string } `json:"choices"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Choices) != 2 || resp.Choices[0].Name != "specs" {
		t.Fatalf("choices: %+v", resp.Choices)
	}

	// a declared root that does not exist yet is still openable (the
	// declaration is the consent; the folder comes with the first change)
	rec = patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn#product"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty-root open: %d %s", rec.Code, rec.Body.String())
	}
	rec = patDo(env.h, "GET", "/api/repos/dyn.777.product/tree", session, "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty workspace tree: %d %s", rec.Code, rec.Body.String())
	}
}

// REQ-025.3: the user's forge permission on the dynamic repository governs,
// alone — a reporter (viewer) gets a read-only project even though the same
// user edits the anchor as a viewer... and vice versa: tok-b is viewer
// everywhere here, so the dynamic project must refuse writes.
func TestDynamicViewerIsReadOnly(t *testing.T) {
	env := dynamicServer(t, dynManifest)
	session, _ := patLogin(t, env.h, "tok-b") // level 20 → viewer

	rec := patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn#specs"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"readonly":true`) {
		t.Fatalf("viewer open: %d %s", rec.Code, rec.Body.String())
	}
	// reading works…
	rec = patDo(env.h, "GET", "/api/repos/dyn.777.specs/tree", session, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer tree: %d %s", rec.Code, rec.Body.String())
	}
	// …writing does not
	rec = patDo(env.h, "PUT", "/api/repos/dyn.777.specs/files/x.md", session, `{"content":"x","baseSha":""}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer write: want 403, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestDynamicReclaimAndRematerialize(t *testing.T) {
	env := dynamicServer(t, dynManifest)
	session, _ := patLogin(t, env.h, "tok-a")
	if rec := patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn#specs"}`); rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}
	clones, _ := filepath.Glob(filepath.Join(env.dataDir, "repos", "u*", "dyn.777.specs", "git", "HEAD"))
	if len(clones) != 1 {
		t.Fatalf("clone missing: %v", clones)
	}

	// clean clone reclaims without force; the entry survives
	rec := patDo(env.h, "POST", "/api/dynamic/reclaim", session, `{"id":"dyn.777.specs"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reclaim: %d %s", rec.Code, rec.Body.String())
	}
	if clones, _ = filepath.Glob(filepath.Join(env.dataDir, "repos", "u*", "dyn.777.specs", "git", "HEAD")); len(clones) != 0 {
		t.Fatalf("clone survived reclaim: %v", clones)
	}
	rec = patDo(env.h, "GET", "/api/dynamic/checkouts", session, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"materialized":false`) {
		t.Fatalf("checkouts after reclaim: %d %s", rec.Code, rec.Body.String())
	}

	// reopening (or just browsing) re-materializes on demand (REQ-025.6)
	rec = patDo(env.h, "GET", "/api/repos/dyn.777.specs/tree", session, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "index.md") {
		t.Fatalf("re-materialize: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDynamicReclaimGuardsUnsynced(t *testing.T) {
	env := dynamicServer(t, dynManifest)
	session, _ := patLogin(t, env.h, "tok-a")
	if rec := patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn#specs"}`); rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}

	// dirty the worktree via the draft path
	rec := patDo(env.h, "POST", "/api/repos/dyn.777.specs/workspace", session, "{}")
	if rec.Code != http.StatusOK {
		t.Fatalf("workspace: %d %s", rec.Code, rec.Body.String())
	}
	var ws struct{ Branch string }
	_ = json.Unmarshal(rec.Body.Bytes(), &ws)
	rec = patDo(env.h, "PUT", "/api/repos/dyn.777.specs/files/draft.md?branch="+ws.Branch, session, `{"content":"wip","baseSha":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}

	// unsynced → refused without force (REQ-025.5)
	rec = patDo(env.h, "POST", "/api/dynamic/reclaim", session, `{"id":"dyn.777.specs"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "unsynced") {
		t.Fatalf("dirty reclaim: want 409 unsynced, got %d %s", rec.Code, rec.Body.String())
	}
	// the explicit discard goes through
	rec = patDo(env.h, "POST", "/api/dynamic/reclaim", session, `{"id":"dyn.777.specs","force":true,"close":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("forced reclaim: %d %s", rec.Code, rec.Body.String())
	}
	// closed: the entry is gone from the open list
	rec = patDo(env.h, "GET", "/api/repos/dyn.777.specs/tree", session, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("closed project should not resolve: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDynamicJanitorReclaimsIdleOnly(t *testing.T) {
	env := dynamicServer(t, dynManifest)
	session, resp := patLogin(t, env.h, "tok-a")
	if rec := patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn#specs"}`); rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}
	uid := int64(resp["id"].(float64))
	scope := fmt.Sprintf("u%d", uid)

	// fresh stamp → survives the janitor
	env.srv.reclaimIdleClones()
	if clones, _ := filepath.Glob(filepath.Join(env.dataDir, "repos", scope, "dyn.777.specs", "git", "HEAD")); len(clones) != 1 {
		t.Fatal("janitor reclaimed a fresh clone")
	}

	// backdate the stamp past the idle period → reclaimed (clean clone)
	old := time.Now().Add(-2 * env.srv.cfg.Dynamic.IdleAfter).Unix()
	if _, err := env.st.DB().Exec("UPDATE clone_uses SET last_used = ? WHERE repo_id = 'dyn.777.specs'", old); err != nil {
		t.Fatal(err)
	}
	env.srv.reclaimIdleClones()
	if clones, _ := filepath.Glob(filepath.Join(env.dataDir, "repos", scope, "dyn.777.specs", "git", "HEAD")); len(clones) != 0 {
		t.Fatal("janitor left an idle clean clone")
	}
	// the entry survives — reclamation frees disk, not the open list
	if _, err := env.st.UserProject(uid, "dyn.777.specs"); err != nil {
		t.Fatalf("user project row should survive the janitor: %v", err)
	}
}

func TestDynamicJanitorRetentionCap(t *testing.T) {
	env := dynamicServer(t, dynManifest)
	session, resp := patLogin(t, env.h, "tok-a")
	if rec := patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn#specs"}`); rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}
	uid := int64(resp["id"].(float64))
	scope := fmt.Sprintf("u%d", uid)

	// dirty the worktree, then backdate past idle but inside retention
	rec := patDo(env.h, "POST", "/api/repos/dyn.777.specs/workspace", session, "{}")
	var ws struct{ Branch string }
	_ = json.Unmarshal(rec.Body.Bytes(), &ws)
	if rec = patDo(env.h, "PUT", "/api/repos/dyn.777.specs/files/draft.md?branch="+ws.Branch, session, `{"content":"wip","baseSha":""}`); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	backdate := func(d time.Duration) {
		if _, err := env.st.DB().Exec("UPDATE clone_uses SET last_used = ? WHERE repo_id = 'dyn.777.specs'",
			time.Now().Add(-d).Unix()); err != nil {
			t.Fatal(err)
		}
	}
	glob := filepath.Join(env.dataDir, "repos", scope, "dyn.777.specs", "git", "HEAD")

	backdate(env.srv.cfg.Dynamic.IdleAfter + time.Hour)
	env.srv.reclaimIdleClones()
	if clones, _ := filepath.Glob(glob); len(clones) != 1 {
		t.Fatal("unsynced clone reclaimed before the retention cap (REQ-025.6)")
	}

	backdate(env.srv.cfg.Dynamic.UnsyncedRetention + time.Hour)
	env.srv.reclaimIdleClones()
	if clones, _ := filepath.Glob(glob); len(clones) != 0 {
		t.Fatal("unsynced clone survived past the retention cap")
	}
}

func TestDynamicSearchTogglesAndBudget(t *testing.T) {
	env := dynamicServer(t, dynManifest)
	session, _ := patLogin(t, env.h, "tok-a")

	rec := patDo(env.h, "GET", "/api/dynamic/search?q=dyn", session, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "acme/dyn") {
		t.Fatalf("search: %d %s", rec.Code, rec.Body.String())
	}
	// search is its own opt-in on top of the feature (REQ-025.2)
	env.srv.cfg.Dynamic.Search = false
	rec = patDo(env.h, "GET", "/api/dynamic/search?q=dyn", session, "")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "search_disabled") {
		t.Fatalf("disabled search: %d %s", rec.Code, rec.Body.String())
	}

	// an exhausted byte budget refuses further opens with the actionable
	// error — materialize one clone first so usage is non-zero
	if rec = patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn#specs"}`); rec.Code != http.StatusOK {
		t.Fatalf("open: %d %s", rec.Code, rec.Body.String())
	}
	env.srv.cfg.Dynamic.UserBudget = 1
	rec = patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn#specs"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "budget_exceeded") {
		t.Fatalf("budget: want 403 budget_exceeded, got %d %s", rec.Code, rec.Body.String())
	}
}

// REQ-025.10: a workspace-branch commit is pushed to the forge as it happens.
func TestDynamicCommitPushesImmediately(t *testing.T) {
	env := dynamicServer(t, dynManifest)
	session, _ := patLogin(t, env.h, "tok-a")

	rec := patDo(env.h, "POST", "/api/repos/w/workspace", session, "{}")
	var ws struct{ Branch string }
	_ = json.Unmarshal(rec.Body.Bytes(), &ws)
	if rec = patDo(env.h, "PUT", "/api/repos/w/files/notes.md?branch="+ws.Branch, session, `{"content":"# n\n","baseSha":""}`); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	rec = patDo(env.h, "POST", "/api/repos/w/commit?branch="+ws.Branch, session, `{"message":"n"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pushed":true`) {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body.String())
	}
	if out := gitOut(t, "-C", env.mainSrc, "show-ref", ws.Branch); !strings.Contains(out, "refs/heads/"+ws.Branch) {
		t.Fatalf("commit was not pushed to origin: %s", out)
	}
}

// Dynamic endpoints 404 on deployments that did not opt in (REQ-025.1).
func TestDynamicDisabledIsInvisible(t *testing.T) {
	env := patServer(t) // the plain forge-PAT fixture: no dynamic block
	session, _ := patLogin(t, env.h, "tok-a")
	rec := patDo(env.h, "GET", "/api/dynamic", session, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("info: %d %s", rec.Code, rec.Body.String())
	}
	rec = patDo(env.h, "POST", "/api/dynamic/open", session, `{"spec":"acme/dyn"}`)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "dynamic_disabled") {
		t.Fatalf("open on disabled: %d %s", rec.Code, rec.Body.String())
	}
}
