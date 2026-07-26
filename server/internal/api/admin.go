package api

// Management API (config-split plan, phase 2): projects and sources
// administered at runtime, persisted as managed_by='api' rows that survive
// boot reconciliation. All mutations require the `admin` deployment role.

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/authz"
	"specquill/server/internal/config"
	"specquill/server/internal/okf"
	"specquill/server/internal/project"
	"specquill/server/internal/store"
)

// roleH gates a handler on a minimum DEPLOYMENT role (the authz ladder
// applied to users.role — management, not per-repo access).
func (s *Server) roleH(minRole authz.Role, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := auth.UserFrom(r.Context())
		role, err := s.deployRole(u)
		if err != nil || role < minRole {
			jsonError2(w, http.StatusForbidden, "requires "+minRole.String()+" role", "role_forbidden")
			return
		}
		h(w, r)
	}
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type projectInfo struct {
	ID            string                       `json:"id"`
	ContentRoot   string                       `json:"contentRoot,omitempty"`
	DefaultBranch string                       `json:"defaultBranch"`
	Protected     []string                     `json:"protectedBranches"`
	ManagedBy     string                       `json:"managedBy"`
	References    []project.EffectiveReference `json:"references"`
	Warnings      []string                     `json:"warnings,omitempty"`
}

// GET /api/projects — the deployment's projects with their EFFECTIVE
// references (in-repo selection ∩ catalog, config read from the default
// branch).
func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := s.store.Projects()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	catalog, _ := s.store.Sources()
	kinds := map[string]string{}
	for _, src := range catalog {
		kinds[src.Name] = src.Kind
	}
	out := []projectInfo{}
	for _, p := range ps {
		info := projectInfo{ID: p.ProjectID, ContentRoot: p.ContentRoot, ManagedBy: p.ManagedBy, References: []project.EffectiveReference{}}
		if repo, ok := s.git.Repo(p.RepoID); ok {
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
						info.References[i].OKF = s.sourceIsOKF(ref.Source)
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

// POST /api/sources/{name}/sync — re-import a cataloged, importer-backed
// source now. Member-gated; the catalog is the availability gate. Returns
// the fresh import status.
func (s *Server) syncSource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := s.store.SourceByName(name); err != nil {
		jsonError2(w, http.StatusForbidden, "source "+name+" is not in the catalog", "source_forbidden")
		return
	}
	if s.importer == nil || !s.importer.Manages(name) {
		jsonError(w, http.StatusBadRequest, "source "+name+" is not an importer source")
		return
	}
	rec, err := s.importer.Sync(r.Context(), name)
	if err != nil {
		jsonError2(w, http.StatusBadGateway, err.Error(), "import_failed")
		return
	}
	s.publish("repos-changed", name, "")
	jsonOK(w, map[string]any{
		"name": rec.Name, "status": rec.Status, "fileCount": rec.FileCount, "headSha": rec.HeadSHA,
	})
}

// sourceIsOKF reports whether a source's default branch is an OKF bundle
// (root index.md declaring okf_version).
func (s *Server) sourceIsOKF(name string) bool {
	repo, ok := s.git.Repo(name)
	if !ok {
		return false
	}
	content, _, err := repo.FileAt(repo.Cfg.DefaultBranch, "index.md")
	return err == nil && okf.EnabledContent(content)
}

// POST /api/projects {id, remote, contentRoot?, defaultBranch?, tokenEnv?}
// — admin only; clones the repo and registers the project (managed_by=api).
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
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
	if _, err := s.store.Project(body.ID); err == nil {
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
	if _, err := s.git.AddRepo(rc); err != nil {
		jsonError(w, http.StatusBadGateway, "clone failed: "+err.Error())
		return
	}
	if err := s.store.UpsertRepoRow(store.RepoRow{
		RepoID: body.ID, Mode: string(config.Writable), Remote: body.Remote, DefaultBranch: body.DefaultBranch,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.AddProject(store.Project{
		ProjectID: body.ID, RepoID: body.ID, ContentRoot: rc.ContentRoot,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.publish("repos-changed", body.ID, "")
	jsonOK(w, map[string]string{"id": body.ID})
}

// DELETE /api/projects/{id} — admin only; unregisters (clone stays on disk).
func (s *Server) deleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tp, err := s.store.Project(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "unknown project")
		return
	}
	if tp.ManagedBy == "config" {
		jsonError(w, http.StatusConflict, "project is config-managed — remove it from specquill.yml")
		return
	}
	if err := s.store.DeleteProject(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.git.RemoveRepo(tp.RepoID)
	s.publish("repos-changed", id, "")
	jsonOK(w, map[string]bool{"ok": true})
}
