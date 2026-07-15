package api

// GitHub-App tenant management (repo-product/docs/specs/specs/multi-tenancy.md, phase B): roles in a
// github tenant are DERIVED from GitHub repo permissions — synced on demand
// with a TTL cache, never duplicated — and the repo picker turns installation
// repositories into workspaces (projects) or reference sources.

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/store"
)

// ---------------------------------------------------------------- role sync

const roleTTL = 5 * time.Minute

type roleCache struct {
	mu sync.Mutex
	m  map[string]roleEntry // "<tenantID>:<userID>"
}
type roleEntry struct {
	role    string // admin | member | viewer | "" (no access)
	expires time.Time
}

func newRoleCache() *roleCache { return &roleCache{m: map[string]roleEntry{}} }

func (c *roleCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || time.Now().After(e.expires) {
		return "", false
	}
	return e.role, true
}

func (c *roleCache) put(key, role string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = roleEntry{role: role, expires: time.Now().Add(roleTTL)}
}

// githubRole maps a GitHub repo permission onto a tenant role.
func githubRole(permission string) string {
	switch permission {
	case "admin":
		return "admin"
	case "write", "maintain":
		return "member"
	case "read", "triage":
		return "viewer"
	default:
		return ""
	}
}

// syncGitHubMemberships derives the user's role in every github tenant from
// their repo permissions (max across the tenant's repos), upserting or
// revoking memberships accordingly. Failures leave existing memberships
// untouched — GitHub being down must not lock anyone out mid-session.
func (s *Server) syncGitHubMemberships(u *store.User) {
	if s.ghApp == nil || u.Provider != "github" {
		return
	}
	login, err := s.store.UserLogin(u.ID)
	if err != nil || login == "" {
		return
	}
	tenants, err := s.store.TenantsByProvider("github")
	if err != nil {
		return
	}
	for _, ten := range tenants {
		key := ten.Slug + ":" + login
		role, cached := s.ghRoles.get(key)
		if !cached {
			repos, err := s.store.TenantRepos(ten.ID)
			if err != nil {
				continue
			}
			names := make([]string, 0, len(repos))
			for _, tr := range repos {
				if tr.GhFullName != "" {
					names = append(names, tr.GhFullName)
				}
			}
			// bootstrap: a fresh installation has no adopted repos yet, so
			// derive the role from the installation's candidates instead —
			// this is how the org admin becomes tenant admin and reaches
			// the repo picker at all (capped; any repo-admin qualifies)
			if len(names) == 0 {
				if cands, err := s.ghApp.Repos(ten.Installation); err == nil {
					for i, c := range cands {
						if i >= 10 {
							break
						}
						names = append(names, c.FullName)
					}
				}
			}
			resolved, failed := "", false
			rank := map[string]int{"viewer": 1, "member": 2, "admin": 3}
			for _, fullName := range names {
				perm, err := s.ghApp.Permission(ten.Installation, fullName, login)
				if err != nil {
					log.Printf("github role sync: %s @%s: %v", fullName, login, err)
					failed = true
					break
				}
				if r := githubRole(perm); rank[r] > rank[resolved] {
					resolved = r
				}
			}
			if failed {
				continue // keep whatever membership exists
			}
			role = resolved
			s.ghRoles.put(key, role)
		}
		cur, err := s.store.MemberRole(ten.ID, u.ID)
		switch {
		case role == "" && err == nil:
			_ = s.store.DeleteMember(ten.ID, u.ID)
		case role != "" && (err != nil || cur != role):
			if err := s.store.EnsureMember(ten.ID, u.ID, role); err == nil {
				_ = s.store.SetMemberRole(ten.ID, u.ID, role)
			}
		}
	}
}

// ------------------------------------------------------------- repo picker

type ghRepoInfo struct {
	FullName      string `json:"fullName"`
	Private       bool   `json:"private"`
	Description   string `json:"description,omitempty"`
	DefaultBranch string `json:"defaultBranch"`
	// State: "" (candidate), "workspace" or "reference"
	State string `json:"state,omitempty"`
	ID    string `json:"id,omitempty"` // the repo id inside the tenant, when added
}

// githubTenantFor gates the picker: the request's tenant must be a github
// tenant and the caller its admin (roleH already checked the role).
func (s *Server) githubTenantFor(w http.ResponseWriter, r *http.Request) (*store.Tenant, bool) {
	if s.ghApp == nil {
		jsonError(w, http.StatusNotFound, "github app not configured")
		return nil, false
	}
	t, ok := s.tenant(w, r)
	if !ok {
		return nil, false
	}
	if t.Provider != "github" || t.Installation == 0 {
		jsonError(w, http.StatusBadRequest, "tenant "+t.Slug+" is not a github installation")
		return nil, false
	}
	return t, true
}

