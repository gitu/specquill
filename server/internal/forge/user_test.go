package forge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGitHubCurrentUserEmailFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/user":
			w.Header().Set("X-OAuth-Scopes", "repo, read:org")
			_, _ = w.Write([]byte(`{"id":77,"login":"flo","name":"","email":null}`))
		case "/user/emails":
			// simulate a token without the email scope
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c, err := New(Config{Kind: KindGitHub, BaseURL: srv.URL}, "https://github.com/acme/specs.git", "tok")
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.CurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.Subject != "77" || u.Login != "flo" || u.Name != "flo" {
		t.Fatalf("identity: %+v", u)
	}
	if u.Email != "77+flo@users.noreply.github.com" {
		t.Fatalf("hidden email should fall back to the noreply address, got %q", u.Email)
	}
	if len(u.Scopes) != 2 || u.Scopes[0] != "repo" {
		t.Fatalf("scopes: %v", u.Scopes)
	}
}

func TestGitLabCurrentUser(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/user" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":5,"username":"flo","name":"Flo","email":"flo@example.com","commit_email":"flo@commits.example.com"}`))
	}))
	defer srv.Close()

	c, err := New(Config{Kind: KindGitLab, BaseURL: srv.URL}, "https://gitlab.example.com/acme/specs.git", "glpat-x")
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.CurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.Subject != "5" || u.Name != "Flo" || u.Email != "flo@commits.example.com" {
		t.Fatalf("identity: %+v", u)
	}
	if gotToken != "glpat-x" {
		t.Fatalf("gitlab should use PRIVATE-TOKEN, got %q", gotToken)
	}
}

func TestCurrentUserRejectsBadToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"401 Unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	c, _ := New(Config{Kind: KindGitLab, BaseURL: srv.URL}, "https://gitlab.example.com/acme/specs.git", "bad")
	if _, err := c.CurrentUser(context.Background()); err == nil {
		t.Fatal("expected an error for a rejected token")
	}
}

func TestProjectRoleMapping(t *testing.T) {
	// github: permission booleans → ladder
	ghCases := []struct{ perms, want string }{
		{`{"admin":true,"maintain":true,"push":true,"pull":true}`, "admin"},
		{`{"admin":false,"maintain":true,"push":true,"pull":true}`, "maintainer"},
		{`{"admin":false,"maintain":false,"push":true,"pull":true}`, "editor"},
		{`{"admin":false,"maintain":false,"push":false,"pull":true}`, "viewer"},
		{`{"admin":false,"maintain":false,"push":false,"pull":false}`, "none"},
	}
	for _, tc := range ghCases {
		perms := tc.perms
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"permissions":` + perms + `}`))
		}))
		c, _ := New(Config{Kind: KindGitHub, BaseURL: srv.URL}, "https://github.com/acme/specs.git", "tok")
		role, err := c.ProjectRole(context.Background())
		srv.Close()
		if err != nil || role != tc.want {
			t.Errorf("github %s → (%q,%v), want %q", tc.perms, role, err, tc.want)
		}
	}

	// gitlab: max(project, group) access level → ladder
	glCases := []struct {
		perms string
		want  string
	}{
		{`{"project_access":{"access_level":50},"group_access":null}`, "admin"},
		{`{"project_access":{"access_level":40},"group_access":null}`, "maintainer"},
		{`{"project_access":{"access_level":30},"group_access":null}`, "editor"},
		{`{"project_access":null,"group_access":{"access_level":30}}`, "editor"},
		{`{"project_access":{"access_level":20},"group_access":null}`, "viewer"},
		{`{"project_access":null,"group_access":null}`, "none"},
	}
	for _, tc := range glCases {
		perms := tc.perms
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"permissions":` + perms + `}`))
		}))
		c, _ := New(Config{Kind: KindGitLab, BaseURL: srv.URL}, "https://gitlab.example.com/acme/specs.git", "tok")
		role, err := c.ProjectRole(context.Background())
		srv.Close()
		if err != nil || role != tc.want {
			t.Errorf("gitlab %s → (%q,%v), want %q", tc.perms, role, err, tc.want)
		}
	}
}

func TestGitLabCreateRequest(t *testing.T) {
	var created bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/merge_requests"):
			_, _ = w.Write([]byte(`[]`)) // nothing open yet
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/merge_requests"):
			created = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"iid":7,"title":"Venue rules","state":"opened",
				"web_url":"https://gitlab.example.com/acme/specs/-/merge_requests/7","author":{"username":"flo"}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Kind: KindGitLab, BaseURL: srv.URL}, "https://gitlab.example.com/acme/specs.git", "tok")
	req, isNew, err := c.CreateRequest(context.Background(), "ws/flo", "main", "Venue rules", "please review")
	if err != nil || !isNew || !created {
		t.Fatalf("CreateRequest: %v new=%v created=%v", err, isNew, created)
	}
	if req.Number != 7 || req.URL == "" {
		t.Fatalf("request: %+v", req)
	}
}

func TestCreateRequestReusesOpenOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			t.Errorf("must not POST when a request is already open")
		case strings.Contains(r.URL.Path, "/notes"):
			_, _ = w.Write([]byte(`[]`))
		default:
			_, _ = w.Write([]byte(`[{"iid":7,"title":"Venue rules","state":"opened",
				"web_url":"u","author":{"username":"flo"}}]`))
		}
	}))
	defer srv.Close()

	c, _ := New(Config{Kind: KindGitLab, BaseURL: srv.URL}, "https://gitlab.example.com/acme/specs.git", "tok")
	req, isNew, err := c.CreateRequest(context.Background(), "ws/flo", "main", "", "")
	if err != nil || isNew {
		t.Fatalf("want re-used request, got new=%v err=%v", isNew, err)
	}
	if req.Number != 7 {
		t.Fatalf("request: %+v", req)
	}
}
