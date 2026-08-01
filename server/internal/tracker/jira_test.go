package tracker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJiraCreateIssueBasicAuth(t *testing.T) {
	var gotAuth string
	var gotFields map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/issue" {
			t.Errorf("unexpected path %s (v2, not v3 — plain-text description)", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Fields map[string]any `json:"fields"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotFields = body.Fields
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"SPEC-42"}`))
	}))
	defer srv.Close()

	j := NewJira(srv.URL, "SPEC", "", "flo@example.com:api-token")
	key, url, err := j.CreateIssue(context.Background(), "[drift] t", "desc", []string{"specquill-drift", "with space"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "SPEC-42" || url != srv.URL+"/browse/SPEC-42" {
		t.Fatalf("got (%q, %q)", key, url)
	}
	// email:token → HTTP Basic (Jira Cloud, importer convention)
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("flo@example.com:api-token"))
	if gotAuth != want {
		t.Fatalf("auth = %q, want Basic", gotAuth)
	}
	if gotFields["summary"] != "[drift] t" || gotFields["description"] != "desc" {
		t.Fatalf("unexpected fields: %v", gotFields)
	}
	if it, _ := gotFields["issuetype"].(map[string]any); it["name"] != "Task" {
		t.Fatalf("issue type must default to Task, got %v", gotFields["issuetype"])
	}
	labels, _ := gotFields["labels"].([]any)
	if len(labels) != 2 || labels[1] != "with-space" {
		t.Fatalf("labels must be space-free, got %v", labels)
	}
}

func TestJiraBearerAuthAndFind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// bare token → Bearer (Jira Server/DC PAT)
		if r.Header.Get("Authorization") != "Bearer pat-token" {
			t.Errorf("auth = %q, want Bearer", r.Header.Get("Authorization"))
		}
		if !strings.HasPrefix(r.URL.Path, "/rest/api/2/search") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		jql := r.URL.Query().Get("jql")
		if !strings.Contains(jql, "specquill-drift") || !strings.Contains(jql, "abc123") {
			t.Errorf("jql must scope by label and marker, got %q", jql)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[{"key":"SPEC-7"}]}`))
	}))
	defer srv.Close()

	j := NewJira(srv.URL, "SPEC", "Bug", "pat-token")
	key, url, err := j.FindIssue(context.Background(), "specquill-drift", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if key != "SPEC-7" || url != srv.URL+"/browse/SPEC-7" {
		t.Fatalf("got (%q, %q)", key, url)
	}
}
