package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"

	"specquill/server/internal/ai"
	"specquill/server/internal/auth"
	"specquill/server/internal/authz"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
)

// GET /api/speccy/info?repo=&branch= — capability probe. When a project is
// resolvable (explicit ?repo=, else the deployment's sole project) it also
// reports the grounded reference sources feeding that project's speccy
// context, resolved from ?branch= (default branch when absent).
func (s *Server) speccyInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]any{"enabled": s.ai != nil}
	if s.ai != nil {
		info["model"] = s.ai.Model()
		if proj := s.speccyProject(r); proj != nil {
			names := []string{}
			_, grounded := s.resolveSources(r, proj, r.URL.Query().Get("branch"))
			for _, src := range grounded {
				names = append(names, src.Name)
			}
			info["groundedSources"] = names
		}
	}
	jsonOK(w, info)
}

// speccyProject resolves the project the info probe reports on: the ?repo=
// project when given, otherwise the deployment's first project. Best-effort
// (nil on any miss) — the info endpoint degrades to enabled/model only.
func (s *Server) speccyProject(r *http.Request) *project.Project {
	ps, err := s.store.Projects()
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
	repo, ok := s.gitm(r).Repo(target.RepoID)
	if !ok {
		return nil
	}
	return project.New(repo, target.ProjectID, target.ContentRoot, false)
}

// POST /api/repos/{repo}/speccy/chat {messages, focusPath?, branch?} → SSE
// stream. /api/speccy/chat is the legacy alias (the sole project).
func (s *Server) speccyChatAlias(w http.ResponseWriter, r *http.Request) {
	if repo, ok := s.soleProject(w, r); ok {
		s.speccyChat(w, r, repo)
	}
}

