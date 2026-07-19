package api

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"

	"specquill/server/internal/config"
	"specquill/server/internal/store"
)

// Stage-4 role gating + runtime project management: members cannot manage
// projects; admins can create/switch-to/delete them, and config-managed
// projects refuse deletion.
func TestProjectManagementAndRoles(t *testing.T) {
	h, st, git := testServerFull(t, false)
	cookie := login(t, h)

	// the login helper's user auto-enrolled as member → management is 403
	code, out := doJSON(t, h, cookie, "POST", "/api/projects", map[string]string{"id": "p2", "remote": "x"})
	if code != http.StatusForbidden || out["code"] != "role_forbidden" {
		t.Fatalf("member create: want 403 role_forbidden, got %d %v", code, out)
	}

	// promote to admin
	u, err := st.UpsertUser("local", "flo", "Flo Test", "flo@test.local")
	if err != nil {
		t.Fatal(err)
	}
	ten, err := st.TenantBySlug("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMemberRole(ten.ID, u.ID, "admin"); err != nil {
		t.Fatal(err)
	}

	// second fixture remote
	src := filepath.Join(t.TempDir(), "p2-src")
	for _, args := range [][]string{
		{"init", "-b", "main", src},
		{"-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "init"},
	} {
		if o, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, o)
		}
	}

	code, out = doJSON(t, h, cookie, "POST", "/api/projects", map[string]string{"id": "p2", "remote": src})
	if code != http.StatusOK {
		t.Fatalf("admin create: %d %v", code, out)
	}
	if _, ok := git.Repo("default/p2"); !ok {
		t.Fatal("runtime AddRepo did not register the clone")
	}
	// project is immediately usable through the normal routes
	code, _ = doJSON(t, h, cookie, "GET", "/api/repos/p2/branches", nil)
	if code != http.StatusOK {
		t.Fatalf("new project branches: %d", code)
	}

	// config-managed projects refuse deletion
	if err := st.SyncTenantProjects(ten.ID, []store.Project{{ProjectID: "w", RepoID: "w"}}); err != nil {
		t.Fatal(err)
	}
	code, out = doJSON(t, h, cookie, "DELETE", "/api/projects/w", nil)
	if code != http.StatusConflict {
		t.Fatalf("delete config project: want 409, got %d %v", code, out)
	}

	// api-managed deletes
	code, _ = doJSON(t, h, cookie, "DELETE", "/api/projects/p2", nil)
	if code != http.StatusOK {
		t.Fatalf("delete api project: %d", code)
	}
	if _, ok := git.Repo("default/p2"); ok {
		t.Fatal("RemoveRepo did not unregister")
	}
	code, _ = doJSON(t, h, cookie, "GET", "/api/repos/p2/branches", nil)
	if code != http.StatusNotFound {
		t.Fatalf("deleted project still resolves: %d", code)
	}
}

// Stage-2 gate: browsing a source requires a grant; revoking hides it.
func TestSourceBrowsingRequiresGrant(t *testing.T) {
	h, st, git := testServerFull(t, false)
	cookie := login(t, h)
	ten, err := st.TenantBySlug("default")
	if err != nil {
		t.Fatal(err)
	}

	// a readonly source repo, registered but NOT granted
	src := filepath.Join(t.TempDir(), "reg-src")
	for _, args := range [][]string{
		{"init", "-b", "main", src},
		{"-C", src, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "init"},
	} {
		if o, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, o)
		}
	}
	if _, err := git.AddRepo("default", config.RepoConfig{ID: "reg", Mode: config.ReadOnly, Remote: src, DefaultBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SyncGlobalSources([]store.Source{{Name: "reg", Kind: "git", Remote: src, DefaultBranch: "main", SyncInterval: 300}}); err != nil {
		t.Fatal(err)
	}

	// ungranted: browse 403, hidden from /api/repos
	code, out := doJSON(t, h, cookie, "GET", "/api/repos/reg/tree", nil)
	if code != http.StatusForbidden || out["code"] != "source_forbidden" {
		t.Fatalf("ungranted browse: want 403 source_forbidden, got %d %v", code, out)
	}

	// grant → browse works
	srcRow, err := st.SourceByName(ten.ID, "reg")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.GrantSource(ten.ID, srcRow.ID, 0); err != nil {
		t.Fatal(err)
	}
	code, _ = doJSON(t, h, cookie, "GET", "/api/repos/reg/tree", nil)
	if code != http.StatusOK {
		t.Fatalf("granted browse: %d", code)
	}
	// writes on a source are always refused
	code, _ = doJSON(t, h, cookie, "PUT", "/api/repos/reg/files/x.md?branch=main", map[string]string{"content": "x", "baseSha": ""})
	if code != http.StatusForbidden {
		t.Fatalf("source write: want 403, got %d", code)
	}

	// revoke → gone again
	if err := st.RevokeGrant(ten.ID, srcRow.ID); err != nil {
		t.Fatal(err)
	}
	code, _ = doJSON(t, h, cookie, "GET", "/api/repos/reg/tree", nil)
	if code != http.StatusForbidden {
		t.Fatalf("revoked browse: want 403, got %d", code)
	}
}
