package api

import (
	"net/http"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
)

func (s *Server) listRepos(w http.ResponseWriter, r *http.Request) {
	type repoInfo struct {
		ID                string   `json:"id"`
		Kind              string   `json:"kind"` // project | source
		Mode              string   `json:"mode"` // legacy alias of kind
		ContentRoot       string   `json:"contentRoot,omitempty"`
		OKF               bool     `json:"okf,omitempty"`      // source is an OKF bundle
		Importer          string   `json:"importer,omitempty"` // non-git source kind (url|openapi|confluence)
		SyncStatus        string   `json:"syncStatus,omitempty"`
		SyncError         string   `json:"syncError,omitempty"`
		DefaultBranch     string   `json:"defaultBranch"`
		ProtectedBranches []string `json:"protectedBranches"`
		SyncedAt          string   `json:"syncedAt,omitempty"`
		Role              string   `json:"role"` // caller's effective role (viewer|member|admin)
	}
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	u := auth.UserFrom(r.Context())
	rootOf := map[string]string{}
	if projects, err := s.store.TenantProjects(t.ID); err == nil {
		for _, p := range projects {
			rootOf[p.RepoID] = p.ContentRoot
		}
	}
	grantedNames := map[string]bool{}
	grantedKind := map[string]string{}
	if granted, err := s.store.TenantGrantedSources(t.ID); err == nil {
		for _, src := range granted {
			grantedNames[src.Name] = true
			grantedKind[src.Name] = src.Kind
		}
	}
	syncs, _ := s.store.TenantSourceSyncs(t.ID)
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
		// repos the caller has no effective role on are invisible (REQ-020:
		// grant-only users see exactly their granted repos)
		role := s.effectiveRepoRole(u, t, repo.Cfg.ID)
		if roleRank[role] < roleRank["viewer"] {
			continue
		}
		info := repoInfo{
			Role: role,
			ID:                repo.Cfg.ID,
			Kind:              kind,
			Mode:              string(repo.Cfg.Mode),
			ContentRoot:       rootOf[repo.Cfg.ID],
			DefaultBranch:     repo.Cfg.DefaultBranch,
			ProtectedBranches: repo.Cfg.ProtectedBranches,
		}
		if kind == "source" {
			info.OKF = s.sourceIsOKF(t.Slug, repo.Cfg.ID)
			if k := grantedKind[repo.Cfg.ID]; k != "" && k != "git" {
				info.Importer = k
			}
			if rec, ok := syncs[repo.Cfg.ID]; ok {
				info.SyncStatus, info.SyncError = rec.Status, rec.Error
			}
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

func (s *Server) getHistory(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	path := r.URL.Query().Get("path")
	if path == "" {
		jsonError(w, http.StatusBadRequest, "path is required")
		return
	}
	entries, err := repo.FileHistory(r.URL.Query().Get("ref"), path, 0)
	if err != nil {
		gitFail(w, err)
		return
	}
	if entries == nil {
		entries = []gitx.HistoryEntry{}
	}
	jsonOK(w, entries)
}

func (s *Server) listBranches(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	branches, err := repo.Branches()
	if err != nil {
		gitFail(w, err)
		return
	}
	jsonOK(w, branches)
}
