package api

// Token-scoped dynamic projects (REQ-025). Opt-in per deployment
// (config `dynamic:`): a forge-PAT user opens any manifest-carrying
// repository on the deployment's forge that their own token can reach —
// `owner/repo[#name]` or a full remote URL. Everything here runs with the
// requesting user's token; nothing can mint access beyond it.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/forge"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
	"specquill/server/internal/store"
)

const dynPrefix = "dyn."

func (s *Server) dynamicEnabled() bool { return s.patMode() && s.cfg.Dynamic.Enabled }

// dynProjectID derives the stable project id from the forge's stable
// repository id and the declared subproject name (REQ-025.4) — never from
// spellings or paths, so renames converge.
//
// The id becomes a clone DIRECTORY NAME and reaches git argv, and both of its
// inputs are attacker-influenced (the request spelling, and the manifest of
// whatever repository is being opened). The idRe match is therefore the final
// gate here rather than a caller-side precondition: the returned value is
// provably a single lowercase path segment — no separators, no traversal, not
// option-shaped (same discipline as gitx.safeRelPath, and a CodeQL barrier).
func dynProjectID(forgeRepoID, name string) (string, error) {
	// each part on its own: idRe only anchors its first character, so a
	// segment like "-x" would ride along unnoticed inside the composed id
	if name != "" && !idRe.MatchString(name) {
		return "", fmt.Errorf("invalid subproject name %q", name)
	}
	if !idRe.MatchString(forgeRepoID) {
		return "", fmt.Errorf("invalid forge repository id %q", forgeRepoID)
	}
	id := dynPrefix + forgeRepoID
	if name != "" {
		id += "." + name
	}
	if !idRe.MatchString(id) {
		return "", fmt.Errorf("invalid project id %q", id)
	}
	return id, nil
}

// forgeHostName is the single host dynamic projects may live on
// (REQ-025.1): the deployment's own forge, nothing else.
func (s *Server) forgeHostName() string {
	if base := s.cfg.Auth.Forge.BaseURL; base != "" {
		if u, err := url.Parse(base); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	if s.cfg.Auth.Forge.Kind == forge.KindGitHub {
		return "github.com"
	}
	return "gitlab.com"
}

// forgePathRe is the POSITIVE allowlist for a forge repository path: two or
// more `/`-separated segments, each starting alphanumeric and continuing in
// [A-Za-z0-9._-]. Written as an allowlist rather than a list of refusals
// because this value is interpolated into forge API URLs and reaches git
// argv: it excludes traversal (a segment cannot start with "."), empty
// segments, option-shaped leading "-", URL syntax ("?", "#", "@", ":") and
// every control character in one rule. GitLab's nested groups are why more
// than two segments are allowed; GitHub is pinned to exactly two below.
var forgePathRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(?:/[A-Za-z0-9][A-Za-z0-9._-]*)+$`)

// parseDynSpec splits `owner/repo[.git][#name]` — or the same as a full URL
// on the deployment's forge host — into the forge path and subproject name.
func (s *Server) parseDynSpec(spec string) (path, name string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", fmt.Errorf("repository is required")
	}
	if i := strings.IndexByte(spec, '#'); i >= 0 {
		spec, name = spec[:i], spec[i+1:]
	}
	if strings.Contains(spec, "://") {
		u, err := url.Parse(spec)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return "", "", fmt.Errorf("remote must be an http(s) URL on %s", s.forgeHostName())
		}
		if !strings.EqualFold(u.Hostname(), s.forgeHostName()) {
			return "", "", fmt.Errorf("repositories may only be opened on %s (got %s)", s.forgeHostName(), u.Hostname())
		}
		spec = strings.Trim(u.Path, "/")
	}
	spec = strings.TrimSuffix(strings.Trim(spec, "/"), ".git")
	// the allowlist is the final gate: everything downstream (forge API URLs,
	// git argv) may assume a well-formed path from here on
	if !forgePathRe.MatchString(spec) {
		return "", "", fmt.Errorf("repository must be named owner/repo")
	}
	if s.cfg.Auth.Forge.Kind == forge.KindGitHub && strings.Count(spec, "/") != 1 {
		return "", "", fmt.Errorf("github repositories are named owner/repo")
	}
	return spec, name, nil
}

// dynH gates a handler on the deployment's dynamic-projects opt-in.
func (s *Server) dynH(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.dynamicEnabled() {
			jsonError2(w, http.StatusNotFound, "dynamic projects are not enabled on this deployment", "dynamic_disabled")
			return
		}
		h(w, r)
	}
}

