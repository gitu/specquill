package api

import (
	"net/http"
	"strings"

	"specquill/server/internal/auth"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
	"specquill/server/internal/store"
)

// Tenant resolution (repo-product/docs/specs/specs/multi-tenancy.md): API URLs carry the short repo
// id; the tenant comes from the request — the X-SpecQuill-Tenant header (or
// ?tenant= for websocket connects, which can't set headers), else the
// user's only membership. Users with no membership yet are auto-enrolled
// into the built-in `default` (config) tenant — self-host semantics.
func (s *Server) tenant(w http.ResponseWriter, r *http.Request) (*store.Tenant, bool) {
	u := auth.UserFrom(r.Context())
	if u == nil {
		jsonError(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	want := r.Header.Get("X-SpecQuill-Tenant")
	if want == "" {
		want = r.URL.Query().Get("tenant")
	}
	ms, err := s.memberships(u)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if want != "" {
		for i := range ms {
			if ms[i].Tenant.Slug == want {
				return &ms[i].Tenant, true
			}
		}
		jsonError2(w, http.StatusForbidden, "not a member of tenant "+want, "tenant_forbidden")
		return nil, false
	}
	switch len(ms) {
	case 0:
		jsonError2(w, http.StatusForbidden, "no tenant membership", "tenant_forbidden")
		return nil, false
	case 1:
		return &ms[0].Tenant, true
	default:
		jsonError2(w, http.StatusBadRequest, "multiple tenants — set X-SpecQuill-Tenant", "tenant_required")
		return nil, false
	}
}

// tenantQuiet resolves the request's tenant like tenant() but writes no error
// response — for best-effort paths (grounding) where the caller already
// resolved the tenant. Returns nil when resolution is ambiguous or fails.
func (s *Server) tenantQuiet(r *http.Request) *store.Tenant {
	u := auth.UserFrom(r.Context())
	if u == nil {
		return nil
	}
	want := r.Header.Get("X-SpecQuill-Tenant")
	if want == "" {
		want = r.URL.Query().Get("tenant")
	}
	ms, err := s.memberships(u)
	if err != nil {
		return nil
	}
	if want != "" {
		for i := range ms {
			if ms[i].Tenant.Slug == want {
				return &ms[i].Tenant
			}
		}
		return nil
	}
	if len(ms) == 1 {
		return &ms[0].Tenant
	}
	return nil
}

// memberships returns the user's tenants, auto-enrolling first-time users
// into the default config tenant when one exists. Users whose email is in
// auth.admin_emails are promoted to admin there — the bootstrap for a fresh
// deployment, where otherwise nobody could reach the management API.
func (s *Server) memberships(u *store.User) ([]store.Membership, error) {
	// github tenants first: roles derive from repo permissions (TTL-cached;
	// a no-op unless the GitHub App is configured and the user is a github
	// identity)
	s.syncGitHubMemberships(u)
	ms, err := s.store.Memberships(u.ID)
	if err != nil {
		return ms, err
	}
	if len(ms) == 0 {
		def, err := s.store.TenantBySlug(gitx.DefaultTenant)
		if err != nil || def.Provider != "config" {
			return nil, nil // no self-host tenant → membership comes from GitHub sync
		}
		if err := s.store.EnsureMember(def.ID, u.ID, "member"); err != nil {
			return nil, err
		}
		ms, err = s.store.Memberships(u.ID)
		if err != nil {
			return ms, err
		}
	}
	if s.isConfiguredAdmin(u.Email) {
		for i := range ms {
			if ms[i].Tenant.Provider == "config" && ms[i].Role != "admin" {
				if err := s.store.SetMemberRole(ms[i].Tenant.ID, u.ID, "admin"); err != nil {
					return ms, err
				}
				ms[i].Role = "admin"
			}
		}
	}
	return ms, nil
}

func (s *Server) isConfiguredAdmin(email string) bool {
	for _, a := range s.cfg.Auth.AdminEmails {
		if strings.EqualFold(a, email) {
			return true
		}
	}
	return false
}

// tenantProject resolves {repo} within the request's tenant: a project id
// first, else a source/repo name browsed as a read-only pseudo-project
// (config-split plan, D3 — the URL segment is stable across both).
func (s *Server) tenantProject(w http.ResponseWriter, r *http.Request) (*project.Project, bool) {
	t, ok := s.tenant(w, r)
	if !ok {
		return nil, false
	}
	id := r.PathValue("repo")
	if tp, err := s.store.TenantProject(t.ID, id); err == nil {
		repo, ok := s.git.Repo(t.Slug + "/" + tp.RepoID)
		if !ok {
			jsonError(w, http.StatusNotFound, "project repo not initialized")
			return nil, false
		}
		return project.New(repo, tp.ProjectID, tp.ContentRoot, false), true
	}
	repo, ok := s.git.Repo(t.Slug + "/" + id)
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown repo")
		return nil, false
	}
	if repo.Writable() {
		// a writable repo without a project row (test fixtures, migration
		// gaps) still resolves as a root project
		return project.New(repo, id, "", false), true
	}
	// read-only repos are SOURCES: browsing requires a stage-2 grant
	if _, err := s.store.GrantedSource(t.ID, id); err != nil {
		jsonError2(w, http.StatusForbidden, "source "+id+" is not granted to this tenant", "source_forbidden")
		return nil, false
	}
	return project.New(repo, id, "", true), true
}
