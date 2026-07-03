package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"reqbase/server/internal/ai"
	"reqbase/server/internal/gitx"
)

// GET /api/copilot/info
func (s *Server) copilotInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]any{"enabled": s.ai != nil}
	if s.ai != nil {
		info["model"] = s.ai.Model()
	}
	jsonOK(w, info)
}

// POST /api/copilot/chat {messages, focusPath?, branch?} → SSE stream
func (s *Server) copilotChat(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "copilot is not configured (ai: in reqbase.yml)")
		return
	}
	var body struct {
		Messages  []ai.Message `json:"messages"`
		FocusPath string       `json:"focusPath"`
		Branch    string       `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) == 0 {
		jsonError(w, http.StatusBadRequest, "messages required")
		return
	}
	repo := s.writableRepo()
	if repo == nil {
		jsonError(w, http.StatusInternalServerError, "no writable repo")
		return
	}
	files, err := repo.Snapshot(repo.ResolveRef(body.Branch))
	if err != nil {
		gitFail(w, err)
		return
	}

	msgs := append([]ai.Message{{Role: "system", Content: ai.GroundingPrompt(files, body.FocusPath)}}, body.Messages...)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	send := func(v any) {
		raw, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", raw)
		flusher.Flush()
	}
	err = s.ai.Stream(r.Context(), msgs, func(delta string) error {
		send(map[string]string{"delta": delta})
		return nil
	})
	if err != nil {
		send(map[string]string{"error": err.Error()})
		return
	}
	send(map[string]bool{"done": true})
}

type draftEdit struct {
	Path    string `json:"path"`
	Search  string `json:"search"`
	Replace string `json:"replace"`
}

// POST /api/copilot/draft {changePath, files, branch?}
// Asks the model for surgical edits and applies them as *uncommitted saves*
// on a copilot branch — the human reviews via status → commit → PR.
func (s *Server) copilotDraft(w http.ResponseWriter, r *http.Request) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "copilot is not configured (ai: in reqbase.yml)")
		return
	}
	var body struct {
		ChangePath string   `json:"changePath"`
		Files      []string `json:"files"`  // impacted paths, resolved by the client from the model
		Branch     string   `json:"branch"` // target branch; default: copilot/<change-name>
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ChangePath == "" || len(body.Files) == 0 {
		jsonError(w, http.StatusBadRequest, "changePath and files required")
		return
	}
	repo := s.writableRepo()
	if repo == nil {
		jsonError(w, http.StatusInternalServerError, "no writable repo")
		return
	}

	branch := body.Branch
	if branch == "" {
		name := strings.TrimSuffix(body.ChangePath[strings.LastIndex(body.ChangePath, "/")+1:], ".md")
		branch = "copilot/" + name
	}
	if !repo.BranchExists(branch) {
		if err := repo.CreateBranch(branch, ""); err != nil {
			gitFail(w, err)
			return
		}
	}

	changeContent, _, err := repo.File(branch, body.ChangePath)
	if err != nil {
		gitFail(w, err)
		return
	}
	allowed := map[string]string{}
	for _, p := range body.Files {
		content, _, err := repo.File(branch, p)
		if err != nil {
			continue // impacted file may not exist (e.g. planned spec)
		}
		allowed[p] = content
	}
	if len(allowed) == 0 {
		jsonError(w, http.StatusBadRequest, "none of the impacted files exist on "+branch)
		return
	}

	reply, err := s.ai.Complete(r.Context(), ai.DraftPrompt(changeContent, allowed))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	var draft struct {
		Summary string      `json:"summary"`
		Edits   []draftEdit `json:"edits"`
	}
	if err := ai.ExtractJSON(reply, &draft); err != nil {
		jsonError(w, http.StatusBadGateway, "model reply was not valid edit JSON: "+err.Error())
		return
	}

	applied := []string{}
	failures := []string{}
	for _, e := range draft.Edits {
		if err := s.applyEdit(repo, branch, allowed, e); err != nil {
			failures = append(failures, e.Path+": "+err.Error())
			continue
		}
		applied = append(applied, e.Path)
	}
	jsonOK(w, map[string]any{
		"branch":   branch,
		"summary":  draft.Summary,
		"applied":  applied,
		"failures": failures,
	})
}

// applyEdit validates a search/replace against the *current* file state on the
// branch and saves the result (uncommitted). allowed limits editable paths.
func (s *Server) applyEdit(repo *gitx.Repo, branch string, allowed map[string]string, e draftEdit) error {
	e.Path = normalizePath(e.Path, allowed)
	if _, ok := allowed[e.Path]; !ok {
		return fmt.Errorf("not in the impacted file set")
	}
	if e.Search == "" || e.Search == e.Replace {
		return fmt.Errorf("empty or no-op edit")
	}
	content, sha, err := repo.File(branch, e.Path)
	if err != nil {
		return err
	}
	switch strings.Count(content, e.Search) {
	case 0:
		if strings.Contains(content, e.Replace) {
			return nil // already applied (e.g. a re-run) — idempotent no-op
		}
		return fmt.Errorf("search text not found")
	case 1:
	default:
		return fmt.Errorf("search text is not unique")
	}
	next := strings.Replace(content, e.Search, e.Replace, 1)
	_, err = repo.SaveFile(branch, e.Path, next, sha)
	return err
}

// normalizePath maps a sloppy model-emitted path (stray prefixes like
// "<file path>/…" or "./…") onto the allowed set. Safe: only ever returns a
// path we offered for editing.
func normalizePath(p string, allowed map[string]string) string {
	if _, ok := allowed[p]; ok {
		return p
	}
	for a := range allowed {
		if p == "./"+a || strings.HasSuffix(p, "/"+a) {
			return a
		}
	}
	return p
}

func (s *Server) writableRepo() *gitx.Repo {
	for _, r := range s.git.Repos() {
		if r.Writable() {
			return r
		}
	}
	return nil
}

// POST /api/repos/{repo}/commit-message?branch= — draft a commit message from
// the uncommitted diff on the fast one-shot tier (ai.quick_model).
func (s *Server) postCommitMessage(w http.ResponseWriter, r *http.Request, repo *gitx.Repo) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "copilot is not configured (ai: in reqbase.yml)")
		return
	}
	branch := r.URL.Query().Get("branch")
	files, err := repo.DiffWorktree(branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	if len(files) == 0 {
		jsonError(w, http.StatusBadRequest, "nothing to commit")
		return
	}

	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s %s (+%d -%d)\n", f.Status, f.Path, f.Additions, f.Deletions)
	}
	b.WriteString("\n")
	budget := 6000 // prompt-size cap: summaries beat completeness here
	for _, f := range files {
		if f.BinaryLike || f.Hunks == nil || budget <= 0 {
			continue
		}
		fmt.Fprintf(&b, "--- %s\n", f.Path)
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				if l.Op == " " {
					continue
				}
				line := l.Op + l.Text + "\n"
				if budget -= len(line); budget < 0 {
					break
				}
				b.WriteString(line)
			}
		}
	}

	reply, err := s.ai.QuickComplete(r.Context(), []ai.Message{
		{Role: "system", Content: "You write git commit messages for a requirements-engineering workspace (markdown documents: requirements, specs, data mappings). Reply with the commit message ONLY — no quotes, no code fences, no commentary. First line: imperative summary, at most 72 characters. Add a short body (1-3 bullet lines) only when the change spans several concerns."},
		{Role: "user", Content: "Uncommitted changes:\n\n" + b.String()},
	})
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, map[string]string{"message": sanitizeOneShot(reply), "model": s.ai.QuickModel()})
}

// sanitizeOneShot strips reasoning tags, code fences and wrapping quotes from
// a one-shot reply — thinking-tuned models tend to decorate their output.
func sanitizeOneShot(s string) string {
	if i := strings.Index(s, "</think>"); i >= 0 {
		s = s[i+len("</think>"):]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if len(s) > 1 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '`' && s[len(s)-1] == '`') {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}