func (s *Server) speccyChat(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	var body struct {
		Messages  []ai.Message `json:"messages"`
		FocusPath string       `json:"focusPath"`
		Branch    string       `json:"branch"`
		// AllowEdits opts the conversation into the write tools; the server
		// still refuses protected branches regardless of what the client asks
		AllowEdits bool `json:"allowEdits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Messages) == 0 {
		jsonError(w, http.StatusBadRequest, "messages required")
		return
	}
	branch := repo.ResolveRef(body.Branch)
	files, err := repo.Snapshot(branch)
	if err != nil {
		gitFail(w, err)
		return
	}
	sources, grounded := s.resolveSources(r, repo, body.Branch)
	instructions := ""
	if cfg := inRepoConfig(repo, body.Branch); cfg != nil {
		instructions = cfg.Speccy.Instructions
	}
	writable := body.AllowEdits && repo.Writable() && !repo.Repo.Cfg.IsProtected(branch)

	system := ai.GroundingPrompt(files, grounded, body.FocusPath, s.ai.GroundingBudget(), instructions)
	system += ai.ToolRules // read_file/ask_user are always registered
	system += modelRules(files)
	if len(sources) > 0 {
		names := make([]string, 0, len(sources))
		for _, src := range sources {
			names = append(names, "~"+src.Name)
		}
		sort.Strings(names)
		system += "\nSelected reference sources — explore them with list_files/search/read_file even when not excerpted above: " + strings.Join(names, ", ") + "\n"
	}
	if writable {
		system += ai.EditingRules
	}
	msgs := append([]ai.Message{{Role: "system", Content: system}}, body.Messages...)

	stream, ok := startSSE(w)
	if !ok {
		return
	}
	defer stream.Close()
	send := stream.Send

	// binary sketches never enter the text snapshot — surface them in the
	// listing anyway so the model can discover and read_file their scenes
	if entries, err := repo.Tree(branch); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Path, ".excalidraw.png") {
				if _, ok := files[e.Path]; !ok {
					files[e.Path] = ""
				}
			}
		}
	}
	tb := &speccyToolbox{repo: repo, branch: branch, writable: writable, sources: sources, files: files,
		publish: func() { s.publish("save", repo.Key(), branch) }}
	onCall := func(tc ai.ToolCall, result string, execErr error) error {
		if execErr != nil {
			log.Printf("speccy chat [%s@%s]: tool %s failed: %v", repo.ID, branch, tc.Function.Name, execErr)
		}
		if tc.Function.Name == "ask_user" {
			return nil // the ask event below carries the question
		}
		var a struct{ Path, From, To string }
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &a)
		path := a.Path
		if path == "" && a.From != "" {
			path = a.From + " → " + a.To // move_file carries from/to instead
		}
		ev := map[string]string{"name": tc.Function.Name, "path": path, "status": "ok"}
		if execErr != nil {
			ev["status"] = "error"
			ev["detail"] = execErr.Error()
		}
		send(map[string]any{"tool": ev})
		return nil
	}
	resume, pending, err := s.ai.StreamTools(ai.WithLabel(r.Context(), "speccy chat "+repo.ID), msgs, tb.specs(files), tb.exec,
		func(delta string) error {
			send(map[string]string{"delta": delta})
			return nil
		}, onCall)
	if err != nil {
		// the SSE error event reaches the panel; the log line is for ops —
		// "the chat just stopped" must always leave a trace somewhere
		log.Printf("speccy chat [%s@%s]: %v", repo.ID, branch, err)
		send(map[string]string{"error": err.Error()})
		return
	}
	if pending != nil {
		// a question for the human: hand the client everything it needs to
		// resume statelessly — the appended messages plus the open call id
		var q struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		}
		_ = json.Unmarshal([]byte(pending.Function.Arguments), &q)
		send(map[string]any{
			"ask":    map[string]any{"callId": pending.ID, "question": q.Question, "options": q.Options},
			"resume": resume,
		})
	}
	send(map[string]bool{"done": true})
}

type draftEdit struct {
	Path    string `json:"path"`
	Search  string `json:"search"`
	Replace string `json:"replace"`
}

// POST /api/speccy/draft {changePath, files, branch?}
// Asks the model for surgical edits and applies them as *uncommitted saves*
// on a speccy branch — the human reviews via status → commit → PR.
func (s *Server) speccyDraftAlias(w http.ResponseWriter, r *http.Request) {
	if repo, ok := s.soleProject(w, r); ok {
		s.speccyDraft(w, r, repo)
	}
}

func (s *Server) speccyDraft(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	var body struct {
		ChangePath string   `json:"changePath"`
		Files      []string `json:"files"`  // impacted paths, resolved by the client from the model
		Branch     string   `json:"branch"` // target branch; default: speccy/<change-name>
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ChangePath == "" || len(body.Files) == 0 {
		jsonError(w, http.StatusBadRequest, "changePath and files required")
		return
	}

	branch := body.Branch
	if branch == "" {
		name := strings.TrimSuffix(body.ChangePath[strings.LastIndex(body.ChangePath, "/")+1:], ".md")
		branch = "speccy/" + name
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

	// the draft flow writes documents too — give it the same authoring
	// guidance (skills + workspace instructions) as the chat
	authoring := ""
	if snap, err := repo.Snapshot(branch); err == nil {
		instr := ""
		if cfg := inRepoConfig(repo, branch); cfg != nil {
			instr = cfg.Speccy.Instructions
		}
		authoring = ai.AuthoringRules(snap, instr)
	}
	var draft struct {
		Summary string      `json:"summary"`
		Edits   []draftEdit `json:"edits"`
	}
	if err := s.completeJSON(ai.WithLabel(r.Context(), "speccy draft "+body.ChangePath),
		ai.DraftPrompt(changeContent, allowed, authoring), &draft); err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
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

// resolveSources resolves the speccy's reference sources for a project: its
// EFFECTIVE references (selection ∩ catalog), each read as a read-only
// snapshot of the source's default branch (filtered to the reference's
// paths). ALL selected sources are tool-reachable (read_file/list_files/
// search); the `grounding: true` subset is ADDITIONALLY prompt-stuffed. The
// selection is read from the branch the request works on (worktree edits
// included), so config changes take effect before they merge; the D5 trust
// boundary holds because a selection can never reach an uncataloged source
// (catalog mode) and in-repo definitions stay bound by the host allowlist +
// the caller's own token (forge-PAT mode). Best-effort: any failure yields
// no sources.
func (s *Server) resolveSources(r *http.Request, proj *project.Project, ref string) (all, grounded []ai.GroundingSource) {
	cfg := inRepoConfig(proj, ref)
	if cfg == nil {
		return nil, nil
	}
	mgr := s.gitm(r)
	var refs []project.EffectiveReference
	if s.patMode() {
		refs, _ = project.EffectiveReferencesInRepo(cfg)
		s.registerUserSources(mgr, s.tok(r))
		// branch-defined sources aren't on the default branch yet — register
		// them here (same validation gate as everywhere else)
		for _, sd := range cfg.Sources {
			s.registerSourceDef(mgr, sd)
		}
	} else {
		catalog, err := s.store.Sources()
		if err != nil || len(catalog) == 0 {
			return nil, nil
		}
		kinds := map[string]string{}
		for _, src := range catalog {
			kinds[src.Name] = src.Kind
		}
		refs, _ = project.EffectiveReferences(cfg, kinds)
	}
	for _, ref := range refs {
		repo, ok := mgr.Repo(ref.Source)
		if !ok {
			continue
		}
		if s.patMode() && repo.EnsureCloned(s.tok(r)) != nil {
			continue // token cannot reach this source — proceed without it
		}
		snap := s.sourceSnapshot(ref.Source, repo)
		if snap == nil {
			continue
		}
		files := filterByPaths(snap, ref.Paths)
		if len(files) == 0 {
			continue
		}
		src := ai.GroundingSource{Name: ref.Source, Files: files}
		all = append(all, src)
		if ref.Grounding {
			grounded = append(grounded, src)
		}
	}
	return all, grounded
}

// sourceSnapshot returns a read-only snapshot of a source's default branch,
// cached by (repo key, head SHA): the content only changes when the branch
// moves, so repeated requests never re-snapshot an unchanged source.
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

// soleProject resolves the deployment's first project — the legacy
// /api/speccy/* alias routes use it; per-project routes carry {repo} and
// resolve normally.
func (s *Server) soleProject(w http.ResponseWriter, r *http.Request) (*project.Project, bool) {
	ps, err := s.store.Projects()
	if err != nil || len(ps) == 0 {
		jsonError(w, http.StatusInternalServerError, "no project configured")
		return nil, false
	}
	// same gate as the writableH speccy routes: speccy drafts write
	u := auth.UserFrom(r.Context())
	if s.effectiveRepoRole(u, ps[0].RepoID) < authz.Editor {
		jsonError2(w, http.StatusForbidden, "requires editor role", "role_forbidden")
		return nil, false
	}
	repo, ok := s.gitm(r).Repo(ps[0].RepoID)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "project repo not initialized")
		return nil, false
	}
	if !s.cloneReady(w, repo, s.tok(r)) {
		return nil, false
	}
	return project.New(repo, ps[0].ProjectID, ps[0].ContentRoot, false), true
}

// POST /api/repos/{repo}/speccy/title {text} — name a chat after its first
// exchange on the fast one-shot tier. Purely cosmetic: any failure leaves the
// client's deterministic fallback title in place.
func (s *Server) postSpeccyTitle(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" {
		jsonError(w, http.StatusBadRequest, "text required")
		return
	}
	if len(body.Text) > 2000 {
		body.Text = body.Text[:2000]
	}
	reply, err := s.ai.QuickComplete(r.Context(), []ai.Message{
		{Role: "system", Content: "You label chat conversations in a requirements-engineering workspace. Reply with ONLY a 3-6 word title for the conversation — no quotes, no punctuation at the end."},
		{Role: "user", Content: body.Text},
	})
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	title := sanitizeOneShot(reply)
	if len(title) > 60 {
		title = title[:60]
	}
	jsonOK(w, map[string]string{"title": title})
}

// POST /api/repos/{repo}/commit-message?branch= — draft a commit message from
// the uncommitted diff on the fast one-shot tier (ai.quick_model).
func (s *Server) postCommitMessage(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
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
