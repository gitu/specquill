package api

// Per-repo authorization (REQ-020): the effective role on a repo is the MAX
// of the deployment role and an explicit repo grant. Grants are how a user
// below the deployment-wide role — or outside auto-enrollment — gets scoped
// access: the server owns the git credentials, so the app layer is the only
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

// effectiveRepoRole resolves the caller's role on one repo:
// max(deployment role, explicit grant), where
//   - admin → admin everywhere (admins manage the repo set),
//   - otherwise the deployment role (the auto-enroll default_role floor).
//
// "" means no access. Grant-only users are not enrolled, so their deployment
// role is "" and the grant alone decides.
func (s *Server) effectiveRepoRole(u *store.User, repoID string) string {
	deploy, err := s.deployRole(u)
	if err != nil {
		deploy = ""
	}
	if deploy == "admin" {
		return "admin"
	}
	grant, err := s.store.RepoGrantRole(repoID, u.ID)
	if err != nil {
		grant = ""
	}
	if roleRank[grant] > roleRank[deploy] {
		return grant
	}
	return deploy
}
