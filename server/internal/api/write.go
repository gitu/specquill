package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"specquill/server/internal/project"
	"strings"

	"specquill/server/internal/auth"
	"specquill/server/internal/gitx"
)

// writableH gates every mutation: the repo must be writable AND the caller
// at least a member on it (viewers read and comment, they never write).
func (s *Server) writableH(h func(http.ResponseWriter, *http.Request, *project.Project)) http.HandlerFunc {
	return s.repoH(func(w http.ResponseWriter, r *http.Request, repo *project.Project) {
		if !repo.Writable() {
			jsonError(w, http.StatusForbidden, "repo "+repo.ID+" is read-only")
			return
		}
		if roleRank[repoRoleFrom(r.Context())] < roleRank["member"] {
			jsonError2(w, http.StatusForbidden, "requires member role", "role_forbidden")
			return
		}
		h(w, r, repo)
	})
}

// writableViewH requires a writable repo but only viewer role — PR reads,
// comments, presence and draft status live on writable repos yet are open
// to viewers ("read, comment" in the role table).
func (s *Server) writableViewH(h func(http.ResponseWriter, *http.Request, *project.Project)) http.HandlerFunc {
	return s.repoH(func(w http.ResponseWriter, r *http.Request, repo *project.Project) {
		if !repo.Writable() {
			jsonError(w, http.StatusForbidden, "repo "+repo.ID+" is read-only")
			return
		}
		h(w, r, repo)
	})
}

func (s *Server) putFile(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	var body struct {
		Content string `json:"content"`
		BaseSha string `json:"baseSha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	if full, err := repo.MapIn(r.PathValue("path")); err == nil && s.hub.RoomActive(repo.Key(), branch, full) {
		jsonError2(w, http.StatusConflict, "file is being co-edited — its live session owns the content", "room_active")
		return
	}
	sha, err := repo.SaveFile(r.URL.Query().Get("branch"), r.PathValue("path"), body.Content, body.BaseSha)
	if errors.Is(err, gitx.ErrStale) {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		gitFail(w, err)
		return
	}
	s.publish("save", repo.Key(), repo.ResolveRef(r.URL.Query().Get("branch")))
	jsonOK(w, map[string]string{"sha": sha})
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if full, err := repo.MapIn(r.PathValue("path")); err == nil && s.hub.RoomActive(repo.Key(), repo.ResolveRef(r.URL.Query().Get("branch")), full) {
		jsonError2(w, http.StatusConflict, "file is being co-edited", "room_active")
		return
	}
	if err := repo.DeleteFile(r.URL.Query().Get("branch"), r.PathValue("path")); err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// postMove renames a file in the branch worktree via git mv; the reference
// rewrite that usually follows is a series of ordinary PUTs from the client.
func (s *Server) postMove(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	var body struct{ From, To string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.From == "" || body.To == "" {
		jsonError(w, http.StatusBadRequest, "from and to are required")
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	if full, err := repo.MapIn(body.From); err == nil && s.hub.RoomActive(repo.Key(), branch, full) {
		jsonError2(w, http.StatusConflict, "file is being co-edited — its live session owns the content", "room_active")
		return
	}
	if err := repo.MoveFile(r.URL.Query().Get("branch"), body.From, body.To); err != nil {
		gitFail(w, err)
		return
	}
	s.publish("save", repo.Key(), branch)
	jsonOK(w, map[string]string{"from": body.From, "to": body.To})
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	st, err := repo.Status(r.URL.Query().Get("branch"))
	if err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, st)
}

func (s *Server) postCommit(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	var body struct {
		Message string   `json:"message"`
		Paths   []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	u := auth.UserFrom(r.Context())
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))

	// commit barrier: live co-editing rooms flush their docs first, and every
	// contributor besides the author lands as a Co-authored-by trailer.
	// Hub/store key rooms by FULL repo paths — map the request's
	// project-relative paths in.
	fullPaths := make([]string, 0, len(body.Paths))
	for _, rel := range body.Paths {
		if full, err := repo.MapIn(rel); err == nil {
			fullPaths = append(fullPaths, full)
		}
	}
	_ = s.hub.FlushBranch(r.Context(), repo.Key(), branch, fullPaths)
	message := body.Message
	if contributors, err := s.store.Contributors(repo.Key(), branch, fullPaths); err == nil {
		trailers := ""
		for _, c := range contributors {
			if c.ID == u.ID {
				continue
			}
			trailers += "\nCo-authored-by: " + c.Name + " <" + c.Email + ">"
		}
		if trailers != "" {
			message = strings.TrimRight(message, "\n") + "\n" + trailers
		}
	}

	sha, err := repo.Commit(branch, message, u.Name, u.Email, body.Paths)
	if err != nil {
		gitFail(w, err)
		return
	}
	_ = s.store.ClearContributors(repo.Key(), branch, fullPaths)
	s.publish("commit", repo.Key(), branch)
	jsonOK(w, map[string]string{"commitSha": sha})
}

func (s *Server) postBranch(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	var body struct{ Name, From string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		jsonError(w, http.StatusBadRequest, "branch name required")
		return
	}
	if err := repo.CreateBranch(body.Name, body.From); err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]string{"name": body.Name})
}

func (s *Server) postPush(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if err := repo.Push(r.URL.Query().Get("branch")); err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

func (s *Server) postFetch(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if err := repo.Fetch(); err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
