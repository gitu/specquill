package api

// Per-repo authorization (REQ-020): the effective role on a repo is the MAX
// of the derived role (GitHub permission on that repo / config-tenant
// membership) and an explicit repo grant. Grants are how a user outside the
// git host — or below the needed git permission — gets scoped access: the
// server pushes with the installation token, so the app layer is the only
// gate that matters.

import (
	"context"

	"specquill/server/internal/store"
)

var roleRank = map[string]int{"": 0, "viewer": 1, "member": 2, "admin": 3}

type repoRoleCtxKey struct{}

func withRepoRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, repoRoleCtxKey{}, role)
}

// repoRoleFrom reads the effective role repoH resolved for this request.
func repoRoleFrom(ctx context.Context) string {
	role, _ := ctx.Value(repoRoleCtxKey{}).(string)
	return role
}

// effectiveRepoRole resolves the caller's role on one repo of the tenant:
// max(derived, explicit grant), where derived is
//   - tenant admin → admin everywhere (admins manage the repo set),
//   - github tenant + github identity → the GitHub permission on THAT repo
//     (TTL-cached; a lookup failure falls back to the tenant role — GitHub
//     being down must not lock users out mid-session),
//   - otherwise the tenant member role (config tenants: the auto-enroll
//     default_role floor).
//
// "" means no access. Grant-only users have no tenant_members row, so their
// derived role is "" and the grant alone decides.
func (s *Server) effectiveRepoRole(u *store.User, t *store.Tenant, repoID string) string {
	memberRole, err := s.store.MemberRole(t.ID, u.ID)
	if err != nil {
		memberRole = ""
	}
	if memberRole == "admin" {
		return "admin"
	}
	derived := memberRole
	if t.Provider == "github" && s.ghApp != nil && t.Installation != 0 && u.Provider == "github" {
		derived = s.githubRepoRole(t, u, repoID, memberRole)
	}
	grant, err := s.store.RepoGrantRole(t.ID, repoID, u.ID)
	if err != nil {
		grant = ""
	}
	if roleRank[grant] > roleRank[derived] {
		return grant
	}
	return derived
}

// githubRepoRole derives the user's role on one repo from their GitHub
// permission, TTL-cached per (tenant, login, repo). fallback is returned on
// lookup failure (never revoke mid-session).
func (s *Server) githubRepoRole(t *store.Tenant, u *store.User, repoID, fallback string) string {
	login, err := s.store.UserLogin(u.ID)
	if err != nil || login == "" {
		return fallback
	}
	key := t.Slug + ":" + login + ":" + repoID
	if role, ok := s.ghRoles.get(key); ok {
		return role
	}
	tr, err := s.store.TenantRepo(t.ID, repoID)
	if err != nil {
		return fallback
	}
	if tr.GhFullName == "" {
		// not backed by a GitHub repository — nothing to derive from
		return ""
	}
	perm, err := s.ghApp.Permission(t.Installation, tr.GhFullName, login)
	if err != nil {
		return fallback
	}
	role := githubRole(perm)
	s.ghRoles.put(key, role)
	return role
}
