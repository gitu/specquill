package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"specquill/server/internal/auth"
	"specquill/server/internal/gitx"
	"specquill/server/internal/store"
)

type prView struct {
	*store.PR
	HeadSha      string         `json:"headSha"`
	Mergeable    *bool          `json:"mergeable,omitempty"`
	Conflicts    []string       `json:"conflicts,omitempty"`
	Approvals    []approvalView `json:"approvals"`
	CommentCount int            `json:"commentCount"`
}

type approvalView struct {
	store.PRApproval
	Current bool `json:"current"` // approval is for the current head
}

func (s *Server) prView(repo *gitx.Repo, pr *store.PR, withMergeability bool) (*prView, error) {
	v := &prView{PR: pr}
	if head, err := repo.Head(pr.SourceBranch); err == nil {
		v.HeadSha = head
	}
	apps, err := s.store.Approvals(pr.ID)
	if err != nil {
		return nil, err
	}
	v.Approvals = []approvalView{}
	for _, a := range apps {
		v.Approvals = append(v.Approvals, approvalView{PRApproval: a, Current: a.CommitSha == v.HeadSha})
	}
	comments, err := s.store.Comments(pr.ID)
	if err != nil {
		return nil, err
	}
	v.CommentCount = len(comments)
	if withMergeability && pr.State == "open" {
		if check, err := repo.CheckMerge(pr.TargetBranch, pr.SourceBranch); err == nil {
			v.Mergeable = &check.Mergeable
			v.Conflicts = check.Conflicts
		}
	}
	return v, nil
}

func (s *Server) prByNumber(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) *store.PR {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad PR number")
		return nil
	}
	pr, err := s.store.PRByNumber(repo.Key(), n)
	if errors.Is(err, store.ErrNotFound) {
		jsonError(w, http.StatusNotFound, "PR not found")
		return nil
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return nil
	}
	return pr
}

// GET /api/repos/{repo}/prs?state=open
func (s *Server) listPRs(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	prs, err := s.store.ListPRs(repo.Key(), state)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []*prView{}
	for _, pr := range prs {
		v, err := s.prView(repo, pr, false)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, v)
	}
	jsonOK(w, out)
}

// POST /api/repos/{repo}/prs {title, body, source, target}
func (s *Server) createPR(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	var body struct{ Title, Body, Source, Target string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" || body.Source == "" {
		jsonError(w, http.StatusBadRequest, "title and source are required")
		return
	}
	if body.Target == "" {
		body.Target = repo.Cfg.DefaultBranch
	}
	if body.Source == body.Target {
		jsonError(w, http.StatusBadRequest, "source and target are the same branch")
		return
	}
	if !repo.BranchExists(body.Source) || !repo.BranchExists(body.Target) {
		jsonError(w, http.StatusNotFound, "branch not found")
		return
	}
	// dirty source worktree → the client prompts to commit first
	st, err := repo.Status(body.Source)
	if err != nil {
		gitFail(w, err)
		return
	}
	if len(st.Dirty) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "uncommitted changes on " + body.Source, "code": "dirty", "dirty": st.Dirty})
		return
	}
	if existing, err := s.store.OpenPRForBranch(repo.Key(), body.Source); err == nil {
		jsonError(w, http.StatusConflict, "open PR #"+strconv.Itoa(existing.Number)+" already exists for "+body.Source)
		return
	}
	u := auth.UserFrom(r.Context())
	pr, err := s.store.CreatePR(repo.Key(), body.Title, body.Body, body.Source, body.Target, u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v, _ := s.prView(repo, pr, true)
	jsonOK(w, v)
}

// GET /api/repos/{repo}/prs/{n}
func (s *Server) getPR(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	pr := s.prByNumber(w, r, repo)
	if pr == nil {
		return
	}
	v, err := s.prView(repo, pr, true)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, v)
}

