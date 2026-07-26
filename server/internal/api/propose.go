package api

// Propose changes: the forge-PAT counterpart of the in-app merge. Pushes the
// caller's branch with their own token and opens (or re-uses) a merge
// request / pull request on the forge — review and the actual merge happen
// there; the default branch only moves back into specquill via fetch.

import (
	"encoding/json"
	"net/http"

	"specquill/server/internal/forge"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
)

// POST /api/repos/{repo}/propose {source, title?, body?}
func (s *Server) postPropose(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	cfg := repo.Repo.Cfg.Forge
	if !cfg.Enabled() {
		jsonError(w, http.StatusNotImplemented, "no forge configured for this project")
		return
	}
	var body struct{ Source, Title, Body string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Source == "" {
		jsonError(w, http.StatusBadRequest, "source branch is required")
		return
	}
	if !gitx.ValidRef(body.Source) {
		jsonError(w, http.StatusBadRequest, "invalid branch name")
		return
	}
	target := repo.Cfg.DefaultBranch
	if body.Source == target {
		jsonError(w, http.StatusBadRequest, "cannot propose the default branch onto itself")
		return
	}
	if !repo.BranchExists(body.Source) {
		jsonError(w, http.StatusNotFound, "branch not found")
		return
	}

	// uncommitted work would not travel with the push — same contract as the
	// in-app merge, so the client prompts to commit first
	st, err := repo.Status(body.Source)
	if err != nil {
		gitFail(w, err)
		return
	}
	if len(st.Dirty) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "uncommitted changes on " + body.Source, "code": "dirty", "dirty": st.Dirty,
		})
		return
	}

	if err := repo.Push(body.Source); err != nil {
		gitFail(w, err)
		return
	}
	client, err := forge.New(cfg, repo.Repo.Cfg.Remote, s.forgeToken(r, cfg))
	if err != nil || client == nil {
		jsonError(w, http.StatusInternalServerError, "forge misconfigured: "+errStr(err))
		return
	}
	req, created, err := client.CreateRequest(r.Context(), body.Source, target, body.Title, body.Body)
	if err != nil {
		jsonError2(w, http.StatusBadGateway, "create merge request: "+err.Error(), "forge_failed")
		return
	}
	s.publish("propose", repo.Key(), body.Source)
	jsonOK(w, map[string]any{
		"number": req.Number, "url": req.URL, "title": req.Title, "created": created,
	})
}
