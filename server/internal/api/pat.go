package api

// Forge-PAT mode plumbing: per-user git managers and in-repo reference
// sources. In this mode every user gets their own clones (fetched with their
// own token), reference repos are DEFINED in the workspace's
// .specquill/config.yml, and access to each of them is whatever the user's
// own token can fetch from the forge.

import (
	"log"
	"net/http"
	"net/url"
	"time"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
)

// patMode reports whether the deployment authenticates with forge PATs.
func (s *Server) patMode() bool { return s.cfg.Auth.Forge.Enabled() }

// gitm returns the git manager serving this request: the shared one in
// local/dev mode, the caller's own (per-user clones) in forge-PAT mode.
// Managers hold no credentials — the token travels per operation, see tok.
func (s *Server) gitm(r *http.Request) *gitx.Manager {
	if !s.patMode() {
		return s.git
	}
	u := auth.UserFrom(r.Context())
	return s.fleet.ForUser(u.ID)
}

// tok is the git/forge credential for this request: the caller's own PAT in
// forge-PAT mode, "" in local/dev mode (where gitx falls back to token_env).
// Passing it per call — rather than parking it on the shared per-user
// manager — is what keeps concurrent requests with different tokens from
// interfering.
func (s *Server) tok(r *http.Request) string {
	if !s.patMode() {
		return ""
	}
	return auth.TokenFrom(r.Context())
}

// vaultJanitor reaps tokens whose session has idled out. Sessions slide
// their own expiry on every request, so an entry untouched for longer than
// the session TTL belongs to a session that can no longer authenticate.
func (s *Server) vaultJanitor() {
	maxIdle := 2 * s.cfg.Session.TTL
	if maxIdle < 30*time.Minute {
		maxIdle = 30 * time.Minute
	}
	for range time.Tick(10 * time.Minute) {
		if n := s.vault.PruneIdle(maxIdle); n > 0 {
			log.Printf("vault: reaped %d idle token(s)", n)
		}
	}
}

// registerUserSources reads every project's in-repo config (default branch
// only, D5) from the user's own clones and registers the `sources:`
// definitions as read-only repos in the user's manager. Cloning stays lazy —
// registration only makes the name resolvable. Best-effort: an unreadable
// project or config just contributes nothing.
func (s *Server) registerUserSources(mgr *gitx.Manager, token string) {
	if !s.patMode() {
		return
	}
	ps, err := s.store.Projects()
	if err != nil {
		return
	}
	for _, p := range ps {
		repo, ok := mgr.Repo(p.RepoID)
		if !ok || !repo.Writable() {
			continue
		}
		if repo.EnsureCloned(token) != nil {
			continue
		}
		proj := project.New(repo, p.ProjectID, p.ContentRoot, false)
		yml, _, err := proj.FileAt(repo.Cfg.DefaultBranch, ".specquill/config.yml")
		if err != nil {
			continue
		}
		cfg, err := project.ParseConfig(yml)
		if err != nil {
			continue
		}
		for _, sd := range cfg.Sources {
			s.registerSourceDef(mgr, sd)
		}
	}
}

// sourceDefError validates one in-repo source definition. The config file is
// user-writable repo content, so its remotes are treated as hostile input:
// http(s) only (a filesystem path could read arbitrary local git repos on
// the server), no embedded credentials, and the host must be on the
// deployment's allowlist (the forge, project remotes,
// auth.forge.allowed_source_hosts) — anything else could be offered users'
// tokens or probe the internal network. Returns "" when acceptable.
func (s *Server) sourceDefError(sd project.SourceDef) string {
	if sd.Name == "" || !idRe.MatchString(sd.Name) {
		return "name must be lowercase alphanumeric with ._-"
	}
	u, err := url.Parse(sd.Remote)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "remote must be an http(s) URL"
	}
	if u.User != nil {
		return "remote must not embed credentials"
	}
	if !s.cfg.SourceHostAllowed(u.Hostname()) {
		return "remote host " + u.Hostname() + " is not allowed (forge, project hosts, or auth.forge.allowed_source_hosts)"
	}
	return ""
}

// registerSourceDef registers one validated in-repo source definition.
func (s *Server) registerSourceDef(mgr *gitx.Manager, sd project.SourceDef) {
	if s.sourceDefError(sd) != "" {
		return
	}
	if _, exists := mgr.Repo(sd.Name); exists {
		return
	}
	branch := sd.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	mgr.RegisterRepo(config.RepoConfig{
		ID: sd.Name, Mode: config.ReadOnly, Remote: sd.Remote, DefaultBranch: branch,
	})
}

// registerStoreRepo registers a store-known repo into a per-user manager —
// api-managed projects added after the manager was created (fleet managers
// only learn config repos at birth).
func (s *Server) registerStoreRepo(mgr *gitx.Manager, repoID string) (*gitx.Repo, bool) {
	rows, err := s.store.RepoRows()
	if err != nil {
		return nil, false
	}
	for _, tr := range rows {
		if tr.RepoID != repoID {
			continue
		}
		mode := config.ReadOnly
		if tr.Mode == string(config.Writable) {
			mode = config.Writable
		}
		return mgr.RegisterRepo(config.RepoConfig{
			ID: tr.RepoID, Mode: mode, Remote: tr.Remote, DefaultBranch: tr.DefaultBranch,
			ProtectedBranches: []string{tr.DefaultBranch},
		}), true
	}
	return nil, false
}

// inRepoConfig parses a project's .specquill/config.yml from its default
// branch (nil when absent or unparsable).
func inRepoConfig(proj *project.Project) *project.Config {
	yml, _, err := proj.FileAt(proj.Cfg.DefaultBranch, ".specquill/config.yml")
	if err != nil {
		return nil
	}
	cfg, err := project.ParseConfig(yml)
	if err != nil {
		return nil
	}
	return cfg
}
