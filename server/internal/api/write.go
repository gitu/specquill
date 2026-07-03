package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"reqbase/server/internal/auth"
	"reqbase/server/internal/gitx"
)

// writableH additionally rejects requests against read-only repos.
func (s *Server) writableH(h func(http.ResponseWriter, *http.Request, *gitx.Repo)) http.HandlerFunc {
	return s.repoH(func(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
		if !repo.Writable() {
			jsonError(w, http.StatusForbidden, "repo "+repo.Cfg.ID+" is read-only")
			return
		}
		h(w, r, repo)
	})
}

func (s *Server) putFile(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	var body struct {
		Content string `json:"content"`
		BaseSha string `json:"baseSha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	if s.hub.RoomActive(repo.Cfg.ID, branch, r.PathValue("path")) {
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
	s.publish("save", repo.Cfg.ID, repo.ResolveRef(r.URL.Query().Get("branch")))
	jsonOK(w, map[string]string{"sha": sha})
}

func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	if s.hub.RoomActive(repo.Cfg.ID, repo.ResolveRef(r.URL.Query().Get("branch")), r.PathValue("path")) {
		jsonError2(w, http.StatusConflict, "file is being co-edited", "room_active")
		return
	}
	if err := repo.DeleteFile(r.URL.Query().Get("branch"), r.PathValue("path")); err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	st, err := repo.Status(r.URL.Query().Get("branch"))
	if err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, st)
}

func (s *Server) postCommit(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
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
	// contributor besides the author lands as a Co-authored-by trailer
	_ = s.hub.FlushBranch(r.Context(), repo.Cfg.ID, branch, body.Paths)
	message := body.Message
	if contributors, err := s.store.Contributors(repo.Cfg.ID, branch, body.Paths); err == nil {
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
	_ = s.store.ClearContributors(repo.Cfg.ID, branch, body.Paths)
	s.publish("commit", repo.Cfg.ID, branch)
	jsonOK(w, map[string]string{"commitSha": sha})
}

func (s *Server) postBranch(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
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

func (s *Server) postPush(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	if err := repo.Push(r.URL.Query().Get("branch")); err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

func (s *Server) postFetch(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	if err := repo.Fetch(); err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
