package api

import (
	"net/http"
	"testing"
)

// REQ-021.2: merging into a protected branch requires maintainer; an editor
// opens and approves PRs but cannot land them on main. A PR onto an
// unprotected branch merges at editor.
func TestMergeRequiresMaintainerOnProtected(t *testing.T) {
	h, st, _ := testServerFull(t, true) // main protected
	cookie := login(t, h)              // auto-enrolled as editor
	wRepoRow(t, st)

	prep := func(branch, file string) float64 {
		if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/branches", map[string]string{"name": branch}); code != http.StatusOK {
			t.Fatalf("branch %s: %d %v", branch, code, out)
		}
		if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/"+file+"?branch="+branch, map[string]string{"content": "x"}); code != http.StatusOK {
			t.Fatalf("save on %s: %d %v", branch, code, out)
		}
		if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/commit?branch="+branch, map[string]any{"message": "c", "paths": []string{file}}); code != http.StatusOK {
			t.Fatalf("commit on %s: %d %v", branch, code, out)
		}
		return 0
	}

	// editor → PR onto protected main: open yes, merge no
	prep("feat", "a.md")
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/prs", map[string]string{"title": "t", "source": "feat", "target": "main"})
	if code != http.StatusOK {
		t.Fatalf("editor PR create: %d %v", code, out)
	}
	n := jsonStr(out["number"])
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/prs/"+n+"/merge", nil)
	if code != http.StatusForbidden || out["code"] != "role_forbidden" {
		t.Fatalf("editor merge into protected: want 403 role_forbidden, got %d %v", code, out)
	}

	// an unprotected target merges at editor
	prep("side", "b.md")
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/prs", map[string]string{"title": "t2", "source": "side", "target": "feat"})
	if code != http.StatusOK {
		t.Fatalf("editor PR create (unprotected): %d %v", code, out)
	}
	n2 := jsonStr(out["number"])
	if code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/prs/"+n2+"/merge", nil); code != http.StatusOK {
		t.Fatalf("editor merge into unprotected: want 200, got %d %v", code, out)
	}

	// maintainer lands the protected merge
	promoteTenantRole(t, st, "flo@test.local", "maintainer")
	if code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/prs/"+n+"/merge", nil); code != http.StatusOK {
		t.Fatalf("maintainer merge into protected: want 200, got %d %v", code, out)
	}
}
