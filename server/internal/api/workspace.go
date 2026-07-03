package api

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"reqbase/server/internal/auth"
	"reqbase/server/internal/gitx"
	"reqbase/server/internal/store"
)

var slugRe = regexp.MustCompile(`[^a-z0-9-]+`)

func workspaceSlug(u *store.User) string {
	base := u.Subject
	if u.Provider != "local" || base == "" {
		if i := strings.IndexByte(u.Email, '@'); i > 0 {
			base = u.Email[:i]
		} else {
			base = u.Name
		}
	}
	slug := slugRe.ReplaceAllString(strings.ToLower(base), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "user-" + strconv.FormatInt(u.ID, 10)
	}
	return slug
}

// POST /api/repos/{repo}/workspace — resolve/claim the caller's personal
// workspace branch and ensure it exists (fast-forwarding when safe).
func (s *Server) postWorkspace(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	u := auth.UserFrom(r.Context())
	branch, err := s.store.WorkspaceBranch(repo.Cfg.ID, u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if branch == "" {
		branch = "ws/" + workspaceSlug(u)
		if err := s.store.ClaimWorkspaceBranch(repo.Cfg.ID, branch, u.ID); err != nil {
			// name taken by another user → deterministic suffix
			branch = branch + "-" + strconv.FormatInt(u.ID, 10)
			if err := s.store.ClaimWorkspaceBranch(repo.Cfg.ID, branch, u.ID); err != nil {
				jsonError(w, http.StatusInternalServerError, "claim workspace: "+err.Error())
				return
			}
		}
	}
	// a live co-editing room owns its file — never move the ref under it
	noFF := len(s.hub.ActiveOnBranch(repo.Cfg.ID, branch)) > 0
	ws, err := repo.EnsureWorkspace(branch, noFF)
	if err != nil {
		gitFail(w, err)
		return
	}
	s.publish("workspace", repo.Cfg.ID, branch)
	jsonOK(w, map[string]any{
		"branch": ws.Branch, "created": ws.Created, "state": ws.State, "base": ws.Base,
		// tells the client the ff was withheld, not impossible
		"heldByRoom": noFF && ws.State == "behind",
	})
}

// POST /api/repos/{repo}/pull?branch= — fast-forward onto origin.
func (s *Server) postPull(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	branch := r.URL.Query().Get("branch")
	// never move a ref while a co-editing room is live on the branch
	if paths := s.hub.ActiveOnBranch(repo.Cfg.ID, repo.ResolveRef(branch)); len(paths) > 0 {
		jsonError2(w, http.StatusConflict, "live co-editing session on "+strings.Join(paths, ", "), "room_active")
		return
	}
	head, updated, err := repo.Pull(branch)
	switch {
	case errors.Is(err, gitx.ErrDirtyWorktree):
		jsonError2(w, http.StatusConflict, err.Error(), "dirty")
		return
	case errors.Is(err, gitx.ErrDiverged):
		jsonError2(w, http.StatusConflict, err.Error(), "diverged")
		return
	case err != nil:
		gitFail(w, err)
		return
	}
	if updated {
		s.publish("pull", repo.Cfg.ID, repo.ResolveRef(branch))
	}
	jsonOK(w, map[string]any{"head": head, "updated": updated})
}

// GET /api/repos/{repo}/diff/worktree?branch=
func (s *Server) getWorktreeDiff(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	files, err := repo.DiffWorktree(r.URL.Query().Get("branch"))
	if err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]any{"files": files})
}
