package api

import (
	"net/http"
	"strings"

	"github.com/coder/websocket"

	"specquill/server/internal/auth"
	"specquill/server/internal/gitx"
)

// GET /api/repos/{repo}/collab/{path...}?branch=  — websocket upgrade into a
// co-editing room. Auth comes from the session middleware; CSRF does not
// apply to GET upgrades, so origins are checked explicitly.
func (s *Server) collabWS(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	path := r.PathValue("path")
	if repo.Cfg.IsProtected(branch) {
		jsonError2(w, http.StatusForbidden, "cannot co-edit on a protected branch", "protected_branch")
		return
	}
	if !strings.HasSuffix(path, ".md") {
		jsonError(w, http.StatusBadRequest, "co-editing is only available for markdown files")
		return
	}
	if !repo.BranchExists(branch) {
		jsonError(w, http.StatusNotFound, "branch not found")
		return
	}
	if _, _, err := repo.File(branch, path); err != nil {
		gitFail(w, err)
		return
	}
	u := auth.UserFrom(r.Context())

	opts := &websocket.AcceptOptions{}
	if !s.cfg.Session.CookieSecure {
		opts.InsecureSkipVerify = true // dev: vite origin differs from the API origin
	}
	ws, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(4 << 20) // yjs snapshots of large docs
	s.hub.Join(r.Context(), ws, repo.Key(), branch, path, u.ID, u.Name)
}

// GET /api/repos/{repo}/presence
func (s *Server) getPresence(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	jsonOK(w, s.hub.Presence(repo.Key()))
}
