// Package config loads and validates the specquill server configuration.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"specquill/server/internal/forge"
)

type RepoMode string

const (
	Writable RepoMode = "writable"
	ReadOnly RepoMode = "readonly"
)

// RepoConfig describes a physical clone. It remains gitx's internal currency;
// user-facing configuration is `projects:` + `sources:` (legacy `repos:` lists
// still load and are normalized — see normalize()).
type RepoConfig struct {
	ID                string        `yaml:"id"`
	Mode              RepoMode      `yaml:"mode"`
	Remote            string        `yaml:"remote"`
	DefaultBranch     string        `yaml:"default_branch"`
	TokenEnv          string        `yaml:"token_env"`
	SyncInterval      time.Duration `yaml:"sync_interval"`
	ProtectedBranches []string      `yaml:"protected_branches"` // default: [default_branch]
	ContentRoot       string        `yaml:"-"`                  // set from the owning project
	// Mirror marks a remote-less source repo whose content is materialized by
	// an importer (kind url|openapi|confluence), not cloned/fetched from a
	// remote. ensure() inits it empty; the importer.Runner commits snapshots.
	Mirror bool `yaml:"-"`
	// Forge optionally reads merge-request review threads from the git host
	// (read-only; see internal/forge). Copied from the owning project.
	Forge forge.Config `yaml:"-"`
}

// SourceConfig is a catalog entry: a named external source that projects may
// reference. Sources are read-only downstream, always. Credentials come from
// the environment via token_env — never from the DB or in-repo config.
type SourceConfig struct {
	Name          string        `yaml:"name"`
	Kind          string        `yaml:"kind"`   // git | url | openapi | confluence
	Remote        string        `yaml:"remote"` // git: clone URL; else: importer endpoint
	TokenEnv      string        `yaml:"token_env"`
	DefaultBranch string        `yaml:"default_branch"`
	SyncInterval  time.Duration `yaml:"sync_interval"`
	// importer-specific (non-git kinds):
	URLs  []string `yaml:"urls"`  // url: explicit page list (else Remote is the single page)
	Space string   `yaml:"space"` // confluence: space key to mirror
}

// IsGit reports whether the source is a plain git clone (vs an importer mirror).
func (s SourceConfig) IsGit() bool { return s.Kind == "" || s.Kind == "git" }

// ProjectConfig is a writable workspace: a git repo plus an optional
// content_root subfolder (monorepo case; "" = repo root).
type ProjectConfig struct {
	ID                string        `yaml:"id"`
	Remote            string        `yaml:"remote"`
	ContentRoot       string        `yaml:"content_root"`
	DefaultBranch     string        `yaml:"default_branch"`
	TokenEnv          string        `yaml:"token_env"`
	SyncInterval      time.Duration `yaml:"sync_interval"`
	ProtectedBranches []string      `yaml:"protected_branches"`
	// Forge (optional) shows the branch's open merge request and its comments
	// from the git host. Read-only and opt-in — set `kind` to enable.
	Forge forge.Config `yaml:"forge"`
}

// IsProtected reports whether direct writes/commits to branch are forbidden
// (such branches only move via merges from a workspace branch).
func (rc *RepoConfig) IsProtected(branch string) bool {
	for _, b := range rc.ProtectedBranches {
		if b == branch {
			return true
		}
	}
	return false
}

type GitConfig struct {
	CommitterName  string `yaml:"committer_name"`
	CommitterEmail string `yaml:"committer_email"`
}

// ForgeAuthConfig turns on forge-PAT authentication: users sign in with a
// personal access token from the deployment's git host. The token lives in
// the user's browser (localStorage) and, per session, in server RAM — never
// in the store. Identity, deployment role and git/forge credentials all come
// from that token.
type ForgeAuthConfig struct {
	Kind    string `yaml:"kind"`     // github | gitlab
	BaseURL string `yaml:"base_url"` // forge web base for self-hosted (e.g. https://gitlab.example.com)
	// TokenCreateURL overrides the derived "create a token" deep link shown on
	// the login page (prefilled name + scopes where the forge supports it).
	TokenCreateURL string `yaml:"token_create_url"`
	// Scopes the login page asks the user to grant; defaulted per kind
	// (gitlab: api; github: repo).
	Scopes []string `yaml:"scopes"`
	// AllowedSourceHosts extends the hosts in-repo `sources:` remotes may
	// name beyond the forge and the project remotes — e.g. a public mirror
	// host references are allowed to read from.
	AllowedSourceHosts []string `yaml:"allowed_source_hosts"`
}

