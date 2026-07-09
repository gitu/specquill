package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": "flo", "password": "hunter2secret"})
	req := httptest.NewRequest("POST", "/auth/local/login", bytes.NewReader(body))
	req.Header.Set("X-SpecQuill", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "specquill_session" {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func doJSON(t *testing.T, h http.Handler, cookie *http.Cookie, method, url string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, url, &buf)
	req.Header.Set("X-SpecQuill", "1")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestProtectedMainAndWorkspaceFlow(t *testing.T) {
	h := testServerProtected(t)
	cookie := login(t, h)

	// direct write to protected main → 403 with machine-readable code
	code, out := doJSON(t, h, cookie, "PUT", "/api/repos/w/files/specs/a.md?branch=main",
		map[string]string{"content": "x", "baseSha": ""})
	if code != http.StatusForbidden || out["code"] != "protected_branch" {
		t.Fatalf("want 403 protected_branch, got %d %v", code, out)
	}

	// commit refused too
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/commit?branch=main", map[string]string{"message": "m"})
	if code != http.StatusForbidden || out["code"] != "protected_branch" {
		t.Fatalf("commit: want 403, got %d %v", code, out)
	}

	// workspace resolution: slug from local username
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/workspace", nil)
	if code != http.StatusOK || out["branch"] != "ws/flo" || out["created"] != true {
		t.Fatalf("workspace: got %d %v", code, out)
	}
	// idempotent
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/workspace", nil)
	if code != http.StatusOK || out["created"] != false || out["state"] != "current" {
		t.Fatalf("workspace reuse: got %d %v", code, out)
	}

	// writes on the workspace succeed
	code, _ = doJSON(t, h, cookie, "PUT", "/api/repos/w/files/specs/a.md?branch=ws/flo",
		map[string]string{"content": "# edited\n", "baseSha": shaOf(t, h, cookie, "/api/repos/w/files/specs/a.md?ref=ws/flo")})
	if code != http.StatusOK {
		t.Fatalf("ws write: got %d", code)
	}

	// worktree diff endpoint sees it
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/w/diff/worktree?branch=ws/flo", nil)
	files := out["files"].([]any)
	if code != http.StatusOK || len(files) != 1 {
		t.Fatalf("worktree diff: %d %v", code, out)
	}
}

func shaOf(t *testing.T, h http.Handler, cookie *http.Cookie, url string) string {
	t.Helper()
	_, out := doJSON(t, h, cookie, "GET", url, nil)
	sha, _ := out["sha"].(string)
	return sha
}
