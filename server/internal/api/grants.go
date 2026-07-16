package api

// Per-repo grant management (REQ-020) — tenant-admin gated. A grant targets
// an existing user by email or GitHub login; unknown identities become
// pending invites, claimed on the invitee's first login.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"specquill/server/internal/auth"
	"specquill/server/internal/store"
)

// grantRepoID resolves the {repo} URL segment to the tenant_repos repo id
// (grants are per repository — a monorepo's projects share one grant).
func (s *Server) grantRepoID(t *store.Tenant, id string) (string, error) {
	if tp, err := s.store.TenantProject(t.ID, id); err == nil {
		return tp.RepoID, nil
	}
	if _, err := s.store.TenantRepo(t.ID, id); err == nil {
		return id, nil
	}
	return "", store.ErrNotFound
}

// GET /api/members — the tenant's members (derived or enrolled roles).
func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	ms, err := s.store.TenantMemberList(t.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, ms)
}

// GET /api/repos/{repo}/grants — explicit grants + pending invites.
func (s *Server) listGrants(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	repoID, err := s.grantRepoID(t, r.PathValue("repo"))
	if err != nil {
		jsonError(w, http.StatusNotFound, "unknown repo")
		return
	}
	grants, err := s.store.RepoGrants(t.ID, repoID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	invites, err := s.store.RepoGrantInvites(t.ID, repoID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"grants": grants, "invites": invites})
}

// POST /api/repos/{repo}/grants {user, role} — grant an existing user, or
// leave an invite for an identity that has not logged in yet. `user` is an
// email address or a GitHub login (optionally @-prefixed).
func (s *Server) createGrant(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	repoID, err := s.grantRepoID(t, r.PathValue("repo"))
	if err != nil {
		jsonError(w, http.StatusNotFound, "unknown repo")
		return
	}
	var body struct {
		User string `json:"user"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.User) == "" {
		jsonError(w, http.StatusBadRequest, "user (email or github login) is required")
		return
	}
	if body.Role != "viewer" && body.Role != "member" {
		// per-repo admin is meaningless — repo/project management is tenant-scoped
		jsonError(w, http.StatusBadRequest, "role must be viewer or member")
		return
	}
	caller := auth.UserFrom(r.Context())
	target := strings.TrimSpace(body.User)
	u, err := s.store.UserByEmailOrLogin(target)
	switch {
	case err == nil:
		if err := s.store.UpsertRepoGrant(t.ID, repoID, u.ID, body.Role, caller.ID); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, map[string]any{"status": "granted", "userId": u.ID, "role": body.Role})
	case errors.Is(err, store.ErrNotFound):
		kind, matcher := "github", strings.TrimPrefix(target, "@")
		if strings.Contains(matcher, "@") {
			kind = "email"
		}
		if err := s.store.AddGrantInvite(t.ID, repoID, kind, matcher, body.Role, caller.ID); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		jsonOK(w, map[string]any{"status": "invited", "kind": kind, "matcher": strings.ToLower(matcher), "role": body.Role})
	default:
		jsonError(w, http.StatusInternalServerError, err.Error())
	}
}

// DELETE /api/repos/{repo}/grants/{userId} — revoke an explicit grant.
func (s *Server) deleteGrant(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	repoID, err := s.grantRepoID(t, r.PathValue("repo"))
	if err != nil {
		jsonError(w, http.StatusNotFound, "unknown repo")
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := s.store.DeleteRepoGrant(t.ID, repoID, userID); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// DELETE /api/repos/{repo}/grants/invites/{id} — withdraw a pending invite.
func (s *Server) deleteGrantInvite(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid invite id")
		return
	}
	if err := s.store.DeleteGrantInvite(t.ID, id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
