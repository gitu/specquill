package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"specquill/server/internal/config"
	"specquill/server/internal/forge"
)

// Unconfigured projects report the panel as off — the endpoint always answers
// 200 so the UI can render nothing without treating it as a failure.
func TestForgeDisabledByDefault(t *testing.T) {
	h, _, _ := testServerFull(t, false)
	cookie := login(t, h)
	code, out := doJSON(t, h, cookie, "GET", "/api/repos/w/forge/request?branch=main", nil)
	if code != http.StatusOK || out["enabled"] != false {
		t.Fatalf("unconfigured forge: want 200 enabled:false, got %d %v", code, out)
	}
}

// With a forge configured, the branch's open request and its comments come
// back; a forge outage degrades to an error field rather than a failed page.
func TestForgeRequestSurfaced(t *testing.T) {
	var fail bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		switch {
		case hasSuffix(r.URL.Path, "/merge_requests"):
			_, _ = w.Write([]byte(`[{"iid":7,"title":"Venue rules","state":"opened",
				"web_url":"https://gl/mr/7","author":{"username":"rev"}}]`))
		case hasSuffix(r.URL.Path, "/notes"):
			_, _ = w.Write([]byte(`[{"body":"tighten this","system":false,"created_at":"t1",
				"author":{"username":"rev"},"position":{"new_path":"specs/a.md","new_line":3}}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()

	h, _, _ := testServerCfg(t, false, func(c *config.Config) {
		// the fixture remote is a local path, so name the forge project
		// explicitly — the same escape hatch an ssh host alias needs
		c.Repos[0].Forge = forge.Config{
			Kind: forge.KindGitLab, BaseURL: srv.URL, Project: "acme/specs",
		}
	})
	cookie := login(t, h)

	code, out := doJSON(t, h, cookie, "GET", "/api/repos/w/forge/request?branch=main", nil)
	if code != http.StatusOK || out["enabled"] != true {
		t.Fatalf("configured forge: %d %v", code, out)
	}
	req, _ := out["request"].(map[string]any)
	if req == nil || req["title"] != "Venue rules" {
		t.Fatalf("request not surfaced: %v", out)
	}
	comments, _ := req["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("comments not surfaced: %v", req["comments"])
	}

	// a failing forge must not break the endpoint (cached answer clears first)
	fail = true
	code, out = doJSON(t, h, cookie, "GET", "/api/repos/w/forge/request?branch=other", nil)
	if code != http.StatusOK || out["enabled"] != true || out["error"] == nil {
		t.Fatalf("forge outage: want 200 with an error field, got %d %v", code, out)
	}
}

func hasSuffix(s, suf string) bool { return len(s) >= len(suf) && s[len(s)-len(suf):] == suf }
