package api

import (
	"net/http"
	"specquill/server/internal/project"
	"time"

)

func (s *Server) listRepos(w http.ResponseWriter, r *http.Request) {
	type repoInfo struct {
		ID                string   `json:"id"`
		Kind              string   `json:"kind"` // project | source
		Mode              string   `json:"mode"` // legacy alias of kind
		ContentRoot       string   `json:"contentRoot,omitempty"`
		OKF               bool     `json:"okf,omitempty"` // source is an OKF bundle
		DefaultBranch     string   `json:"defaultBranch"`
		ProtectedBranches []string `json:"protectedBranches"`
		SyncedAt          string   `json:"syncedAt,omitempty"`
	}
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	rootOf := map[string]string{}
	if projects, err := s.store.TenantProjects(t.ID); err == nil {
		for _, p := range projects {
			rootOf[p.RepoID] = p.ContentRoot
		}
	}
	grantedNames := map[string]bool{}
	if granted, err := s.store.TenantGrantedSources(t.ID); err == nil {
		for _, src := range granted {
			grantedNames[src.Name] = true
		}
	}
	var out []repoInfo
	for _, repo := range s.git.Repos() {
		if repo.Tenant() != t.Slug {
			continue
		}
		kind := "source"
		if repo.Writable() {
			kind = "project"
		}
		// ungranted sources are invisible (browsing is grant-gated)
		if kind == "source" && !grantedNames[repo.Cfg.ID] {
			continue
		}
		info := repoInfo{
			ID:                repo.Cfg.ID,
			Kind:              kind,
			Mode:              string(repo.Cfg.Mode),
			ContentRoot:       rootOf[repo.Cfg.ID],
			DefaultBranch:     repo.Cfg.DefaultBranch,
			ProtectedBranches: repo.Cfg.ProtectedBranches,
		}
		if kind == "source" {
			info.OKF = s.sourceIsOKF(t.Slug, repo.Cfg.ID)
		}
		if t := repo.LastFetch(); !t.IsZero() {
			info.SyncedAt = t.UTC().Format(time.RFC3339)
		}
		out = append(out, info)
	}
	jsonOK(w, out)
}

func (s *Server) getTree(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	entries, err := repo.Tree(r.URL.Query().Get("ref"))
	if err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, entries)
}

func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	ref := repo.ResolveRef(r.URL.Query().Get("ref"))
	files, err := repo.Snapshot(ref)
	if err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]any{"ref": ref, "files": files})
}

func (s *Server) getFile(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	var content, sha string
	var err error
	if r.URL.Query().Get("at") == "head" {
		// committed baseline from the object db, ignoring worktree state
		content, sha, err = repo.FileAt(r.URL.Query().Get("ref"), r.PathValue("path"))
	} else {
		content, sha, err = repo.File(r.URL.Query().Get("ref"), r.PathValue("path"))
	}
	if err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, map[string]string{"content": content, "sha": sha})
}

func (s *Server) listBranches(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	branches, err := repo.Branches()
	if err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, branches)
}
