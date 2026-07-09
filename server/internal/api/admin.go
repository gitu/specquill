package api

// Management API (config-split plan, phase 2): projects, sources and grants
// administered at runtime, persisted as managed_by='api' rows that survive
// boot reconciliation. All mutations are stage-gated: tenant `admin` role.

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/store"
)

// roleH gates a handler on a minimum tenant role (viewer < member < admin).
func (s *Server) roleH(minRole string, h http.HandlerFunc) http.HandlerFunc {
	rank := map[string]int{"viewer": 0, "member": 1, "admin": 2}
	return func(w http.ResponseWriter, r *http.Request) {
		t, ok := s.tenant(w, r)
		if !ok {
			return
		}
		u := auth.UserFrom(r.Context())
		role, err := s.store.MemberRole(t.ID, u.ID)
		if err != nil || rank[role] < rank[minRole] {
			jsonError2(w, http.StatusForbidden, "requires "+minRole+" role", "role_forbidden")
			return
		}
		h(w, r)
	}
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type projectInfo struct {
	ID            string   `json:"id"`
	ContentRoot   string   `json:"contentRoot,omitempty"`
	DefaultBranch string   `json:"defaultBranch"`
	Protected     []string `json:"protectedBranches"`
	ManagedBy     string   `json:"managedBy"`
}

// GET /api/projects — the tenant's projects (the switcher's data source).
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	ps, err := s.store.TenantProjects(t.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := []projectInfo{}
	for _, p := range ps {
		info := projectInfo{ID: p.ProjectID, ContentRoot: p.ContentRoot, ManagedBy: p.ManagedBy}
		if repo, ok := s.git.Repo(t.Slug + "/" + p.RepoID); ok {
			info.DefaultBranch = repo.Cfg.DefaultBranch
			info.Protected = repo.Cfg.ProtectedBranches
		}
		out = append(out, info)
	}
	jsonOK(w, out)
}

// POST /api/projects {id, remote, contentRoot?, defaultBranch?, tokenEnv?}
// — admin only; clones the repo and registers the project (managed_by=api).
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	var body struct {
		ID            string `json:"id"`
		Remote        string `json:"remote"`
		ContentRoot   string `json:"contentRoot"`
		DefaultBranch string `json:"defaultBranch"`
		TokenEnv      string `json:"tokenEnv"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" || body.Remote == "" {
		jsonError(w, http.StatusBadRequest, "id and remote are required")
		return
	}
	if !idRe.MatchString(body.ID) {
		jsonError(w, http.StatusBadRequest, "id must be lowercase alphanumeric with ._-")
		return
	}
	if strings.Contains(body.ContentRoot, "..") {
		jsonError(w, http.StatusBadRequest, "contentRoot must not traverse")
		return
	}
	if _, err := s.store.TenantProject(t.ID, body.ID); err == nil {
		jsonError(w, http.StatusConflict, "project "+body.ID+" already exists")
		return
	}
	if body.DefaultBranch == "" {
		body.DefaultBranch = "main"
	}
	rc := config.RepoConfig{
		ID: body.ID, Mode: config.Writable, Remote: body.Remote,
		DefaultBranch: body.DefaultBranch, TokenEnv: body.TokenEnv,
		SyncInterval:      2 * time.Minute,
		ProtectedBranches: []string{body.DefaultBranch},
		ContentRoot:       strings.Trim(body.ContentRoot, "/"),
	}
	if _, err := s.git.AddRepo(t.Slug, rc); err != nil {
		jsonError(w, http.StatusBadGateway, "clone failed: "+err.Error())
		return
	}
	if err := s.store.UpsertTenantRepo(t.ID, store.TenantRepo{
		RepoID: body.ID, Mode: string(config.Writable), Remote: body.Remote, DefaultBranch: body.DefaultBranch,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.AddProject(store.Project{
		TenantID: t.ID, ProjectID: body.ID, RepoID: body.ID, ContentRoot: rc.ContentRoot,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.publish("repos-changed", t.Slug+"/"+body.ID, "")
	jsonOK(w, map[string]string{"id": body.ID})
}

// DELETE /api/projects/{id} — admin only; unregisters (clone stays on disk).
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	tp, err := s.store.TenantProject(t.ID, id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "unknown project")
		return
	}
	if tp.ManagedBy == "config" {
		jsonError(w, http.StatusConflict, "project is config-managed — remove it from specquill.yml")
		return
	}
	if err := s.store.DeleteProject(t.ID, id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.git.RemoveRepo(t.Slug + "/" + tp.RepoID)
	s.publish("repos-changed", t.Slug+"/"+id, "")
	jsonOK(w, map[string]bool{"ok": true})
}