// GET /api/github/repos — the installation's repositories with their state.
func (s *Server) listGitHubRepos(w http.ResponseWriter, r *http.Request) {
	t, ok := s.githubTenantFor(w, r)
	if !ok {
		return
	}
	candidates, err := s.ghApp.Repos(t.Installation)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	added := map[string]store.TenantRepo{} // gh_full_name (lower) → row
	if rows, err := s.store.TenantRepos(t.ID); err == nil {
		for _, tr := range rows {
			if tr.GhFullName != "" {
				added[strings.ToLower(tr.GhFullName)] = tr
			}
		}
	}
	out := make([]ghRepoInfo, 0, len(candidates))
	for _, c := range candidates {
		info := ghRepoInfo{
			FullName: c.FullName, Private: c.Private,
			Description: c.Description, DefaultBranch: c.DefaultBranch,
		}
		if tr, ok := added[strings.ToLower(c.FullName)]; ok {
			info.ID = tr.RepoID
			if tr.Mode == string(config.Writable) {
				info.State = "workspace"
			} else {
				info.State = "reference"
			}
		}
		out = append(out, info)
	}
	jsonOK(w, out)
}

// POST /api/github/repos {fullName, mode: workspace|reference} — adopt an
// installation repository into the tenant.
func (s *Server) addGitHubRepo(w http.ResponseWriter, r *http.Request) {
	t, ok := s.githubTenantFor(w, r)
	if !ok {
		return
	}
	var body struct {
		FullName string `json:"fullName"`
		Mode     string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.FullName == "" {
		jsonError(w, http.StatusBadRequest, "fullName is required")
		return
	}
	if body.Mode != "workspace" && body.Mode != "reference" {
		jsonError(w, http.StatusBadRequest, "mode must be workspace or reference")
		return
	}
	// only repos the installation actually grants — the picker can never
	// reach beyond what the org admin installed the app on
	candidates, err := s.ghApp.Repos(t.Installation)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	var cand *ghCandidate
	for _, c := range candidates {
		if strings.EqualFold(c.FullName, body.FullName) {
			cand = &ghCandidate{FullName: c.FullName, DefaultBranch: c.DefaultBranch, CloneURL: c.CloneURL}
			break
		}
	}
	if cand == nil {
		jsonError(w, http.StatusNotFound, body.FullName+" is not part of this installation")
		return
	}
	id := strings.ToLower(cand.FullName[strings.Index(cand.FullName, "/")+1:])
	if !idRe.MatchString(id) {
		jsonError(w, http.StatusBadRequest, "repository name does not map to a valid id: "+id)
		return
	}
	branch := cand.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	mode := config.ReadOnly
	if body.Mode == "workspace" {
		mode = config.Writable
	}
	if _, err := s.git.AddRepo(t.Slug, config.RepoConfig{
		ID: id, Mode: mode, Remote: cand.CloneURL, DefaultBranch: branch,
		SyncInterval:      2 * time.Minute,
		ProtectedBranches: []string{branch},
	}); err != nil {
		jsonError(w, http.StatusBadGateway, "clone failed: "+err.Error())
		return
	}
	if err := s.store.UpsertTenantRepo(t.ID, store.TenantRepo{
		RepoID: id, Mode: string(mode), Remote: cand.CloneURL,
		DefaultBranch: branch, GhFullName: cand.FullName,
	}); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	u := auth.UserFrom(r.Context())
	if body.Mode == "workspace" {
		if err := s.store.AddProject(store.Project{TenantID: t.ID, ProjectID: id, RepoID: id}); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if err := s.store.AddTenantSource(t.ID, id, cand.CloneURL, branch, u.ID); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.publish("repos-changed", t.Slug+"/"+id, "")
	jsonOK(w, map[string]string{"id": id, "mode": body.Mode})
}

type ghCandidate struct {
	FullName      string
	DefaultBranch string
	CloneURL      string
}

// DELETE /api/github/repos/{id} — remove an adopted repo from the tenant
// (the GitHub repository itself is untouched).
func (s *Server) removeGitHubRepo(w http.ResponseWriter, r *http.Request) {
	t, ok := s.githubTenantFor(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	_ = s.store.DeleteProject(t.ID, id)
	_ = s.store.DeleteTenantSource(t.ID, id)
	if err := s.store.DeleteTenantRepo(t.ID, id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.git.RemoveRepo(t.Slug + "/" + id)
	s.publish("repos-changed", t.Slug+"/"+id, "")
	jsonOK(w, map[string]bool{"ok": true})
}