// GET /api/dynamic — feature discovery for the SPA (served even when off).
func (s *Server) dynamicInfo(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"enabled": s.dynamicEnabled()}
	if s.dynamicEnabled() {
		out["search"] = s.cfg.Dynamic.Search
		out["host"] = s.forgeHostName()
		out["budget"] = int64(s.cfg.Dynamic.UserBudget)
	}
	jsonOK(w, out)
}

// GET /api/dynamic/search?q= — forge repository search with the caller's own
// token. A separate opt-in on top of the feature (REQ-025.2): server-exposure
// control, not secrecy.
func (s *Server) dynamicSearch(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Dynamic.Search {
		jsonError2(w, http.StatusForbidden, "repository search is not enabled on this deployment", "search_disabled")
		return
	}
	client := forge.NewHost(s.cfg.Auth.Forge.Kind, s.cfg.Auth.Forge.BaseURL, s.tok(r))
	repos, err := client.SearchRepos(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		jsonError(w, http.StatusBadGateway, "forge search failed: "+err.Error())
		return
	}
	jsonOK(w, map[string]any{"repos": repos})
}

// POST /api/dynamic/open {spec} — resolve the repository through the forge,
// read its root manifest, and register the chosen workspace as a per-user
// project. The clone happens with the caller's token; a repository the token
// cannot reach yields an error and nothing lands on disk (REQ-025.3).
func (s *Server) dynamicOpen(w http.ResponseWriter, r *http.Request) {
	var body struct{ Spec string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	path, name, err := s.parseDynSpec(body.Spec)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	u := auth.UserFrom(r.Context())

	// budget first — opening must not start work the budget forbids (REQ-025.5)
	used := gitx.DirSize(s.userScopeDir(u.ID))
	if used >= int64(s.cfg.Dynamic.UserBudget) {
		jsonError2(w, http.StatusForbidden,
			fmt.Sprintf("your storage budget is exhausted (%d of %d bytes) — reclaim checkouts first", used, int64(s.cfg.Dynamic.UserBudget)),
			"budget_exceeded")
		return
	}

	client := forge.NewHost(s.cfg.Auth.Forge.Kind, s.cfg.Auth.Forge.BaseURL, s.tok(r))
	info, err := client.ResolveRepo(r.Context(), path)
	if err != nil {
		var se *forge.StatusError
		if errors.As(err, &se) && (se.StatusCode == http.StatusNotFound || se.StatusCode == http.StatusForbidden || se.StatusCode == http.StatusUnauthorized) {
			jsonError2(w, http.StatusNotFound, "repository not found — or your token has no access to it", "repo_unreachable")
			return
		}
		jsonError(w, http.StatusBadGateway, "forge lookup failed: "+err.Error())
		return
	}
	if info.Role == "none" || info.Role == "" {
		jsonError2(w, http.StatusNotFound, "repository not found — or your token has no access to it", "repo_unreachable")
		return
	}
	if info.DefaultBranch == "" {
		info.DefaultBranch = "main"
	}

	// the manifest gate (REQ-025.1): a root .specquill/config.yml on the
	// default branch is the repository's consent to being a workspace host
	yml, err := client.RepoFile(r.Context(), info.Path, info.DefaultBranch, ".specquill/config.yml")
	if err != nil {
		var se *forge.StatusError
		if errors.As(err, &se) && se.StatusCode == http.StatusNotFound {
			jsonError2(w, http.StatusConflict,
				"this repository does not declare SpecQuill workspaces — commit a root .specquill/config.yml first", "no_manifest")
			return
		}
		jsonError(w, http.StatusBadGateway, "manifest read failed: "+err.Error())
		return
	}
	manifest, err := project.ParseConfig(yml)
	if err != nil {
		jsonError2(w, http.StatusConflict, "the repository's .specquill/config.yml does not parse: "+err.Error(), "bad_manifest")
		return
	}

	contentRoot := ""
	switch {
	case len(manifest.Projects) == 0:
		// no projects list: the repository root is the single workspace
		if name != "" {
			jsonError2(w, http.StatusNotFound,
				"this repository declares no subprojects — open it without #"+name, "unknown_subproject")
			return
		}
	case name == "" && len(manifest.Projects) == 1:
		name, contentRoot = manifest.Projects[0].Name, manifest.Projects[0].Root
	case name == "":
		choices := []map[string]string{}
		for _, e := range manifest.Projects {
			choices = append(choices, map[string]string{"name": e.Name, "root": e.Root})
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "this repository declares several workspaces — pick one",
			"code":  "choose_project", "choices": choices,
		})
		return
	default:
		found := false
		for _, e := range manifest.Projects {
			if e.Name == name {
				contentRoot, found = e.Root, true
				break
			}
		}
		if !found {
			jsonError2(w, http.StatusNotFound, "no subproject "+name+" in the repository's manifest", "unknown_subproject")
			return
		}
	}
	projectID, err := dynProjectID(info.ID, name)
	if err != nil {
		jsonError2(w, http.StatusConflict,
			"manifest subproject name must be lowercase alphanumeric with ._-", "bad_manifest")
		return
	}
	contentRoot = strings.Trim(contentRoot, "/")
	if strings.Contains(contentRoot, "..") {
		jsonError2(w, http.StatusConflict, "manifest root must not traverse", "bad_manifest")
		return
	}

	spelling := info.Path
	if name != "" {
		spelling += "#" + name
	}
	up := store.UserProject{
		UserID: u.ID, ProjectID: projectID, ForgeRepoID: info.ID,
		Name: name, Spelling: spelling, Remote: info.Remote, ContentRoot: contentRoot,
		DefaultBranch: info.DefaultBranch, Role: info.Role,
	}
	if err := s.store.UpsertUserProject(up); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// materialize now, with the caller's token — a failing clone leaves the
	// row (retry is cheap) but reports honestly
	mgr := s.gitm(r)
	repo := s.registerDynRepo(mgr, up)
	if err := repo.EnsureCloned(s.tok(r)); err != nil {
		jsonError2(w, http.StatusBadGateway, "clone failed: "+err.Error(), "clone_failed")
		return
	}
	s.store.TouchClone(u.ID, scopeName(u.ID), up.ProjectID)
	s.publish("repos-changed", up.ProjectID, "")
	jsonOK(w, map[string]any{
		"id": up.ProjectID, "name": name, "spelling": spelling, "root": contentRoot,
		"readonly": up.Role == "viewer", "role": up.Role,
	})
}

