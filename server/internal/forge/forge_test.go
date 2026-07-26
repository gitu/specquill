package forge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseRemote(t *testing.T) {
	cases := []struct{ remote, host, path string }{
		{"https://github.com/acme/specs.git", "github.com", "acme/specs"},
		{"https://gitlab.example.com/group/sub/specs.git", "gitlab.example.com", "group/sub/specs"},
		{"git@github.com:acme/specs.git", "github.com", "acme/specs"},
		{"ssh://git@gitlab.example.com:2222/group/specs.git", "gitlab.example.com", "group/specs"},
		{"https://gitlab.com/acme/specs/", "gitlab.com", "acme/specs"},
	}
	for _, c := range cases {
		host, path, err := parseRemote(c.remote)
		if err != nil || host != c.host || path != c.path {
			t.Errorf("%s → (%q,%q,%v), want (%q,%q)", c.remote, host, path, err, c.host, c.path)
		}
	}
	// local paths have no forge behind them
	for _, bad := range []string{"./data/origin/specs.git", "/srv/specs.git", ""} {
		if _, _, err := parseRemote(bad); err == nil {
			t.Errorf("%q should not parse as a forge remote", bad)
		}
	}
}

func TestDisabledConfigYieldsNoClient(t *testing.T) {
	c, err := New(Config{}, "https://github.com/acme/specs.git", "")
	if err != nil || c != nil {
		t.Fatalf("disabled config: want (nil,nil), got (%v,%v)", c, err)
	}
	// a nil client answers "no open request" rather than panicking
	if req, err := c.OpenRequest(context.Background(), "ws/flo"); req != nil || err != nil {
		t.Fatalf("nil client: want (nil,nil), got (%v,%v)", req, err)
	}
}

func TestGitHubOpenRequest(t *testing.T) {
	var gotAuth, gotHead string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			gotHead = r.URL.Query().Get("head")
			_, _ = w.Write([]byte(`[{"number":12,"title":"Venue rules","state":"open",
				"html_url":"https://github.com/acme/specs/pull/12","user":{"login":"rev"}}]`))
		case strings.HasSuffix(r.URL.Path, "/pulls/12/comments"):
			_, _ = w.Write([]byte(`[{"body":"tighten this","path":"specs/venue.md","line":14,
				"created_at":"2026-07-25T10:00:00Z","html_url":"u1","user":{"login":"rev"}}]`))
		case strings.HasSuffix(r.URL.Path, "/issues/12/comments"):
			_, _ = w.Write([]byte(`[{"body":"looks good otherwise","created_at":"2026-07-25T10:05:00Z",
				"html_url":"u2","user":{"login":"boss"}}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c, err := New(Config{Kind: KindGitHub, BaseURL: srv.URL}, "https://github.com/acme/specs.git", "tok")
	if err != nil {
		t.Fatal(err)
	}
	req, err := c.OpenRequest(context.Background(), "ws/flo")
	if err != nil || req == nil {
		t.Fatalf("OpenRequest: %v %v", req, err)
	}
	if req.Number != 12 || req.Title != "Venue rules" || req.Author != "rev" {
		t.Fatalf("request: %+v", req)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("github should use a bearer token, got %q", gotAuth)
	}
	if gotHead != "acme:ws/flo" { // owner-qualified, else the filter is ignored
		t.Fatalf("head filter: %q", gotHead)
	}
	if len(req.Comments) != 2 {
		t.Fatalf("want review + issue comments, got %+v", req.Comments)
	}
	if c0 := req.Comments[0]; c0.Path != "specs/venue.md" || c0.Line != 14 {
		t.Fatalf("anchored comment lost its position: %+v", c0)
	}
	if c1 := req.Comments[1]; c1.Path != "" || c1.Line != 0 {
		t.Fatalf("general comment should carry no position: %+v", c1)
	}
}

func TestGitLabOpenRequest(t *testing.T) {
	var gotToken, gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge_requests"):
			gotProject = strings.TrimSuffix(strings.TrimPrefix(r.URL.EscapedPath(), "/projects/"), "/merge_requests")
			_, _ = w.Write([]byte(`[{"iid":7,"title":"Venue rules","state":"opened",
				"web_url":"https://gitlab.example.com/g/s/specs/-/merge_requests/7","author":{"username":"rev"}}]`))
		case strings.HasSuffix(r.URL.Path, "/notes"):
			_, _ = w.Write([]byte(`[
				{"body":"changed title","system":true,"created_at":"t0","author":{"username":"rev"}},
				{"body":"tighten this","system":false,"created_at":"t1","author":{"username":"rev"},
				 "position":{"new_path":"specs/venue.md","new_line":14}},
				{"body":"looks good","system":false,"created_at":"t2","author":{"username":"boss"}}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c, err := New(Config{Kind: KindGitLab, BaseURL: srv.URL}, "https://gitlab.example.com/g/s/specs.git", "tok")
	if err != nil {
		t.Fatal(err)
	}
	req, err := c.OpenRequest(context.Background(), "ws/flo")
	if err != nil || req == nil {
		t.Fatalf("OpenRequest: %v %v", req, err)
	}
	if gotToken != "tok" {
		t.Fatalf("gitlab should use PRIVATE-TOKEN, got %q", gotToken)
	}
	// nested groups must be URL-encoded into a single path segment
	if gotProject != "g%2Fs%2Fspecs" {
		t.Fatalf("nested project path not encoded: %q", gotProject)
	}
	if req.Number != 7 || req.Author != "rev" {
		t.Fatalf("request: %+v", req)
	}
	if len(req.Comments) != 2 {
		t.Fatalf("system notes must be filtered out, got %+v", req.Comments)
	}
	if c0 := req.Comments[0]; c0.Path != "specs/venue.md" || c0.Line != 14 {
		t.Fatalf("positioned note lost its anchor: %+v", c0)
	}
}

func TestNoOpenRequestIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c, _ := New(Config{Kind: KindGitHub, BaseURL: srv.URL}, "https://github.com/acme/specs.git", "")
	req, err := c.OpenRequest(context.Background(), "ws/flo")
	if req != nil || err != nil {
		t.Fatalf("branch with no MR: want (nil,nil), got (%v,%v)", req, err)
	}
}

func TestForgeErrorsSurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()
	c, _ := New(Config{Kind: KindGitHub, BaseURL: srv.URL}, "https://github.com/acme/specs.git", "bad")
	_, err := c.OpenRequest(context.Background(), "ws/flo")
	if err == nil || !strings.Contains(err.Error(), "Bad credentials") {
		t.Fatalf("want the forge's own message, got %v", err)
	}
	if strings.Contains(err.Error(), "bad") { // never echo the token
		t.Fatalf("error leaked the token: %v", err)
	}
}
