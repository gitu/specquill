package api

// Direct merges: work lands on the default branch straight from a workspace
// branch, with no in-app review step. Main stays protected (content writes to
// it 403), so a merge commit from `ws/<user>` remains the only way it moves —
// this is the entry point that makes that possible.
//
// Reviewed merges, when a team wants them, belong on the forge: push the
// branch and open a merge request there.

import (
	"encoding/json"
	"log"
	"net/http"

	"specquill/server/internal/auth"
	"specquill/server/internal/authz"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
)

// mergeRefs resolves the source/target pair from the request, defaulting the
// target to the repo's default branch, and validates both exist.
func (s *Server) mergeRefs(w http.ResponseWriter, repo *project.Project, source, target string) (string, string, bool) {
	if source == "" {
		jsonError(w, http.StatusBadRequest, "source branch is required")
		return "", "", false
	}
	if target == "" {
		target = repo.Cfg.DefaultBranch
	}
	// Both names come straight from the request and travel into git argv and
	// worktree paths, so validate here rather than relying on git's own
	// refname rules further down (a leading "-" would be read as an option).
	if !gitx.ValidRef(source) || !gitx.ValidRef(target) {
		jsonError(w, http.StatusBadRequest, "invalid branch name")
		return "", "", false
	}
	if source == target {
		jsonError(w, http.StatusBadRequest, "source and target are the same branch")
		return "", "", false
	}
	if !repo.BranchExists(source) || !repo.BranchExists(target) {
		jsonError(w, http.StatusNotFound, "branch not found")
		return "", "", false
	}
	return source, target, true
}

// GET /api/repos/{repo}/merge?source=&target= — what merging would do:
// the diff being landed, whether it conflicts, and any uncommitted changes
// that would be left behind. Readable by viewers; merging itself is not.
func (s *Server) getMergePreview(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	source, target, ok := s.mergeRefs(w, repo, r.URL.Query().Get("source"), r.URL.Query().Get("target"))
	if !ok {
		return
	}
	out := map[string]any{"source": source, "target": target}

	// uncommitted work is not part of a merge — surface it so the client can
	// prompt to commit first rather than silently landing less than expected
	if st, err := repo.Status(source); err == nil {
		out["dirty"] = st.Dirty
	}
	if check, err := repo.CheckMerge(target, source); err == nil {
		out["mergeable"] = check.Mergeable
		out["conflicts"] = check.Conflicts
	}
	files, err := repo.DiffRange(target, source)
	if err != nil {
		gitFail(w, err)
		return
	}
	if files == nil {
		files = []gitx.DiffFile{} // a level branch has no diff — [] , never null
	}
	out["files"] = files
	jsonOK(w, out)
}

// POST /api/repos/{repo}/merge {source, target?, strategy?, message?}
// — land a workspace branch on the default branch.
//
// Landing on a PROTECTED branch takes maintainer (REQ-021.2): an editor
// writes and commits on their own branch, but publishing to the branch
// everyone reads is the higher rung. Unprotected targets merge at editor.
func (s *Server) postMerge(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.patMode() {
		// forge-PAT mode: the default branch only moves on the forge — push
		// the workspace branch and open a merge request (POST .../propose)
		jsonError2(w, http.StatusForbidden, "in-app merges are disabled — propose the changes as a merge request instead", "merge_via_forge")
		return
	}
	var body struct{ Source, Target, Strategy, Message string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	source, target, ok := s.mergeRefs(w, repo, body.Source, body.Target)
	if !ok {
		return
	}
	if body.Strategy == "" {
		body.Strategy = "merge"
	}
	// gitx only special-cases "squash", so an unrecognised value would
	// silently land a merge commit instead — refuse it rather than guess
	if body.Strategy != "merge" && body.Strategy != "squash" {
		jsonError(w, http.StatusBadRequest, `strategy must be "merge" or "squash"`)
		return
	}
	need := authz.Editor
	if repo.Repo.Cfg.IsProtected(target) {
		need = authz.Maintainer
	}
	if repoRoleFrom(r.Context()) < need {
		jsonError2(w, http.StatusForbidden, "requires "+need.String()+" role", "role_forbidden")
		return
	}

	// dirty source worktree → the client prompts to commit first (same
	// contract the PR dialog used, so the UI flow is unchanged)
	st, err := repo.Status(source)
	if err != nil {
		gitFail(w, err)
		return
	}
	if len(st.Dirty) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "uncommitted changes on " + source, "code": "dirty", "dirty": st.Dirty,
		})
		return
	}

	message := body.Message
	if message == "" {
		message = "Merge " + source + " into " + target
		if body.Strategy == "squash" {
			message = "Squash " + source + " into " + target
		}
	}
	u := auth.UserFrom(r.Context())
	sha, check, err := repo.Merge(target, source, message, u.Name, u.Email, body.Strategy)
	if err != nil {
		gitFail(w, err)
		return
	}
	if check != nil && !check.Mergeable {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "merge conflicts", "code": "conflicts", "conflicts": check.Conflicts,
		})
		return
	}
	// a merged personal workspace resets onto the new default-branch head so
	// it stays perpetually reusable (its worktree is clean — checked above)
	if _, err := s.store.WorkspaceOwner(repo.Key(), source); err == nil {
		if err := repo.ResetBranchFF(source, sha); err != nil {
			log.Printf("workspace reset %s after merge: %v", source, err)
		}
	}
	s.publish("merge", repo.Key(), target)
	jsonOK(w, map[string]string{"mergedCommit": sha})
}