// registerDynRepo registers a user's dynamic project into their manager.
// Mode is always Writable — the per-repo role (the user's forge permission,
// REQ-025.3) gates writes, and viewer rows surface as read-only projects.
func (s *Server) registerDynRepo(mgr *gitx.Manager, up store.UserProject) *gitx.Repo {
	return mgr.RegisterRepo(config.RepoConfig{
		ID: up.ProjectID, Mode: config.Writable, Remote: up.Remote,
		DefaultBranch:     up.DefaultBranch,
		ProtectedBranches: []string{up.DefaultBranch},
	})
}

// registerUserDynamic makes all of a user's dynamic projects resolvable in
// their manager (listing and lazy access after a server restart).
func (s *Server) registerUserDynamic(mgr *gitx.Manager, userID int64) {
	if !s.dynamicEnabled() {
		return
	}
	ups, err := s.store.UserProjects(userID)
	if err != nil {
		return
	}
	for _, up := range ups {
		s.registerDynRepo(mgr, up)
	}
}

func scopeName(userID int64) string { return fmt.Sprintf("u%d", userID) }

func (s *Server) userScopeDir(userID int64) string {
	return filepath.Join(s.cfg.DataDir, "repos", scopeName(userID))
}

// cloneDir locates one clone inside a user's own scope. repoID is
// client-supplied, so the idRe match gates the RETURN here — the path is
// provably built from a single lowercase segment that cannot escape the
// scope directory.
func (s *Server) cloneDir(userID int64, repoID string) (string, error) {
	if !idRe.MatchString(repoID) {
		return "", fmt.Errorf("invalid id %q", repoID)
	}
	return filepath.Join(s.userScopeDir(userID), repoID), nil
}

// checkoutEntry is one row of the per-user checkout overview (REQ-025.9).
type checkoutEntry struct {
	RepoID   string `json:"repoId"`
	Kind     string `json:"kind"` // project | source | dynamic
	Spelling string `json:"spelling,omitempty"`
	Role     string `json:"role,omitempty"`
	Bytes    int64  `json:"bytes"`
	LastUsed int64  `json:"lastUsed"`
	Unsynced bool   `json:"unsynced"`
	// Materialized is false for a dynamic project whose clone was reclaimed
	// (the entry survives; reopening re-clones — REQ-025.6).
	Materialized bool `json:"materialized"`
}

