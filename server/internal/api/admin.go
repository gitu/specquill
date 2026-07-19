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
	"specquill/server/internal/okf"
	"specquill/server/internal/project"
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

// tokenEnvRe allowlists the env-var names the tenant API may point git
// credentials at. Without it, a tenant admin could name any host env var
// (e.g. DATABASE_URL) as tokenEnv and pair it with an attacker-controlled
// remote to exfiltrate it via git's credential helper. Operators who set
// token_env in the server YAML are unaffected — that path never comes
// through here — but must name git-token vars with the SPECQUILL_ prefix
// to expose them to the API.
var tokenEnvRe = regexp.MustCompile(`^SPECQUILL_[A-Z0-9_]+$`)

type projectInfo struct {
	ID            string                       `json:"id"`
	ContentRoot   string                       `json:"contentRoot,omitempty"`
	DefaultBranch string                       `json:"defaultBranch"`
	Protected     []string                     `json:"protectedBranches"`
	ManagedBy     string                       `json:"managedBy"`
	References    []project.EffectiveReference `json:"references"`
	Warnings      []string                     `json:"warnings,omitempty"`
}

// GET /api/projects — the tenant's projects with their EFFECTIVE references
// (stage-3 selection ∩ stage-2 grants, config read from the default branch).
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
	granted, _ := s.store.TenantGrantedSources(t.ID)
	kinds := map[string]string{}
	for _, src := range granted {
		kinds[src.Name] = src.Kind
	}
	out := []projectInfo{}
	for _, p := range ps {
		info := projectInfo{ID: p.ProjectID, ContentRoot: p.ContentRoot, ManagedBy: p.ManagedBy, References: []project.EffectiveReference{}}
		if repo, ok := s.git.Repo(t.Slug + "/" + p.RepoID); ok {
			info.DefaultBranch = repo.Cfg.DefaultBranch
			info.Protected = repo.Cfg.ProtectedBranches
			proj := project.New(repo, p.ProjectID, p.ContentRoot, false)
			// default branch only (D5): a feature branch cannot change the
			// reference selection until merged
			if yml, _, err := proj.FileAt(repo.Cfg.DefaultBranch, ".specquill/config.yml"); err == nil {
				if cfg, err := project.ParseConfig(yml); err == nil {
					refs, warnings := project.EffectiveReferences(cfg, kinds)
					if refs != nil {
						info.References = refs
					}
					info.Warnings = warnings
					for i, ref := range info.References {
						info.References[i].OKF = s.sourceIsOKF(t.Slug, ref.Source)
					}
				} else {
					info.Warnings = []string{err.Error()}
				}
			}
		}
		out = append(out, info)
	}
	jsonOK(w, out)
}

// POST /api/sources/{name}/sync — re-import a granted, importer-backed source
// now. Member-gated and grant-gated: a tenant can only trigger a source it has
// been granted (stage 2). Returns the fresh import status.
func (s *Server) syncSource(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	name := r.PathValue("name")
	if _, err := s.store.GrantedSource(t.ID, name); err != nil {
		jsonError2(w, http.StatusForbidden, "source "+name+" is not granted to this tenant", "source_forbidden")
		return
	}
	if s.importer == nil || !s.importer.Manages(t.Slug, name) {
		jsonError(w, http.StatusBadRequest, "source "+name+" is not an importer source")
		return
	}
	rec, err := s.importer.Sync(r.Context(), t.Slug, name)
	if err != nil {
		jsonError2(w, http.StatusBadGateway, err.Error(), "import_failed")
		return
	}
	s.publish("repos-changed", t.Slug+"/"+name, "")
	jsonOK(w, map[string]any{
		"name": rec.Name, "status": rec.Status, "fileCount": rec.FileCount, "headSha": rec.HeadSHA,
	})
}

// sourceIsOKF reports whether a source's default branch is an OKF bundle
// (root index.md declaring okf_version).
func (s *Server) sourceIsOKF(tenantSlug, name string) bool {
	repo, ok := s.git.Repo(tenantSlug + "/" + name)
	if !ok {
		return false
	}
	content, _, err := repo.FileAt(repo.Cfg.DefaultBranch, "index.md")
	return err == nil && okf.EnabledContent(content)
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
	// a remote starting with "-" would be parsed by git as an option
	// (e.g. --upload-pack executes commands) — refuse it outright
	if strings.HasPrefix(body.Remote, "-") {
		jsonError(w, http.StatusBadRequest, "invalid remote")
		return
	}
	if body.TokenEnv != "" && !tokenEnvRe.MatchString(body.TokenEnv) {
		jsonError(w, http.StatusBadRequest, "tokenEnv must be a SPECQUILL_-prefixed env var name")
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