func (f ForgeAuthConfig) Enabled() bool { return f.Kind != "" }

type LocalAuthConfig struct {
	Enabled bool `yaml:"enabled"`
}

// DevUser auto-authenticates every request as this identity — honored only
// when the server runs with the -dev flag.
type DevUser struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

type AuthConfig struct {
	Forge ForgeAuthConfig `yaml:"forge"`
	Local LocalAuthConfig `yaml:"local"`
	// AdminEmails bootstrap administration: users whose email matches
	// (case-insensitive, any provider) get the admin deployment role on
	// login. Without it a fresh deployment has members only and the
	// management API is unreachable.
	AdminEmails []string `yaml:"admin_emails"`
	DevUser     *DevUser `yaml:"dev_user"`
	// DefaultRole is the deployment role every authenticated user is
	// auto-enrolled with on the authz ladder: editor (default, self-host
	// semantics), viewer, maintainer, admin, or none — with none, users reach
	// only repos explicitly granted to them (REQ-020, restricted deployments).
	DefaultRole string `yaml:"default_role"`
}

type SessionConfig struct {
	TTL          time.Duration `yaml:"ttl"`
	CookieSecure bool          `yaml:"cookie_secure"`
}

// DatabaseConfig locates the SQLite store (users, sessions, workspace
// claims) — a single file, by default inside data_dir, so a deployment
// has no service to operate beside the binary. Load() resolves Path to an
// absolute location; back it with a persistent volume, since the same disk
// already holds the worktree drafts.
type DatabaseConfig struct {
	Path string `yaml:"path"` // default: <data_dir>/specquill.db
}

// AIConfig points the copilot at any OpenAI-compatible chat-completions API
// (OpenAI, Gemini's /v1beta/openai endpoint, Azure, Ollama, …).
type AIConfig struct {
	Enabled bool   `yaml:"enabled"`
	BaseURL string `yaml:"base_url"` // e.g. https://api.openai.com/v1
	Model   string `yaml:"model"`    // main model: chat + draft edits (thinking-class)
	// fast one-shot model for small tasks (commit messages, titles);
	// empty = fall back to Model
	QuickModel string `yaml:"quick_model"`
	APIKeyEnv  string `yaml:"api_key_env"` // empty = no Authorization header (local providers)
	// GroundingBudget caps the copilot system-prompt size in bytes
	// (0 = package default; grows automatically when references exist).
	GroundingBudget int `yaml:"grounding_budget"`
}

