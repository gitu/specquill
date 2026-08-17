package api

import (
	"net/http"
	"strings"

	"specquill/server/internal/auth"
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

// resolveProject resolves {repo}: a project id first, then the caller's own
// dynamic projects (REQ-025 — per-user rows, per-user clones), else a source
// name browsed as a read-only pseudo-project (config-split plan, D3 — the
// URL segment is stable across both).
func (s *Server) resolveProject(w http.ResponseWriter, r *http.Request) (proj *project.Project, ok bool) {
	id := r.PathValue("repo")
	mgr := s.gitm(r)
	if s.patMode() {
		defer func() {
			// last-use stamp for the reclamation janitor (REQ-025.6): any
			// authenticated request that touches the repository counts.
			// Only on a RESOLVED repo, and keyed by the repo key — an
			// unresolved id would let arbitrary URLs grow the table, and a
			// project id is not the clone's directory name when the two
			// differ (the janitor would then never see the stamp).
			if !ok || proj == nil {
				return
			}
			if u := auth.UserFrom(r.Context()); u != nil {
				s.store.TouchClone(u.ID, scopeName(u.ID), proj.Repo.Key())
			}
		}()
	}
	if s.dynamicEnabled() && strings.HasPrefix(id, dynPrefix) {
		u := auth.UserFrom(r.Context())
		up, err := s.store.UserProject(u.ID, id)
		if err != nil {
			jsonError(w, http.StatusNotFound, "unknown repo")
			return nil, false
		}
		repo := s.registerDynRepo(mgr, *up)
		if !s.cloneReady(w, repo, s.tok(r)) {
			return nil, false
		}
		// the user's forge permission on THIS repository governs, and it
		// alone (REQ-025.3): viewer surfaces as a read-only project
		return project.New(repo, id, up.ContentRoot, up.Role == "viewer"), true
	}
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
		if !s.cloneReady(w, repo, s.tok(r)) {
			return nil, false
		}
		return project.New(repo, tp.ProjectID, tp.ContentRoot, false), true
	}
	repo, ok := mgr.Repo(id)
	if !ok && s.patMode() {
		// in-repo `sources:` register lazily — a first request for a source
		// may arrive before anything else listed it
		s.registerUserSources(mgr, s.tok(r))
		repo, ok = mgr.Repo(id)
	}
	if !ok {
		jsonError(w, http.StatusNotFound, "unknown repo")
		return nil, false
	}
	if repo.Writable() {
		// a writable repo without a project row (test fixtures, migration
		// gaps) still resolves as a root project
		if !s.cloneReady(w, repo, s.tok(r)) {
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
	if !s.cloneReady(w, repo, s.tok(r)) {
		return nil, false
	}
	return project.New(repo, id, "", true), true
}

// cloneReady lazily clones in forge-PAT mode with the caller's own token.
// Local mode clones at boot, so this is a no-op there.
func (s *Server) cloneReady(w http.ResponseWriter, repo *gitx.Repo, token string) bool {
	if !s.patMode() {
		return true
	}
	if err := repo.EnsureCloned(token); err != nil {
		jsonError2(w, http.StatusBadGateway, "clone failed: "+err.Error(), "clone_failed")
		return false
	}
	return true
}