// GET /api/dynamic/checkouts — everything materialized for the caller, plus
// their non-materialized dynamic entries, with budget totals.
func (s *Server) dynamicCheckouts(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	scope := scopeName(u.ID)
	ups, _ := s.store.UserProjects(u.ID)
	dyn := map[string]store.UserProject{}
	for _, up := range ups {
		dyn[up.ProjectID] = up
	}
	configured := map[string]bool{}
	for _, p := range s.cfg.Projects {
		configured[p.ID] = true
	}

	entries := []checkoutEntry{}
	seen := map[string]bool{}
	for _, st := range gitx.ScanScope(s.userScopeDir(u.ID)) {
		e := checkoutEntry{
			RepoID: st.RepoID, Bytes: st.Bytes, Unsynced: st.Unsynced, Materialized: true,
			LastUsed: s.store.CloneUse(scope, st.RepoID),
		}
		if e.LastUsed == 0 {
			e.LastUsed = st.ModTime.Unix()
		}
		switch {
		case dyn[st.RepoID].ProjectID != "":
			up := dyn[st.RepoID]
			e.Kind, e.Spelling, e.Role = "dynamic", up.Spelling, up.Role
		case configured[st.RepoID]:
			e.Kind = "project"
		default:
			e.Kind = "source"
		}
		seen[st.RepoID] = true
		entries = append(entries, e)
	}
	for _, up := range ups {
		if seen[up.ProjectID] {
			continue
		}
		entries = append(entries, checkoutEntry{
			RepoID: up.ProjectID, Kind: "dynamic", Spelling: up.Spelling, Role: up.Role,
			LastUsed: up.LastUsed, Materialized: false,
		})
	}
	var used int64
	for _, e := range entries {
		used += e.Bytes
	}
	jsonOK(w, map[string]any{
		"checkouts": entries,
		"budget":    int64(s.cfg.Dynamic.UserBudget),
		"used":      used,
	})
}

// POST /api/dynamic/reclaim {id, force?, close?} — remove one of the
// caller's clones (their scope only). Unsynced clones need force — the
// explicit discard confirmation of REQ-025.5. close additionally drops a
// dynamic project entry from the open list.
func (s *Server) dynamicReclaim(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID    string `json:"id"`
		Force bool   `json:"force"`
		Close bool   `json:"close"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		jsonError(w, http.StatusBadRequest, "id is required")
		return
	}
	u := auth.UserFrom(r.Context())
	scope := scopeName(u.ID)
	dir, err := s.cloneDir(u.ID, body.ID)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid id")
		return
	}

	if _, err := os.Stat(filepath.Join(dir, "git", "HEAD")); err == nil {
		if err := gitx.ReclaimClone(dir, body.Force); err != nil {
			if errors.Is(err, gitx.ErrUnsynced) {
				jsonError2(w, http.StatusConflict,
					"this checkout holds unsynced work (uncommitted edits or unpushed commits) — pass force to discard it", "unsynced")
				return
			}
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.fleet.Invalidate(scope, body.ID)
		s.store.DropCloneUse(scope, body.ID)
	}
	if body.Close && strings.HasPrefix(body.ID, dynPrefix) {
		if err := s.store.DeleteUserProject(u.ID, body.ID); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.gitm(r).RemoveRepo(body.ID)
	}
	s.publish("repos-changed", body.ID, "")
	jsonOK(w, map[string]bool{"ok": true})
}

// cloneJanitor drives automatic reclamation (REQ-025.6): clones untouched
// for the idle period go; unsynced ones only past the retention cap.
func (s *Server) cloneJanitor() {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for range t.C {
		s.reclaimIdleClones()
	}
}

func (s *Server) reclaimIdleClones() {
	root := filepath.Join(s.cfg.DataDir, "repos")
	scopes, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, sc := range scopes {
		if !sc.IsDir() || !strings.HasPrefix(sc.Name(), "u") {
			continue
		}
		for _, st := range gitx.ScanScope(filepath.Join(root, sc.Name())) {
			last := time.Unix(s.store.CloneUse(st.Scope, st.RepoID), 0)
			if last.Unix() <= 0 {
				last = st.ModTime
			}
			idle := time.Since(last)
			if idle < s.cfg.Dynamic.IdleAfter {
				continue
			}
			if st.Unsynced && idle < s.cfg.Dynamic.UnsyncedRetention {
				continue
			}
			// past the retention cap, unsynced state is discarded by policy —
			// force exactly then, never earlier
			if err := gitx.ReclaimClone(st.Dir, st.Unsynced); err != nil {
				log.Printf("janitor: reclaim %s/%s: %v", st.Scope, st.RepoID, err)
				continue
			}
			s.fleet.Invalidate(st.Scope, st.RepoID)
			s.store.DropCloneUse(st.Scope, st.RepoID)
			log.Printf("janitor: reclaimed idle clone %s/%s (idle %s, unsynced=%v)",
				st.Scope, st.RepoID, idle.Round(time.Hour), st.Unsynced)
		}
	}
}
