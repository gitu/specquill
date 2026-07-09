package importer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportURLsReducesHTMLToText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/policy.html":
			w.Write([]byte(`<html><body><h1>Retention</h1><p>Records kept for <b>7 years</b>.</p></body></html>`))
		case "/notes.md":
			w.Write([]byte("# Notes\n\nPlain markdown stays intact."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	files, err := importURLs(context.Background(), srv.Client(), Source{
		Name: "docs", Kind: "url", URLs: []string{srv.URL + "/policy.html", srv.URL + "/notes.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 { // 2 pages + index.md
		t.Fatalf("want 3 files, got %d: %v", len(files), keys(files))
	}
	policy := files["pages/policy.md"]
	if !strings.Contains(policy, "# Retention") || !strings.Contains(policy, "7 years") {
		t.Fatalf("html not reduced to text: %q", policy)
	}
	if strings.Contains(policy, "<b>") || strings.Contains(policy, "<p>") {
		t.Fatalf("markup leaked into output: %q", policy)
	}
	if !strings.Contains(files["pages/notes.md"], "Plain markdown stays intact") {
		t.Fatalf("markdown mangled: %q", files["pages/notes.md"])
	}
	if !strings.Contains(files["index.md"], "/policy.html") {
		t.Fatalf("index missing source url: %q", files["index.md"])
	}
}

func TestImportOpenAPISummarizes(t *testing.T) {
	spec := `{
	  "openapi": "3.0.0",
	  "info": {"title": "Trade API", "version": "2.1"},
	  "paths": {
	    "/trades": {"get": {"summary": "List trades"}, "post": {"operationId": "createTrade"}},
	    "/trades/{id}": {"get": {"summary": "Get a trade"}}
	  },
	  "components": {"schemas": {"Trade": {}, "Execution": {}}}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(spec))
	}))
	defer srv.Close()

	files, err := importOpenAPI(context.Background(), srv.Client(), Source{Name: "trade-api", Kind: "openapi", Remote: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if files["openapi.yaml"] == "" {
		t.Fatal("raw spec not stored")
	}
	idx := files["index.md"]
	for _, want := range []string{"# Trade API", "**Version:** 2.1", "`GET /trades` — List trades", "`POST /trades`", "`GET /trades/{id}` — Get a trade", "- Trade", "- Execution"} {
		if !strings.Contains(idx, want) {
			t.Fatalf("index.md missing %q\n---\n%s", want, idx)
		}
	}
}

func TestImportOpenAPIRejectsNonSpec(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"hello": "world"}`))
	}))
	defer srv.Close()
	if _, err := importOpenAPI(context.Background(), srv.Client(), Source{Remote: srv.URL}); err == nil {
		t.Fatal("expected non-OpenAPI document to be rejected")
	}
}

func TestImportConfluencePaginatesAndConverts(t *testing.T) {
	var gotAuth string
	page := func(id, title, body string) map[string]any {
		return map[string]any{"id": id, "title": title,
			"body":    map[string]any{"storage": map[string]any{"value": body}},
			"version": map[string]any{"number": 3}}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		start := r.URL.Query().Get("start")
		results := []map[string]any{}
		if start == "0" {
			// exactly `limit` (50) results forces a second page fetch
			for i := 0; i < 50; i++ {
				results = append(results, page("p"+string(rune('a'+i%26)), "Page "+start, "<p>body</p>"))
			}
			results[0] = page("mifid", "MiFID II", "<h2>Timestamps</h2><p>Microsecond precision.</p>")
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer srv.Close()

	files, err := importConfluence(context.Background(), srv.Client(), Source{
		Name: "wiki", Kind: "confluence", Remote: srv.URL, Space: "REG", Token: "user@x.com:secrettoken",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Fatalf("email:token should use Basic auth, got %q", gotAuth)
	}
	mifid := files["pages/mifid-ii.md"]
	if !strings.Contains(mifid, "# MiFID II") || !strings.Contains(mifid, "## Timestamps") || !strings.Contains(mifid, "Microsecond precision.") {
		t.Fatalf("confluence page not converted: %q", mifid)
	}
	if !strings.Contains(files["index.md"], "[MiFID II](pages/mifid-ii.md)") {
		t.Fatalf("index missing page link: %q", files["index.md"])
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
