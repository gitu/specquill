package api

// Forge review threads: the read-only counterpart to direct merges. Merging
// in-app has no review step, so when a team reviews on GitLab/GitHub instead,
// this surfaces that conversation next to the branch it is about.
//
// Opt-in per project (`forge.kind`), never required, and never load-bearing:
// any failure degrades to an error string in the payload rather than breaking
// the page it decorates.

import (
	"net/http"
	"os"
	"sync"
	"time"

	"specquill/server/internal/forge"
	"specquill/server/internal/project"
)

// forgeCache keeps forge answers briefly so that re-renders (and several
// users on one branch) do not each spend a request against the host's rate
// limit. Short enough that a new comment shows up promptly.
const forgeTTL = 60 * time.Second

type forgeEntry struct {
	req *forge.Request
	err string
	at  time.Time
}

type forgeCache struct {
	mu sync.Mutex
	m  map[string]forgeEntry
}

func newForgeCache() *forgeCache { return &forgeCache{m: map[string]forgeEntry{}} }

func (c *forgeCache) get(key string) (forgeEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Since(e.at) > forgeTTL {
		return forgeEntry{}, false
	}
	return e, true
}

func (c *forgeCache) put(key string, e forgeEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e.at = time.Now()
	c.m[key] = e
}

// GET /api/repos/{repo}/forge/request?branch= — the open merge request for a
// branch and its comments. Always 200: `enabled:false` when the project has
// no forge configured, `request:null` when the branch has no open request.
func (s *Server) getForgeRequest(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	cfg := repo.Repo.Cfg.Forge
	if !cfg.Enabled() {
		jsonOK(w, map[string]any{"enabled": false})
		return
	}
	branch := repo.ResolveRef(r.URL.Query().Get("branch"))
	key := repo.Repo.Key() + "\x00" + branch
	if e, ok := s.forgeCache.get(key); ok {
		writeForge(w, e)
		return
	}

	var token string
	if cfg.TokenEnv != "" {
		token = os.Getenv(cfg.TokenEnv)
	}
	client, err := forge.New(cfg, repo.Repo.Cfg.Remote, token)
	if err != nil {
		// misconfiguration (e.g. a local remote) — report it, do not cache
		jsonOK(w, map[string]any{"enabled": true, "kind": cfg.Kind, "error": err.Error()})
		return
	}
	req, err := client.OpenRequest(r.Context(), branch)
	e := forgeEntry{req: req}
	if err != nil {
		e.err = err.Error()
	}
	s.forgeCache.put(key, e)
	writeForge(w, e)
}

func writeForge(w http.ResponseWriter, e forgeEntry) {
	out := map[string]any{"enabled": true, "request": e.req}
	if e.err != "" {
		out["error"] = e.err
	}
	jsonOK(w, out)
}
