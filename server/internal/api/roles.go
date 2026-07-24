package api

// Per-repo authorization (REQ-020): the effective role on a repo is the MAX
// of the membership role and an explicit repo grant. Grants are how a user
// below the tenant-wide role — or outside auto-enrollment — gets scoped
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

// effectiveRepoRole resolves the caller's role on one repo of the tenant:
// max(member role, explicit grant), where
//   - tenant admin → admin everywhere (admins manage the repo set),
//   - otherwise the tenant member role (the auto-enroll default_role floor).
//
// "" means no access. Grant-only users have no tenant_members row, so their
// member role is "" and the grant alone decides.
func (s *Server) effectiveRepoRole(u *store.User, t *store.Tenant, repoID string) string {
	memberRole, err := s.store.MemberRole(t.ID, u.ID)
	if err != nil {
		memberRole = ""
	}
	if memberRole == "admin" {
		return "admin"
	}
	grant, err := s.store.RepoGrantRole(t.ID, repoID, u.ID)
	if err != nil {
		grant = ""
	}
	if roleRank[grant] > roleRank[memberRole] {
		return grant
	}
	return memberRole
}
