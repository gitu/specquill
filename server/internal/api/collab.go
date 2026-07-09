package api

import (
	"net/http"
	"specquill/server/internal/project"
	"strings"

	"github.com/coder/websocket"

	"specquill/server/internal/auth"
)

// GET /api/repos/{repo}/collab/{path...}?branch=  — websocket upgrade into a
// co-editing room. Auth comes from the session middleware; CSRF does not
// apply to GET upgrades, so origins are checked explicitly.
func (s *Server) collabWS(w http.ResponseWriter, r *http.Request, repo *project.Project) {
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
	// rooms key by FULL repo paths (store rows are project-agnostic)
	fullPath, err := repo.MapIn(path)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

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
	s.hub.Join(r.Context(), ws, repo.Key(), branch, fullPath, u.ID, u.Name)
}

// GET /api/repos/{repo}/presence — room paths map back to project-relative;
// rooms outside the content root (another project sharing the repo) are
// filtered out.
func (s *Server) getPresence(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	rooms := s.hub.Presence(repo.Key())
	out := rooms[:0]
	for _, room := range rooms {
		if rel, ok := repo.MapOut(room.Path); ok {
			room.Path = rel
			out = append(out, room)
		}
	}
	jsonOK(w, out)
}
