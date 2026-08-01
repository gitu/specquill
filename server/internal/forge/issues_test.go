package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubCreateIssue(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/specs/issues" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number":7,"title":"[drift] t","state":"open","html_url":"https://github.com/acme/specs/issues/7"}`))
	}))
	defer srv.Close()
	c, err := New(Config{Kind: KindGitHub, BaseURL: srv.URL, Project: "acme/specs"}, "", "tok")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := c.CreateIssue(context.Background(), "[drift] t", "body <!-- specquill:drift:abc -->", []string{DriftLabel})
	if err != nil {
		t.Fatal(err)
	}
	if issue.Number != 7 || issue.URL != "https://github.com/acme/specs/issues/7" {
		t.Fatalf("unexpected issue: %+v", issue)
	}
	if gotBody["title"] != "[drift] t" || gotBody["body"] == "" {
		t.Fatalf("unexpected payload: %v", gotBody)
	}
	labels, _ := gotBody["labels"].([]any)
	if len(labels) != 1 || labels[0] != DriftLabel {
		t.Fatalf("unexpected labels: %v", gotBody["labels"])
	}
}

func TestGitLabCreateIssue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// nested group paths stay URL-encoded whole
		if r.URL.EscapedPath() != "/projects/group%2Fsub%2Fspecs/issues" {
			t.Errorf("unexpected path %s", r.URL.EscapedPath())
		}
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			t.Errorf("gitlab must authenticate with PRIVATE-TOKEN")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["labels"] != DriftLabel {
			t.Errorf("gitlab labels must be comma-joined, got %v", body["labels"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"iid":3,"title":"[drift] t","state":"opened","web_url":"https://gitlab.example.com/group/sub/specs/-/issues/3"}`))
	}))
	defer srv.Close()
	c, err := New(Config{Kind: KindGitLab, BaseURL: srv.URL, Project: "group/sub/specs"}, "", "tok")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := c.CreateIssue(context.Background(), "[drift] t", "body", []string{DriftLabel})
	if err != nil {
		t.Fatal(err)
	}
	if issue.Number != 3 || !strings.Contains(issue.URL, "/-/issues/3") {
		t.Fatalf("unexpected issue: %+v", issue)
	}
}

func TestFindIssueByMarkerIdempotency(t *testing.T) {
	marker := "<!-- specquill:drift:abc123 -->"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("labels") != DriftLabel {
			t.Errorf("marker search must scope to the drift label, got %q", r.URL.Query().Get("labels"))
		}
		w.Header().Set("Content-Type", "application/json")
		// one PR (must be skipped), one issue without the marker, one with
		_, _ = w.Write([]byte(`[
			{"number":1,"body":"` + marker + `","html_url":"pr","pull_request":{}},
			{"number":2,"body":"other","html_url":"other"},
			{"number":3,"title":"hit","state":"closed","body":"text ` + marker + ` text","html_url":"https://github.com/acme/specs/issues/3"}
		]`))
	}))
	defer srv.Close()
	c, err := New(Config{Kind: KindGitHub, BaseURL: srv.URL, Project: "acme/specs"}, "", "tok")
	if err != nil {
		t.Fatal(err)
	}
	issue, err := c.FindIssueByMarker(context.Background(), marker)
	if err != nil {
		t.Fatal(err)
	}
	if issue == nil || issue.Number != 3 {
		t.Fatalf("want issue 3, got %+v", issue)
	}
	if miss, err := c.FindIssueByMarker(context.Background(), "<!-- specquill:drift:zzz -->"); err != nil || miss != nil {
		t.Fatalf("want no match, got (%+v, %v)", miss, err)
	}
}
