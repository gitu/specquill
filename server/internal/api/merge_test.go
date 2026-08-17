package api

import (
	"net/http"
	"net/url"
	"testing"
)

// Direct merge replaces the PR flow: a member lands a workspace branch on the
// protected default branch, uncommitted work is refused rather than silently
// dropped, and viewers can preview but not merge.
func TestDirectMerge(t *testing.T) {
	h, st, git := testServerFull(t, true) // main protected
	cookie := login(t, h)
	wRepoRow(t, st)
	promoteRole(t, st, "flo@test.local", "maintainer") // landing on main

	repo, ok := git.Repo("w")
	if !ok {
		t.Fatal("fixture repo missing")
	}
	if err := repo.CreateBranch("ws/flo", "main"); err != nil {
		t.Fatal(err)
	}

	// main itself stays unwritable — the merge below is the only way it moves
	code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/specs/a.md?branch=main",
		map[string]string{"content": "direct", "baseSha": ""})
	if code != http.StatusForbidden || out["code"] != "protected_branch" {
		t.Fatalf("write to main: want 403 protected_branch, got %d %v", code, out)
	}

	// uncommitted work on the source is refused with a commit-first signal
	if code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/specs/a.md?branch=ws/flo",
		map[string]string{"content": "hello\n", "baseSha": ""}); code != http.StatusOK {
		t.Fatalf("workspace write: %d %v", code, out)
	}
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/merge", map[string]string{"source": "ws/flo"})
	if code != http.StatusConflict || out["code"] != "dirty" {
		t.Fatalf("merge with dirty worktree: want 409 dirty, got %d %v", code, out)
	}

	// commit, then preview reports the pending file as mergeable
	if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/commit?branch=ws/flo",
		map[string]string{"message": "add a"}); code != http.StatusOK {
		t.Fatalf("commit: %d %v", code, out)
	}
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/w/merge?source=ws/flo", nil)
	if code != http.StatusOK || out["mergeable"] != true {
		t.Fatalf("merge preview: want mergeable, got %d %v", code, out)
	}
	if files, _ := out["files"].([]any); len(files) != 1 {
		t.Fatalf("preview should list the one changed file: %v", out["files"])
	}

	// merging lands it on main
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/merge", map[string]string{"source": "ws/flo"})
	if code != http.StatusOK || out["mergedCommit"] == "" {
		t.Fatalf("merge: %d %v", code, out)
	}
	code, tree := doJSONList(t, h, cookie, "GET", "/api/repos/w/tree?ref=main")
	if code != http.StatusOK || !contains(tree, "specs/a.md") {
		t.Fatalf("merged file missing from main: %d %v", code, tree)
	}
	// ...and the workspace resets onto the new head, so it stays reusable
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/w/merge?source=ws/flo", nil)
	if code != http.StatusOK {
		t.Fatalf("post-merge preview: %d %v", code, out)
	}
	// must be [] and never null — the SPA indexes into it directly
	files, isArray := out["files"].([]any)
	if !isArray {
		t.Fatalf("files must be a JSON array even when empty, got %#v", out["files"])
	}
	if len(files) != 0 {
		t.Fatalf("workspace should be level with main after merge: %v", files)
	}

	// an unrecognised strategy is refused rather than silently merging
	for _, bad := range []string{"sqaush", "rebase", "SQUASH"} {
		if code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/merge",
			map[string]string{"source": "ws/flo", "strategy": bad}); code != http.StatusBadRequest {
			t.Fatalf("strategy %q: want 400, got %d %v", bad, code, out)
		}
	}

	// bad requests
	if code, _ := doJSON(t, h, cookie, "POST", "/api/repos/w/merge", map[string]string{"source": "main"}); code != http.StatusBadRequest {
		t.Fatalf("merging main into itself: want 400, got %d", code)
	}
	if code, _ := doJSON(t, h, cookie, "POST", "/api/repos/w/merge", map[string]string{"source": "ws/nope"}); code != http.StatusNotFound {
		t.Fatalf("unknown branch: want 404, got %d", code)
	}

	// branch names reach git argv and worktree paths — refuse the dangerous
	// shapes here rather than trusting git's refname rules downstream
	for _, bad := range []string{"--output=/tmp/pwn", "..", "ws/../../etc", "ws/flo:x"} {
		code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/merge", map[string]string{"source": bad})
		if code != http.StatusBadRequest {
			t.Fatalf("merge source %q: want 400, got %d %v", bad, code, out)
		}
		code, out = doJSON(t, h, cookie, "GET", "/api/repos/w/merge?source="+url.QueryEscape(bad), nil)
		if code != http.StatusBadRequest {
			t.Fatalf("preview source %q: want 400, got %d %v", bad, code, out)
		}
	}
}

// Viewers may see what a merge would do, but not perform one.
func TestMergeIsMemberGated(t *testing.T) {
	h, st, git := testServerFull(t, true)
	cookie := login(t, h)
	wRepoRow(t, st)
	if repo, ok := git.Repo("w"); ok {
		if err := repo.CreateBranch("ws/flo", "main"); err != nil {
			t.Fatal(err)
		}
	}
	promoteRole(t, st, "flo@test.local", "viewer")
	if code, _ := doJSON(t, h, cookie, "GET", "/api/repos/w/merge?source=ws/flo", nil); code != http.StatusOK {
		t.Fatalf("viewer preview: want 200, got %d", code)
	}
	code, out := doJSON(t, h, cookie, "POST", "/api/repos/w/merge", map[string]string{"source": "ws/flo"})
	if code != http.StatusForbidden || out["code"] != "role_forbidden" {
		t.Fatalf("viewer merge: want 403 role_forbidden, got %d %v", code, out)
	}
}
