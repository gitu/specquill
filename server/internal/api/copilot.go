package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"specquill/server/internal/ai"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
)

// GET /api/copilot/info?repo= — capability probe. When a project is resolvable
// (explicit ?repo=, else the tenant's sole project) it also reports the grounded
// reference sources feeding that project's copilot context.
func (s *Server) copilotInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]any{"enabled": s.ai != nil}
	if s.ai != nil {
		info["model"] = s.ai.Model()
		if proj := s.copilotProject(r); proj != nil {
			names := []string{}
			for _, src := range s.groundingSources(r, proj) {
				names = append(names, src.Name)
			}
			info["groundedSources"] = names
		}
	}
	jsonOK(w, info)
}

// copilotProject resolves the project the info probe reports on: the ?repo=
// project when given, otherwise the tenant's first project. Best-effort (nil on
// any miss) — the info endpoint degrades to enabled/model only.
func (s *Server) copilotProject(r *http.Request) *project.Project {
	t := s.tenantQuiet(r)
	if t == nil {
		return nil
	}
	ps, err := s.store.TenantProjects(t.ID)
	if err != nil || len(ps) == 0 {
		return nil
	}
	target := ps[0]
	if id := r.URL.Query().Get("repo"); id != "" {
		found := false
		for _, p := range ps {
			if p.ProjectID == id {
				target, found = p, true
				break
			}
		}
		if !found {
			return nil
		}
	}
	repo, ok := s.git.Repo(t.Slug + "/" + target.RepoID)
	if !ok {
		return nil
	}
	return project.New(repo, target.ProjectID, target.ContentRoot, false)
}

// POST /api/repos/{repo}/copilot/chat {messages, focusPath?, branch?} → SSE
// stream. /api/copilot/chat is the legacy alias (tenant's sole project).
func (s *Server) copilotChatAlias(w http.ResponseWriter, r *http.Request) {
	if repo, ok := s.soleProject(w, r); ok {
		s.copilotChat(w, r, repo)
	}
}

func (s *Server) copilotChat(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "copilot is not configured (ai: in specquill.yml)")
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
	files, err := repo.Snapshot(repo.ResolveRef(body.Branch))
	if err != nil {
		gitFail(w, err)
		return
	}
	refs := s.groundingSources(r, repo)

	system := ai.GroundingPrompt(files, refs, body.FocusPath, s.ai.GroundingBudget())
	msgs := append([]ai.Message{{Role: "system", Content: system}}, body.Messages...)

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
func (s *Server) copilotDraftAlias(w http.ResponseWriter, r *http.Request) {
	if repo, ok := s.soleProject(w, r); ok {
		s.copilotDraft(w, r, repo)
	}
}

