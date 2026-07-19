package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Raw blobs are member-authored repo content served same-origin — without a
// sandbox CSP, navigating straight to an uploaded SVG would execute its
// scripts with the viewer's session cookie.
func TestRawResponsesSandboxed(t *testing.T) {
	h, _, _ := testGroundingServer(t)
	cookie := login(t, h)

	req := httptest.NewRequest("GET", apiURL("/api/repos/w/raw/.specquill/config.yml"), nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("raw read: %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "sandbox" {
		t.Fatalf("raw response missing sandbox CSP, got %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("raw response missing nosniff, got %q", got)
	}
}
