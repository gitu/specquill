package api

// Forge-PAT mode plumbing: per-user git managers and in-repo reference
// sources. In this mode every user gets their own clones (fetched with their
// own token), reference repos are DEFINED in the workspace's
// .specquill/config.yml, and access to each of them is whatever the user's
// own token can fetch from the forge.

import (
	"net/http"
	"strings"

	"specquill/server/internal/auth"
	"specquill/server/internal/config"
	"specquill/server/internal/gitx"
	"specquill/server/internal/project"
)

// patMode reports whether the deployment authenticates with forge PATs.
func (s *Server) patMode() bool { return s.cfg.Auth.Forge.Enabled() }

// gitm returns the git manager serving this request: the shared one in
// local/dev mode, the caller's own (with the session token applied) in
// forge-PAT mode.
func (s *Server) gitm(r *http.Request) *gitx.Manager {
	if !s.patMode() {
		return s.git
	}
	u := auth.UserFrom(r.Context())
	mgr := s.fleet.ForUser(u.ID)
	mgr.SetToken(auth.TokenFrom(r.Context()))
	return mgr
}

// registerUserSources reads every project's in-repo config (default branch
// only, D5) from the user's own clones and registers the `sources:`
// definitions as read-only repos in the user's manager. Cloning stays lazy —
// registration only makes the name resolvable. Best-effort: an unreadable
// project or config just contributes nothing.
func (s *Server) registerUserSources(mgr *gitx.Manager) {
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
		if repo.EnsureCloned() != nil {
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
			registerSourceDef(mgr, sd)
		}
	}
}

// registerSourceDef validates and registers one in-repo source definition.
// Only http(s) remotes are accepted: the config file is user-writable repo
// content, and a filesystem path here would let it read arbitrary local git
// repos on the server.
func registerSourceDef(mgr *gitx.Manager, sd project.SourceDef) {
	if sd.Name == "" || !idRe.MatchString(sd.Name) {
		return
	}
	if !strings.HasPrefix(sd.Remote, "https://") && !strings.HasPrefix(sd.Remote, "http://") {
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