// GET /api/repos/{repo}/prs/{n}/diff
func (s *Server) getPRDiff(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	pr := s.prByNumber(w, r, repo)
	if pr == nil {
		return
	}
	var files []gitx.DiffFile
	var err error
	if pr.State == "merged" && pr.MergedCommit != "" {
		files, err = repo.DiffRange(pr.MergedCommit+"^1", pr.MergedCommit)
	} else {
		files, err = repo.DiffRange(pr.TargetBranch, pr.SourceBranch)
	}
	if err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]any{"files": files})
}

// GET|POST /api/repos/{repo}/prs/{n}/comments
func (s *Server) prComments(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	pr := s.prByNumber(w, r, repo)
	if pr == nil {
		return
	}
	if r.Method == http.MethodGet {
		comments, err := s.store.Comments(pr.ID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		head, _ := repo.Head(pr.SourceBranch)
		type cv struct {
			store.PRComment
			Outdated bool `json:"outdated"`
		}
		out := []cv{}
		for _, c := range comments {
			out = append(out, cv{PRComment: c, Outdated: c.AnchoredCommit != "" && c.AnchoredCommit != head})
		}
		jsonOK(w, out)
		return
	}
	var body struct {
		Body string `json:"body"`
		Path string `json:"path"`
		Line int    `json:"line"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Body == "" {
		jsonError(w, http.StatusBadRequest, "comment body required")
		return
	}
	u := auth.UserFrom(r.Context())
	anchor := ""
	if body.Path != "" {
		anchor, _ = repo.Head(pr.SourceBranch)
	}
	id, err := s.store.AddComment(pr.ID, u.ID, body.Path, body.Line, anchor, body.Body)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]int64{"id": id})
}

// POST /api/repos/{repo}/prs/{n}/approve
func (s *Server) approvePR(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	pr := s.prByNumber(w, r, repo)
	if pr == nil {
		return
	}
	head, err := repo.Head(pr.SourceBranch)
	if err != nil {
		gitFail(w, err)
		return
	}
	u := auth.UserFrom(r.Context())
	if err := s.store.Approve(pr.ID, u.ID, head); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	v, _ := s.prView(repo, pr, false)
	jsonOK(w, v)
}

// POST /api/repos/{repo}/prs/{n}/merge {strategy}
func (s *Server) mergePR(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	pr := s.prByNumber(w, r, repo)
	if pr == nil {
		return
	}
	if pr.State != "open" {
		jsonError(w, http.StatusConflict, "PR is "+pr.State)
		return
	}
	var body struct{ Strategy string }
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Strategy == "" {
		body.Strategy = "merge"
	}
	u := auth.UserFrom(r.Context())
	message := "Merge PR #" + strconv.Itoa(pr.Number) + ": " + pr.Title
	if body.Strategy == "squash" {
		message = pr.Title + " (PR #" + strconv.Itoa(pr.Number) + ", squashed)"
	}
	sha, check, err := repo.Merge(pr.TargetBranch, pr.SourceBranch, message, u.Name, u.Email, body.Strategy)
	if err != nil {
		gitFail(w, err)
		return
	}
	if check != nil && !check.Mergeable {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "merge conflicts", "conflicts": check.Conflicts})
		return
	}
	if err := s.store.SetPRState(pr.ID, "merged", sha); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// a merged personal workspace resets onto the new default-branch head so
	// it stays perpetually reusable (its worktree is clean — PRs require it)
	if _, err := s.store.WorkspaceOwner(repo.Key(), pr.SourceBranch); err == nil {
		if err := repo.ResetBranchFF(pr.SourceBranch, sha); err != nil {
			log.Printf("workspace reset %s after merge: %v", pr.SourceBranch, err)
		}
	}
	s.publish("merge", repo.Key(), pr.TargetBranch)
	jsonOK(w, map[string]string{"mergedCommit": sha})
}

// POST /api/repos/{repo}/prs/{n}/close
func (s *Server) closePR(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	pr := s.prByNumber(w, r, repo)
	if pr == nil {
		return
	}
	if pr.State != "open" {
		jsonError(w, http.StatusConflict, "PR is "+pr.State)
		return
	}
	if err := s.store.SetPRState(pr.ID, "closed", ""); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
