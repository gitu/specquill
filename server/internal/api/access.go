package api

import (
	"net/http"
	"strings"

	"specquill/server/internal/authz"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
	"specquill/server/internal/store"
)

// Deployment enrollment: every authenticated user gets the deployment role
// from auth.default_role on first contact (editor unless configured; none
// disables auto-enroll, leaving access to explicit per-repo grants). Users
// whose email is in auth.admin_emails are promoted to admin — the bootstrap
// for a fresh deployment, where otherwise nobody could reach the management
// API.

// deployRole resolves (auto-enrolling if needed) the user's deployment role
// on the authz ladder. authz.None means not enrolled: access comes from
// explicit per-repo grants only.
func (s *Server) deployRole(u *store.User) (authz.Role, error) {
	if s.isConfiguredAdmin(u.Email) {
		if u.Role != "admin" {
			if err := s.store.SetUserRole(u.ID, "admin"); err != nil {
				return authz.Parse(u.Role), err
			}
			u.Role = "admin"
		}
		return authz.Admin, nil
	}
	if u.Role != "" {
		return authz.Parse(u.Role), nil
	}
	role := s.cfg.Auth.DefaultRole
	if role == "" {
		role = authz.Editor.String()
	}
	if role == "none" {
		return authz.None, nil // restricted deployment: access comes from grants only
	}
	if err := s.store.EnsureUserRole(u.ID, role); err != nil {
		return authz.None, err
	}
	u.Role = role
	return authz.Parse(role), nil
}

func (s *Server) isConfiguredAdmin(email string) bool {
	for _, a := range s.cfg.Auth.AdminEmails {
		if strings.EqualFold(a, email) {
			return true
		}
	}
	return false
}

// resolveProject resolves {repo}: a project id first, else a source name
// browsed as a read-only pseudo-project (config-split plan, D3 — the URL
// segment is stable across both).
func (s *Server) resolveProject(w http.ResponseWriter, r *http.Request) (*project.Project, bool) {
	id := r.PathValue("repo")
	mgr := s.gitm(r)
	if tp, err := s.store.Project(id); err == nil {
		repo, ok := mgr.Repo(tp.RepoID)
		if !ok && s.patMode() {
			// api-managed project added after this user's manager was created
			repo, ok = s.registerStoreRepo(mgr, tp.RepoID)
		}
		if !ok {
			jsonError(w, http.StatusNotFound, "project repo not initialized")
			return nil, false
		}
		if !s.cloneReady(w, repo) {
			return nil, false
		}
		return project.New(repo, tp.ProjectID, tp.ContentRoot, false), true
	}
	repo, ok := mgr.Repo(id)
	if !ok && s.patMode() {
		// in-repo `sources:` register lazily — a first request for a source
		// may arrive before anything else listed it
		s.registerUserSources(mgr)
		repo, ok = mgr.Repo(id)
	}
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown repo")
		return nil, false
	}
	if repo.Writable() {
		// a writable repo without a project row (test fixtures, migration
		// gaps) still resolves as a root project
		if !s.cloneReady(w, repo) {
			return nil, false
		}
		return project.New(repo, id, "", false), true
	}
	// read-only repos are SOURCES. Local mode: browsing requires a catalog
	// entry. Forge-PAT mode: being registered in the user's manager IS the
	// gate (only in-repo definitions land there), and the user's own token
	// decides whether the clone below succeeds.
	if !s.patMode() {
		if _, err := s.store.SourceByName(id); err != nil {
			jsonError2(w, http.StatusForbidden, "source "+id+" is not in the catalog", "source_forbidden")
			return nil, false
		}
	}
	if !s.cloneReady(w, repo) {
		return nil, false
	}
	return project.New(repo, id, "", true), true
}

// cloneReady lazily clones in forge-PAT mode (with the caller's token, set on
// the manager by gitm). Local mode clones at boot, so this is a no-op there.
func (s *Server) cloneReady(w http.ResponseWriter, repo *gitx.Repo) bool {
	if !s.patMode() {
		return true
	}
	if err := repo.EnsureCloned(); err != nil {
		jsonError2(w, http.StatusBadGateway, "clone failed: "+err.Error(), "clone_failed")
		return false
	}
	return true
}
