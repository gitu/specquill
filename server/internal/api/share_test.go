package api

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Share links: minting needs a session, downloading needs ONLY the token in
// the URL (the LLM copy-paste use case). Rotation and revocation kill old
// links immediately. OKF-opted bundles get a log.md generated on the fly.
func TestShareLinkLifecycle(t *testing.T) {
	h, st, git := testGroundingServer(t)
	cookie := login(t, h)
	// minting is maintainer-gated (REQ-021) — promote the enrolled editor
	promoteRole(t, st, "flo@test.local", "maintainer")

	// no link yet
	code, out := doJSON(t, h, cookie, "GET", "/api/repos/w/share", nil)
	if code != http.StatusOK || out["url"] != nil {
		t.Fatalf("expected empty share state, got %d %v", code, out)
	}

	// mint
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/share", nil)
	url, _ := out["url"].(string)
	if code != http.StatusOK || !strings.HasPrefix(url, "/share/") || !strings.HasSuffix(url, "w-okf.zip") {
		t.Fatalf("mint failed: %d %v", code, out)
	}

	// download WITHOUT any session cookie or CSRF header
	download := func(u string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
		return rec
	}
	rec := download(url)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("download: %d %s %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names[".specquill/config.yml"] {
		t.Fatalf("bundle missing workspace files, got %v", names)
	}
	// no OKF opt-in → no log.md invented
	if names["log.md"] {
		t.Fatal("log.md in bundle without OKF opt-in")
	}

	// opt into OKF and commit; the bundle now carries a log.md generated on
	// the fly from git history — nothing is materialized in the repo
	repo, _ := git.Repo("w")
	if _, err := repo.SaveFile("main", "index.md", "---\nokf_version: \"0.1\"\n---\n\n# Index\n", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit("main", "adopt OKF", "Jane", "jane@t", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.File("main", "log.md"); err == nil {
		t.Fatal("log.md materialized in the repo")
	}
	rec = download(url)
	zr, err = zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	logMd := ""
	for _, f := range zr.File {
		if f.Name == "log.md" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			logMd = string(b)
		}
	}
	if !strings.Contains(logMd, "# Log") || !strings.Contains(logMd, "adopt OKF (Jane)") {
		t.Fatalf("bundle log.md not generated on the fly:\n%q", logMd)
	}

	// unknown token → 404
	if rec := download("/share/deadbeef/w-okf.zip"); rec.Code != http.StatusNotFound {
		t.Fatalf("bogus token: want 404, got %d", rec.Code)
	}

	// rotate → new token, old link dead
	code, out = doJSON(t, h, cookie, "POST", "/api/repos/w/share", nil)
	url2, _ := out["url"].(string)
	if code != http.StatusOK || url2 == url {
		t.Fatalf("rotate did not change token: %d %v", code, out)
	}
	if rec := download(url); rec.Code != http.StatusNotFound {
		t.Fatalf("old link survived rotation: %d", rec.Code)
	}
	if rec := download(url2); rec.Code != http.StatusOK {
		t.Fatalf("rotated link broken: %d", rec.Code)
	}

	// revoke → link dead, state empty
	if code, _ := doJSON(t, h, cookie, "DELETE", "/api/repos/w/share", nil); code != http.StatusOK {
		t.Fatalf("revoke: %d", code)
	}
	if rec := download(url2); rec.Code != http.StatusNotFound {
		t.Fatalf("revoked link survived: %d", rec.Code)
	}
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/w/share", nil)
	if code != http.StatusOK || out["url"] != nil {
		t.Fatalf("share state not cleared: %d %v", code, out)
	}
}