type Config struct {
	Listen   string          `yaml:"listen"`
	DataDir  string          `yaml:"data_dir"`
	BaseURL  string          `yaml:"base_url"`
	Database DatabaseConfig  `yaml:"database"`
	Projects []ProjectConfig `yaml:"projects"`
	Sources  []SourceConfig  `yaml:"sources"`
	Repos    []RepoConfig    `yaml:"repos"` // legacy shape — normalized into projects/sources
	Git      GitConfig       `yaml:"git"`
	Auth     AuthConfig      `yaml:"auth"`
	Session  SessionConfig   `yaml:"session"`
	AI       AIConfig        `yaml:"ai"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Listen: ":8080",
		Session: SessionConfig{
			// idle timeout: expiry slides on every authenticated request
			TTL:          10 * time.Minute,
			CookieSecure: true,
		},
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.Normalize()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// resolve relative paths against the config file's directory
	base := filepath.Dir(path)
	cfg.DataDir = absAgainst(base, cfg.DataDir)
	if cfg.Database.Path == "" {
		cfg.Database.Path = filepath.Join(cfg.DataDir, "specquill.db")
	} else {
		cfg.Database.Path = absAgainst(base, cfg.Database.Path)
	}
	for i := range cfg.Projects {
		if looksLikePath(cfg.Projects[i].Remote) {
			cfg.Projects[i].Remote = absAgainst(base, cfg.Projects[i].Remote)
		}
	}
	for i := range cfg.Sources {
		if looksLikePath(cfg.Sources[i].Remote) {
			cfg.Sources[i].Remote = absAgainst(base, cfg.Sources[i].Remote)
		}
	}
	for i := range cfg.Repos {
		r := &cfg.Repos[i]
		if looksLikePath(r.Remote) {
			r.Remote = absAgainst(base, r.Remote)
		}
	}
	return cfg, nil
}

// Normalize maps the legacy `repos:` shape onto projects/sources (writable →
// project at repo root; readonly → git source, browse-only until a project
// references it), then rebuilds cfg.Repos as the canonical clone registry
// (projects + git-kind sources) that gitx and the boot sync consume.
// Idempotent — Load calls it, and test fixtures that build Config literals
// may call it again.
func (c *Config) Normalize() {
	legacy := c.Repos
	if len(c.Projects) > 0 || len(c.Sources) > 0 {
		legacy = nil // already normalized (or v2 config); never map twice
	}
	for _, r := range legacy {
		switch r.Mode {
		case Writable:
			c.Projects = append(c.Projects, ProjectConfig{
				ID: r.ID, Remote: r.Remote, DefaultBranch: r.DefaultBranch,
				TokenEnv: r.TokenEnv, SyncInterval: r.SyncInterval,
				ProtectedBranches: r.ProtectedBranches,
			})
		case ReadOnly:
			c.Sources = append(c.Sources, SourceConfig{
				Name: r.ID, Kind: "git", Remote: r.Remote, TokenEnv: r.TokenEnv,
				DefaultBranch: r.DefaultBranch, SyncInterval: r.SyncInterval,
			})
		}
	}
	// defaults
	for i := range c.Projects {
		p := &c.Projects[i]
		// forge-PAT mode: the deployment's forge is every project's forge
		if c.Auth.Forge.Enabled() {
			if p.Forge.Kind == "" {
				p.Forge.Kind = c.Auth.Forge.Kind
			}
			if p.Forge.BaseURL == "" && c.Auth.Forge.BaseURL != "" {
				p.Forge.BaseURL = forgeAPIBase(c.Auth.Forge.Kind, c.Auth.Forge.BaseURL)
			}
		}
		if p.DefaultBranch == "" {
			p.DefaultBranch = "main"
		}
		if p.SyncInterval == 0 {
			p.SyncInterval = 2 * time.Minute
		}
		if len(p.ProtectedBranches) == 0 {
			p.ProtectedBranches = []string{p.DefaultBranch}
		}
		p.ContentRoot = cleanContentRoot(p.ContentRoot)
	}
	for i := range c.Sources {
		src := &c.Sources[i]
		if src.Kind == "" {
			src.Kind = "git"
		}
		if src.DefaultBranch == "" {
			src.DefaultBranch = "main"
		}
		if src.SyncInterval == 0 {
			src.SyncInterval = 5 * time.Minute
		}
	}
	// canonical clone registry: every project + every git source
	c.Repos = c.Repos[:0]
	for _, p := range c.Projects {
		f := p.Forge
		if f.TokenEnv == "" {
			f.TokenEnv = p.TokenEnv // the push/fetch token usually covers the API too
		}
		c.Repos = append(c.Repos, RepoConfig{
			ID: p.ID, Mode: Writable, Remote: p.Remote, DefaultBranch: p.DefaultBranch,
			TokenEnv: p.TokenEnv, SyncInterval: p.SyncInterval,
			ProtectedBranches: p.ProtectedBranches, ContentRoot: p.ContentRoot,
			Forge: f,
		})
	}
	for _, src := range c.Sources {
		if src.IsGit() {
			c.Repos = append(c.Repos, RepoConfig{
				ID: src.Name, Mode: ReadOnly, Remote: src.Remote, DefaultBranch: src.DefaultBranch,
				TokenEnv: src.TokenEnv, SyncInterval: src.SyncInterval,
			})
			continue
		}
		// non-git sources are remote-less mirror repos: gitx inits them empty
		// and the importer.Runner commits fetched snapshots. The importer, not
		// the gitx sync loop, drives updates — so no git SyncInterval.
		c.Repos = append(c.Repos, RepoConfig{
			ID: src.Name, Mode: ReadOnly, DefaultBranch: src.DefaultBranch, Mirror: true,
		})
	}
}

// forgeAPIBase derives the REST API base from a forge's web base URL.
func forgeAPIBase(kind, webBase string) string {
	base := strings.TrimSuffix(webBase, "/")
	switch kind {
	case forge.KindGitHub:
		if strings.HasSuffix(base, "github.com") {
			return "https://api.github.com"
		}
		return base + "/api/v3"
	case forge.KindGitLab:
		return base + "/api/v4"
	}
	return base
}

// TokenCreateLink is the "create a personal access token" deep link the login
// page offers, prefilled where the forge supports it.
func (c *Config) TokenCreateLink() string {
	f := c.Auth.Forge
	if f.TokenCreateURL != "" {
		return f.TokenCreateURL
	}
	scopes := strings.Join(c.ForgeScopes(), ",")
	base := strings.TrimSuffix(f.BaseURL, "/")
	switch f.Kind {
	case forge.KindGitLab:
		if base == "" {
			base = "https://gitlab.com"
		}
		return base + "/-/user_settings/personal_access_tokens?name=specquill&scopes=" + scopes
	case forge.KindGitHub:
		if base == "" {
			base = "https://github.com"
		}
		return base + "/settings/tokens/new?description=specquill&scopes=" + scopes
	}
	return ""
}

// ForgeScopes is the scope list the login page asks for (config override or
// the per-kind default that covers git push/pull plus the MR/PR API).
func (c *Config) ForgeScopes() []string {
	if len(c.Auth.Forge.Scopes) > 0 {
		return c.Auth.Forge.Scopes
	}
	switch c.Auth.Forge.Kind {
	case forge.KindGitLab:
		return []string{"api"}
	case forge.KindGitHub:
		return []string{"repo"}
	}
	return nil
}

// SourceHostAllowed reports whether an in-repo source remote may name this
// hostname. The allowlist is the deployment's own perimeter: the forge, every
// configured project remote's host, and auth.forge.allowed_source_hosts. The
// in-repo config is ordinary repo content — without this fence it could point
// a source at an attacker host (which would then be offered users' tokens) or
// at internal network services.
func (c *Config) SourceHostAllowed(host string) bool {
	host = strings.ToLower(host)
	if host == "" {
		return false
	}
	for _, h := range c.sourceHostAllowlist() {
		if host == h {
			return true
		}
	}
	return false
}

func (c *Config) sourceHostAllowlist() []string {
	var hosts []string
	add := func(h string) {
		if h = strings.ToLower(h); h != "" {
			hosts = append(hosts, h)
		}
	}
	if u, err := url.Parse(c.Auth.Forge.BaseURL); err == nil {
		add(u.Hostname())
	}
	if c.Auth.Forge.BaseURL == "" {
		switch c.Auth.Forge.Kind {
		case forge.KindGitLab:
			add("gitlab.com")
		case forge.KindGitHub:
			add("github.com")
		}
	}
	for _, p := range c.Projects {
		if u, err := url.Parse(p.Remote); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			add(u.Hostname())
		}
	}
	for _, h := range c.Auth.Forge.AllowedSourceHosts {
		add(h)
	}
	return hosts
}

// cleanContentRoot normalizes a project subfolder: slash-separated, no
// leading/trailing slashes, "" for the repo root. Traversal is rejected in
// validate().
func cleanContentRoot(root string) string {
	root = strings.Trim(strings.ReplaceAll(root, "\\", "/"), "/")
	if root == "." {
		return ""
	}
	return root
}

func (c *Config) validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data_dir is required")
	}
	if !c.Auth.Forge.Enabled() && !c.Auth.Local.Enabled {
		return fmt.Errorf("at least one auth method (forge or local) must be enabled")
	}
	if c.Auth.Forge.Enabled() {
		switch c.Auth.Forge.Kind {
		case forge.KindGitHub, forge.KindGitLab:
		default:
			return fmt.Errorf("auth.forge.kind must be github or gitlab (got %q)", c.Auth.Forge.Kind)
		}
		// forge-PAT deployments read reference sources from the in-repo
		// .specquill/config.yml `sources:` — a server-side catalog would need
		// env credentials the per-user model deliberately does not have
		if len(c.Sources) > 0 {
			return fmt.Errorf("auth.forge: sources are defined in-repo (.specquill/config.yml sources:) in forge-PAT mode — remove the top-level sources: block")
		}
	}
	switch c.Auth.DefaultRole {
	case "", "viewer", "editor", "maintainer", "admin", "none":
	default:
		return fmt.Errorf("auth.default_role must be viewer, editor, maintainer, admin or none (got %q)", c.Auth.DefaultRole)
	}
	if c.Git.CommitterName == "" || c.Git.CommitterEmail == "" {
		return fmt.Errorf("git.committer_name and git.committer_email are required")
	}
	if len(c.Projects) == 0 {
		return fmt.Errorf("at least one project must be configured (projects: or a legacy writable repos: entry)")
	}
	// projects and sources share the /api/repos/{x} namespace — ids must be
	// unique across both
	seen := map[string]bool{}
	for i, p := range c.Projects {
		if p.ID == "" || p.Remote == "" {
			return fmt.Errorf("project %d: id and remote are required", i)
		}
		if seen[p.ID] {
			return fmt.Errorf("duplicate project/source id %q", p.ID)
		}
		seen[p.ID] = true
		if strings.Contains(p.ContentRoot, "..") {
			return fmt.Errorf("project %s: content_root must not traverse (%q)", p.ID, p.ContentRoot)
		}
		switch p.Forge.Kind {
		case "", forge.KindGitHub, forge.KindGitLab:
		default:
			return fmt.Errorf("project %s: forge.kind must be github or gitlab (got %q)", p.ID, p.Forge.Kind)
		}
	}
	kinds := map[string]bool{"git": true, "url": true, "openapi": true, "confluence": true}
	for i, src := range c.Sources {
		if src.Name == "" {
			return fmt.Errorf("source %d: name is required", i)
		}
		if seen[src.Name] {
			return fmt.Errorf("duplicate project/source id %q", src.Name)
		}
		seen[src.Name] = true
		if src.Kind != "" && !kinds[src.Kind] {
			return fmt.Errorf("source %s: kind must be git, url, openapi or confluence", src.Name)
		}
		// a url source may list its pages in `urls:` instead of a single remote;
		// every other kind needs an endpoint/clone URL in `remote:`
		if src.Remote == "" && !(src.Kind == "url" && len(src.URLs) > 0) {
			return fmt.Errorf("source %s: remote is required", src.Name)
		}
		if src.Kind == "confluence" && src.Space == "" {
			return fmt.Errorf("source %s: confluence sources require a space", src.Name)
		}
	}
	if c.AI.Enabled && (c.AI.BaseURL == "" || c.AI.Model == "") {
		return fmt.Errorf("ai: base_url and model are required when enabled")
	}
	return nil
}

// looksLikePath reports whether a remote is a filesystem path rather than a URL.
func looksLikePath(remote string) bool {
	if remote == "" {
		return false
	}
	return remote[0] == '/' || remote[0] == '.' || remote[0] == '~'
}

func absAgainst(base, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	abs, err := filepath.Abs(filepath.Join(base, p))
	if err != nil {
		return p
	}
	return abs
}
