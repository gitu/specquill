package api

import (
	"net/http"
	"testing"
)

// REQ-021.2: landing on a protected branch requires maintainer. An editor
// writes and commits on their own branch but cannot publish to the branch
// everyone reads; an unprotected target merges at editor.
func TestMergeRequiresMaintainerOnProtected(t *testing.T) {
	h, st, _ := testServerFull(t, true) // main protected
	cookie := login(t, h)               // auto-enrolled as editor
	wRepoRow(t, st)

	prep := func(branch, file string) {
		t.Helper()
		if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/branches", map[string]string{"name": branch}); code != http.StatusOK {
			t.Fatalf("branch %s: %d %v", branch, code, out)
		}
		if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/"+file+"?branch="+branch, map[string]string{"content": "x"}); code != http.StatusOK {
			t.Fatalf("save on %s: %d %v", branch, code, out)
		}
		if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/commit?branch="+branch, map[string]any{"message": "c", "paths": []string{file}}); code != http.StatusOK {
			t.Fatalf("commit on %s: %d %v", branch, code, out)
		}
	}

	// editor → protected main: previewing is fine, landing is not
	prep("feat", "a.md")
	if code, out := doJSON(t, h, cookie, "GET", "/api/repos/w/merge?source=feat", nil); code != http.StatusOK {
		t.Fatalf("editor preview: want 200, got %d %v", code, out)
	}
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/merge", map[string]string{"source": "feat"})
	if code != http.StatusForbidden || out["code"] != "role_forbidden" {
		t.Fatalf("editor merge into protected: want 403 role_forbidden, got %d %v", code, out)
	}

	// an unprotected target merges at editor
	prep("side", "b.md")
	if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/merge",
		map[string]string{"source": "side", "target": "feat"}); code != http.StatusOK {
		t.Fatalf("editor merge into unprotected: want 200, got %d %v", code, out)
	}

	// maintainer lands the protected merge
	promoteRole(t, st, "flo@test.local", "maintainer")
	if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/merge", map[string]string{"source": "feat"}); code != http.StatusOK {
		t.Fatalf("maintainer merge into protected: want 200, got %d %v", code, out)
	}
}