func (s *Server) copilotDraft(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "copilot is not configured (ai: in specquill.yml)")
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
func (s *Server) applyEdit(repo *project.Project, branch string, allowed map[string]string, e draftEdit) error {
	if strings.HasPrefix(e.Path, "~") {
		return fmt.Errorf("reference sources are read-only")
	}
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

// groundingSources resolves the copilot's grounded reference sources for a
// project: its EFFECTIVE references (default-branch selection ∩ tenant grants)
// with `grounding: true`, each read as a read-only snapshot of the granted
// source's default branch (filtered to the reference's paths). This is the D5
// trust boundary — selection is read from the default branch only and can never
// reach an ungranted source. Best-effort: any failure yields no grounding.
func (s *Server) groundingSources(r *http.Request, proj *project.Project) []ai.GroundingSource {
	t := s.tenantQuiet(r)
	if t == nil {
		return nil
	}
	granted, err := s.store.TenantGrantedSources(t.ID)
	if err != nil || len(granted) == 0 {
		return nil
	}
	kinds := map[string]string{}
	for _, src := range granted {
		kinds[src.Name] = src.Kind
	}
	// default branch only (D5): a feature branch cannot change the selection
	yml, _, err := proj.FileAt(proj.Cfg.DefaultBranch, ".specquill/config.yml")
	if err != nil {
		return nil
	}
	cfg, err := project.ParseConfig(yml)
	if err != nil {
		return nil
	}
	refs, _ := project.EffectiveReferences(cfg, kinds)
	var out []ai.GroundingSource
	for _, ref := range refs {
		if !ref.Grounding {
			continue
		}
		repo, ok := s.git.Repo(t.Slug + "/" + ref.Source)
		if !ok {
			continue
		}
		snap := s.sourceSnapshot(t.Slug+"/"+ref.Source, repo)
		if snap == nil {
			continue
		}
		files := filterByPaths(snap, ref.Paths)
		if len(files) == 0 {
			continue
		}
		out = append(out, ai.GroundingSource{Name: ref.Source, Files: files})
	}
	return out
}

// sourceSnapshot returns a read-only snapshot of a source's default branch,
// cached by (repo key, head SHA): the content only changes when the branch
// moves, so a live room's keystrokes never re-snapshot an unchanged source.
// Returns nil on any failure. The returned map must not be mutated (it is
// shared) — callers filter into a fresh map.
func (s *Server) sourceSnapshot(key string, repo *gitx.Repo) map[string]string {
	sha, err := repo.Head(repo.Cfg.DefaultBranch)
	if err != nil {
		return nil
	}
	ck := key + "@" + sha
	if files, ok := s.srcCache.get(ck); ok {
		return files
	}
	files, err := repo.Snapshot(repo.Cfg.DefaultBranch)
	if err != nil {
		return nil
	}
	s.srcCache.put(ck, files)
	return files
}

// srcCache is a bounded (FIFO-evicted) cache of source snapshots. Keys embed the
// head SHA, so a moved branch is a cache miss rather than stale content.
type srcCache struct {
	mu    sync.Mutex
	items map[string]map[string]string
	order []string
}

const srcCacheMax = 16

func newSrcCache() *srcCache { return &srcCache{items: map[string]map[string]string{}} }

func (c *srcCache) get(key string) (map[string]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.items[key]
	return v, ok
}

func (c *srcCache) put(key string, files map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		return
	}
	c.items[key] = files
	c.order = append(c.order, key)
	for len(c.order) > srcCacheMax {
		delete(c.items, c.order[0])
		c.order = c.order[1:]
	}
}

// filterByPaths keeps only files under one of the given path prefixes; an empty
// filter keeps everything (dropping sketch JSON and uploads either way).
func filterByPaths(files map[string]string, prefixes []string) map[string]string {
	out := make(map[string]string, len(files))
	for p, c := range files {
		if strings.HasSuffix(p, ".excalidraw") || strings.HasPrefix(p, "uploads/") {
			continue
		}
		if len(prefixes) == 0 {
			out[p] = c
			continue
		}
		for _, pre := range prefixes {
			if p == pre || strings.HasPrefix(p, strings.TrimSuffix(pre, "/")+"/") {
				out[p] = c
				break
			}
		}
	}
	return out
}

// soleProject resolves the tenant's first project — the legacy /api/copilot/*
// alias routes use it; per-project routes carry {repo} and resolve normally.
func (s *Server) soleProject(w http.ResponseWriter, r *http.Request) (*project.Project, bool) {
	t, ok := s.tenant(w, r)
	if !ok {
		return nil, false
	}
	ps, err := s.store.TenantProjects(t.ID)
	if err != nil || len(ps) == 0 {
		jsonError(w, http.StatusInternalServerError, "no project configured")
		return nil, false
	}
	repo, ok := s.git.Repo(t.Slug + "/" + ps[0].RepoID)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "project repo not initialized")
		return nil, false
	}
	return project.New(repo, ps[0].ProjectID, ps[0].ContentRoot, false), true
}

// POST /api/repos/{repo}/commit-message?branch= — draft a commit message from
// the uncommitted diff on the fast one-shot tier (ai.quick_model).
func (s *Server) postCommitMessage(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "copilot is not configured (ai: in specquill.yml)")
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
