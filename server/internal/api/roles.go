package api

// Per-repo authorization (REQ-020/REQ-021): the effective role on a repo is
// the MAX of the deployment role and an explicit repo grant, on the
// four-level ladder authz.Viewer < Editor < Maintainer < Admin. Grants are
// how a user below the deployment-wide role — or outside auto-enrollment —
// gets scoped access: the server owns the git credentials, so the app layer
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

// effectiveRepoRole resolves the caller's role on one repo:
// max(deployment role, explicit grant), where
//   - admin → admin everywhere (admins manage the repo set),
//   - otherwise the deployment role (the auto-enroll default_role floor).
//
// authz.None means no access. Grant-only users are not enrolled, so their
// deployment role is None and the grant alone decides.
func (s *Server) effectiveRepoRole(u *store.User, repoID string) authz.Role {
	deploy, err := s.deployRole(u)
	if err != nil {
		deploy = authz.None
	}
	if deploy == authz.Admin {
		return authz.Admin
	}
	grant, err := s.store.RepoGrantRole(repoID, u.ID)
	if err != nil {
		return deploy
	}
	return authz.Max(deploy, authz.Parse(grant))
}
