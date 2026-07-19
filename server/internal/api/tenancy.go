package api

import (
	"net/http"
	"strings"

	"specquill/server/internal/auth"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
	"specquill/server/internal/store"
)

// Tenant resolution (REQ-022, repo-product/docs/specs/specs/multi-tenancy.md):
// the tenant is named by the URL path — every tenant-scoped route lives under
// /api/t/{tenant}/… — and checked against the caller's visibility (a
// membership row, or a repo grant surfaced as a synthetic membership). There
// is no header or query fallback. Users with no membership yet are
// auto-enrolled into the built-in `default` (config) tenant — self-host
// semantics.
func (s *Server) tenant(w http.ResponseWriter, r *http.Request) (*store.Tenant, bool) {
	u := auth.UserFrom(r.Context())
	if u == nil {
		jsonError(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	want := r.PathValue("tenant")
	if want == "" {
		jsonError2(w, http.StatusBadRequest, "tenant missing from URL", "tenant_required")
		return nil, false
	}
	ms, err := s.memberships(u)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	for i := range ms {
		if ms[i].Tenant.Slug == want {
			return &ms[i].Tenant, true
		}
	}
	jsonError2(w, http.StatusForbidden, "not a member of tenant "+want, "tenant_forbidden")
	return nil, false
}

// tenantQuiet resolves the request's tenant like tenant() but writes no error
// response — for best-effort paths (grounding) where the caller already
// resolved the tenant. Returns nil when resolution fails.
func (s *Server) tenantQuiet(r *http.Request) *store.Tenant {
	u := auth.UserFrom(r.Context())
	if u == nil {
		return nil
	}
	want := r.PathValue("tenant")
	if want == "" {
		return nil
	}
	ms, err := s.memberships(u)
	if err != nil {
		return nil
	}
	for i := range ms {
		if ms[i].Tenant.Slug == want {
			return &ms[i].Tenant
		}
	}
	return nil
}

// memberships returns the user's tenants, auto-enrolling first-time users
// into the default config tenant when one exists — with the role from
// auth.default_role (editor unless configured; none disables auto-enroll,
// leaving access to explicit per-repo grants). Users whose email is in
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
	real := false
	for i := range ms {
		if !ms[i].GrantOnly {
			real = true
			break
		}
	}
	if !real {
		def, err := s.store.TenantBySlug(gitx.DefaultTenant)
		if err != nil || def.Provider != "config" {
			return ms, nil // no self-host tenant → membership comes from GitHub sync (or grants)
		}
		role := s.cfg.Auth.DefaultRole
		if role == "" {
			role = "editor"
		}
		if s.isConfiguredAdmin(u.Email) {
			role = "admin" // default_role: none must not lock the bootstrap admin out
		}
		if role == "none" {
			return ms, nil // restricted deployment: access comes from grants only
		}
		if err := s.store.EnsureMember(def.ID, u.ID, role); err != nil {
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
// (config-split plan, D3 — the URL segment is stable across both). The
// tenant is returned alongside so repoH can gate on the effective role.
func (s *Server) tenantProject(w http.ResponseWriter, r *http.Request) (*project.Project, *store.Tenant, bool) {
	t, ok := s.tenant(w, r)
	if !ok {
		return nil, nil, false
	}
	id := r.PathValue("repo")
	if tp, err := s.store.TenantProject(t.ID, id); err == nil {
		repo, ok := s.git.Repo(t.Slug + "/" + tp.RepoID)
		if !ok {
			jsonError(w, http.StatusNotFound, "project repo not initialized")
			return nil, nil, false
		}
		return project.New(repo, tp.ProjectID, tp.ContentRoot, false), t, true
	}
	repo, ok := s.git.Repo(t.Slug + "/" + id)
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown repo")
		return nil, nil, false
	}
	if repo.Writable() {
		// a writable repo without a project row (test fixtures, migration
		// gaps) still resolves as a root project
		return project.New(repo, id, "", false), t, true
	}
	// read-only repos are SOURCES: browsing requires a stage-2 grant
	if _, err := s.store.GrantedSource(t.ID, id); err != nil {
		jsonError2(w, http.StatusForbidden, "source "+id+" is not granted to this tenant", "source_forbidden")
		return nil, nil, false
	}
	return project.New(repo, id, "", true), t, true
}
