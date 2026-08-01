package api

// The change feed: what actually changed in the workspace, read from git
// history rather than from documents a human remembered to write. `/log`
// lists commits, `/commit` explains one of them — the diff plus the semantic
// delta per document (which properties moved, which normative statements were
// added, dropped or reworded) — and `/commit/summary` puts the quick AI tier
// on top of that delta.

import (
	"net/http"
	"strconv"
	"strings"
	"sync"

	"specquill/server/internal/ai"
	"specquill/server/internal/delta"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
)

// summaryCache holds per-commit AI summaries. A commit is immutable, so
// unlike forgeCache there is no TTL — only a size bound so a long-running
// deployment cannot grow without limit.
const summaryCacheMax = 500

type summaryCache struct {
	mu sync.Mutex
	m  map[string]string
}

func newSummaryCache() *summaryCache { return &summaryCache{m: map[string]string{}} }

func (c *summaryCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *summaryCache) put(key, val string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= summaryCacheMax {
		// immutable values, so eviction order does not matter — drop a
		// handful to amortize the next inserts
		n := 0
		for k := range c.m {
			delete(c.m, k)
			if n++; n >= summaryCacheMax/10 {
				break
			}
		}
	}
	c.m[key] = val
}

// GET /api/repos/{repo}/log?ref=&since=&limit= — the workspace's commits,
// newest first, each with the paths it touched. The client classifies those
// paths through the workspace config; the server stays document-agnostic.
func (s *Server) getLog(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	q := r.URL.Query()
	if !gitx.ValidSince(q.Get("since")) {
		jsonError(w, http.StatusBadRequest, "since must be YYYY-MM-DD")
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	commits, err := repo.Log(q.Get("ref"), q.Get("since"), limit)
	if err != nil {
		gitFail(w, err)
		return
	}
	if commits == nil {
		commits = []gitx.Commit{}
	}
	jsonOK(w, commits)
}

// GET /api/repos/{repo}/commit?sha=&parent= — one commit in full: the file
// diffs (hunks included) and the semantic delta of every markdown document it
// touched. `parent` comes from the log payload; an empty one means a root
// commit (diffed against the empty tree).
func (s *Server) getCommit(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	sha := r.URL.Query().Get("sha")
	if sha == "" {
		jsonError(w, http.StatusBadRequest, "sha is required")
		return
	}
	files, err := repo.DiffCommit(r.URL.Query().Get("parent"), sha)
	if err != nil {
		gitFail(w, err)
		return
	}
	if files == nil {
		files = []gitx.DiffFile{}
	}
	jsonOK(w, map[string]any{"sha": sha, "files": files, "deltas": s.deltas(repo, r.URL.Query().Get("parent"), sha, files)})
}

// deltas reads both sides of every changed markdown document from the object
// database (never the worktree) and turns them into semantic deltas.
func (s *Server) deltas(repo *project.Project, parent, sha string, files []gitx.DiffFile) []delta.DocDelta {
	out := make([]delta.DocDelta, 0, len(files))
	for _, f := range files {
		if f.BinaryLike {
			out = append(out, delta.DocDelta{Path: f.Path, Status: f.Status, Plain: true})
			continue
		}
		var before, after string
		if f.Status != "A" && parent != "" {
			old := f.Path
			if f.OldPath != "" {
				old = f.OldPath
			}
			before, _, _ = repo.FileAt(parent, old)
		}
		if f.Status != "D" {
			after, _, _ = repo.FileAt(sha, f.Path)
		}
		out = append(out, delta.Diff(f.Path, f.Status, before, after))
	}
	return out
}

// GET /api/repos/{repo}/commit/summary?sha=&parent= — one or two sentences on
// what the commit did to the workspace, from the quick AI tier over the
// semantic delta (not the raw diff). Cached forever: commits do not change.
func (s *Server) getCommitSummary(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	if s.ai == nil {
		jsonError(w, http.StatusNotImplemented, "Speccy is not configured (ai: in specquill.yml)")
		return
	}
	sha := r.URL.Query().Get("sha")
	if sha == "" {
		jsonError(w, http.StatusBadRequest, "sha is required")
		return
	}
	key := repo.ID + "@" + sha
	if cached, ok := s.summaryCache.get(key); ok {
		jsonOK(w, map[string]any{"sha": sha, "summary": cached, "model": s.ai.QuickModel(), "cached": true})
		return
	}
	parent := r.URL.Query().Get("parent")
	files, err := repo.DiffCommit(parent, sha)
	if err != nil {
		gitFail(w, err)
		return
	}
	if len(files) == 0 {
		jsonError(w, http.StatusBadRequest, "commit touches nothing in this workspace")
		return
	}

	// prompt budget: the delta is already a summary, so completeness beats
	// verbosity here — truncate rather than stream a whole diff at the model
	const budget = 4000
	var b strings.Builder
	for _, d := range s.deltas(repo, parent, sha, files) {
		line := d.Summarize()
		if b.Len()+len(line) > budget {
			b.WriteString("… (truncated)\n")
			break
		}
		b.WriteString(line)
	}

	reply, err := s.ai.QuickComplete(r.Context(), []ai.Message{
		{Role: "system", Content: "You explain changes in a requirements-engineering workspace (markdown requirements, specs, data mappings) to a reviewer. Given the structural delta of one commit, reply with ONE or TWO plain sentences saying what changed in substance — which documents, and what it means (a tightened bound, a new obligation, a status moving on). No preamble, no bullet lists, no code fences. Prefer document ids (REQ-042) over file paths."},
		{Role: "user", Content: "Commit " + sha + "\n\n" + b.String()},
	})
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	summary := sanitizeOneShot(reply)
	s.summaryCache.put(key, summary)
	jsonOK(w, map[string]any{"sha": sha, "summary": summary, "model": s.ai.QuickModel()})
}
