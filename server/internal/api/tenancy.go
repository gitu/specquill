package api

import (
	"net/http"

	"specquill/server/internal/auth"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

// Tenant resolution (docs/multi-tenancy.md): API URLs carry the short repo
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

// memberships returns the user's tenants, auto-enrolling first-time users
// into the default config tenant when one exists.
func (s *Server) memberships(u *store.User) ([]store.Membership, error) {
	ms, err := s.store.Memberships(u.ID)
	if err != nil || len(ms) > 0 {
		return ms, err
	}
	def, err := s.store.TenantBySlug(gitx.DefaultTenant)
	if err != nil || def.Provider != "config" {
		return nil, nil // no self-host tenant → membership comes from GitHub sync
	}
	if err := s.store.EnsureMember(def.ID, u.ID, "member"); err != nil {
		return nil, err
	}
	return s.store.Memberships(u.ID)
}

// tenantRepo resolves {repo} within the request's tenant.
func (s *Server) tenantRepo(w http.ResponseWriter, r *http.Request) (*gitx.Repo, bool) {
	t, ok := s.tenant(w, r)
	if !ok {
		return nil, false
	}
	repo, ok := s.git.Repo(t.Slug + "/" + r.PathValue("repo"))
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown repo")
		return nil, false
	}
	return repo, true
}
