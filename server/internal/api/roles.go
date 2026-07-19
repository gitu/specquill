package api

// Per-repo authorization (REQ-020/REQ-021): the effective role on a repo is
// the MAX of the derived role (GitHub permission on that repo /
// config-tenant membership) and an explicit repo grant, on the four-level
// ladder authz.Viewer < Editor < Maintainer < Admin. Grants are how a user
// outside the git host — or below the needed git permission — gets scoped
// access: the server pushes with the installation token, so the app layer
// is the only gate that matters.

import (
	"context"

	"specquill/server/internal/authz"
	"specquill/server/internal/store"
)

type repoRoleCtxKey struct{}

func withRepoRole(ctx context.Context, role authz.Role) context.Context {
	return context.WithValue(ctx, repoRoleCtxKey{}, role)
}

// repoRoleFrom reads the effective role repoH resolved for this request.
func repoRoleFrom(ctx context.Context) authz.Role {
	role, _ := ctx.Value(repoRoleCtxKey{}).(authz.Role)
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
// authz.None means no access. Grant-only users have no tenant_members row,
// so their derived role is None and the grant alone decides.
func (s *Server) effectiveRepoRole(u *store.User, t *store.Tenant, repoID string) authz.Role {
	memberRole := authz.None
	if role, err := s.store.MemberRole(t.ID, u.ID); err == nil {
		memberRole = authz.Parse(role)
	}
	if memberRole == authz.Admin {
		return authz.Admin
	}
	derived := memberRole
	if t.Provider == "github" && s.ghApp != nil && t.Installation != 0 && u.Provider == "github" {
		derived = s.githubRepoRole(t, u, repoID, memberRole)
	}
	grant := authz.None
	if role, err := s.store.RepoGrantRole(t.ID, repoID, u.ID); err == nil {
		grant = authz.Parse(role)
	}
	return authz.Max(derived, grant)
}

// githubRepoRole derives the user's role on one repo from their GitHub
// permission, TTL-cached per (tenant, login, repo). fallback is returned on
// lookup failure (never revoke mid-session).
func (s *Server) githubRepoRole(t *store.Tenant, u *store.User, repoID string, fallback authz.Role) authz.Role {
	login, err := s.store.UserLogin(u.ID)
	if err != nil || login == "" {
		return fallback
	}
	key := t.Slug + ":" + login + ":" + repoID
	if role, ok := s.ghRoles.get(key); ok {
		return authz.Parse(role)
	}
	tr, err := s.store.TenantRepo(t.ID, repoID)
	if err != nil {
		return fallback
	}
	if tr.GhFullName == "" {
		// not backed by a GitHub repository (missing metadata, not a revoke
		// signal) — keep the tenant role, like any other failed lookup
		return fallback
	}
	perm, err := s.ghApp.Permission(t.Installation, tr.GhFullName, login)
	if err != nil {
		return fallback
	}
	role := authz.FromGitHub(perm)
	s.ghRoles.put(key, role.String())
	return role
}
