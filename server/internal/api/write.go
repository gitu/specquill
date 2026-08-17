package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"specquill/server/internal/project"
	"strings"

	"specquill/server/internal/auth"
	"specquill/server/internal/authz"
	"specquill/server/internal/gitx"
)

// writableH gates every mutation: the repo must be writable AND the caller
// at least an editor on it (viewers read and comment, they never write).
func (s *Server) writableH(h func(http.ResponseWriter, *http.Request, *project.Project)) http.HandlerFunc {
	return s.repoH(func(w http.ResponseWriter, r *http.Request, repo *project.Project) {
		if !repo.Writable() {
			jsonError(w, http.StatusForbidden, "repo "+repo.ID+" is read-only")
			return
		}
		if repoRoleFrom(r.Context()) < authz.Editor {
			jsonError2(w, http.StatusForbidden, "requires editor role", "role_forbidden")
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
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	if err := repo.DeleteFile(r.URL.Query().Get("branch"), r.PathValue("path")); err != nil {
		gitFail(w, err)
		return
	}
	s.publish("save", repo.Key(), branch)
	jsonOK(w, map[string]bool{"ok": true})
}

// postMove renames a file — or, with a trailing slash on from, a whole
// folder — in the branch worktree via git mv, and rewrites every document
// referencing the moved path(s) to the new location (server-side, sha-guarded
// worktree saves). The response lists the rewritten paths; folder moves also
// report how many files moved.
func (s *Server) postMove(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	var body struct{ From, To string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.From == "" || body.To == "" {
		jsonError(w, http.StatusBadRequest, "from and to are required")
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	if strings.HasSuffix(body.From, "/") || strings.HasSuffix(body.To, "/") {
		moved, rewritten, err := repo.MoveFolderRewriting(r.URL.Query().Get("branch"), body.From, body.To)
		if err != nil {
			gitFail(w, err)
			return
		}
		s.publish("save", repo.Key(), branch)
		jsonOK(w, map[string]any{"from": body.From, "to": body.To, "moved": moved, "rewritten": rewritten})
		return
	}
	rewritten, err := repo.MoveFileRewriting(r.URL.Query().Get("branch"), body.From, body.To)
	if err != nil {
		gitFail(w, err)
		return
	}
	s.publish("save", repo.Key(), branch)
	jsonOK(w, map[string]any{"from": body.From, "to": body.To, "rewritten": rewritten})
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
	sha, err := repo.Commit(branch, body.Message, u.Name, u.Email, body.Paths)
	if err != nil {
		gitFail(w, err)
		return
	}
	s.publish("commit", repo.Key(), branch)
	out := map[string]any{"commitSha": sha}
	// forge-PAT mode: push the workspace branch as the commit happens
	// (REQ-025.10) — committed work never exists only in a server-side
	// clone. Best-effort: a failed push never undoes the commit, propose
	// retries it anyway.
	if s.patMode() {
		if err := repo.Push(branch, s.tok(r)); err != nil {
			out["pushed"] = false
			out["pushError"] = err.Error()
		} else {
			out["pushed"] = true
		}
	}
	jsonOK(w, out)
}

// postDiscard rejects pending (uncommitted) worktree changes — the undo
// counterpart of postCommit. Optional paths limit it to specific files.
func (s *Server) postDiscard(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	if err := repo.Discard(r.URL.Query().Get("branch"), body.Paths); err != nil {
		gitFail(w, err)
		return
	}
	s.publish("save", repo.Key(), branch)
	jsonOK(w, map[string]bool{"ok": true})
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
	if err := repo.Push(r.URL.Query().Get("branch"), s.tok(r)); err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

func (s *Server) postFetch(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if err := repo.Fetch(s.tok(r)); err != nil {
		gitFail(w, err)
		return
	}
	s.publish("fetch", repo.Key(), "")
	// remote moved → local follows
	updated := repo.FFBranches()
	for _, branch := range updated {
		s.publish("pull", repo.Key(), branch)
	}
	jsonOK(w, map[string]any{"ok": true, "updated": updated})
}
