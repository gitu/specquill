// Package config loads and validates the specquill server configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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
}

// SourceConfig is a stage-1 catalog entry: a named external source that
// projects may reference (docs/multi-tenancy.md + the projects plan).
// Sources are read-only downstream, always. Credentials come from the
// environment via token_env — never from the DB or in-repo config.
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
}

// IsProtected reports whether direct writes/commits to branch are forbidden
// (such branches only move via PR merges).
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

type OIDCConfig struct {
	Enabled         bool     `yaml:"enabled"`
	Issuer          string   `yaml:"issuer"`
	ClientID        string   `yaml:"client_id"`
	ClientSecretEnv string   `yaml:"client_secret_env"`
	Scopes          []string `yaml:"scopes"`
}

type LocalAuthConfig struct {
	Enabled bool `yaml:"enabled"`
}

// GitHubAuthConfig signs users in with their GitHub account (OAuth app flow —
// GitHub is not an OIDC issuer for user login). allowed_users gates who may
// log in at all; an empty list admits any GitHub account.
type GitHubAuthConfig struct {
	Enabled         bool     `yaml:"enabled"`
	ClientID        string   `yaml:"client_id"`
	ClientSecretEnv string   `yaml:"client_secret_env"`
	AllowedUsers    []string `yaml:"allowed_users"` // GitHub logins admitted (empty = everyone)
	WebBase         string   `yaml:"web_base"`      // override for GHE/tests (default https://github.com)
	APIBase         string   `yaml:"api_base"`      // override for GHE/tests (default https://api.github.com)
}

// DevUser auto-authenticates every request as this identity — honored only
// when the server runs with the -dev flag.
type DevUser struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

type AuthConfig struct {
	OIDC   OIDCConfig       `yaml:"oidc"`
	GitHub GitHubAuthConfig `yaml:"github"`
	Local  LocalAuthConfig  `yaml:"local"`
	// AdminEmails bootstrap tenant administration: users whose email matches
	// (case-insensitive, any provider) get the admin role in the default
	// tenant on login. Without it a fresh deployment has members only and
	// the management API is unreachable.
	AdminEmails []string `yaml:"admin_emails"`
	DevUser     *DevUser `yaml:"dev_user"`
}

type SessionConfig struct {
	TTL          time.Duration `yaml:"ttl"`
	CookieSecure bool          `yaml:"cookie_secure"`
}

// GitHubWebhookConfig accepts push webhooks from GitHub repositories at
// POST /hooks/github: pushes to a registered repo's remote trigger an
// immediate fetch (+ fast-forward of the default branch) instead of waiting
// for the next sync interval. The HMAC secret is the only authentication.
type GitHubWebhookConfig struct {
	Enabled   bool   `yaml:"enabled"`
	SecretEnv string `yaml:"secret_env"` // env var holding the webhook HMAC secret
}

type WebhooksConfig struct {
	GitHub GitHubWebhookConfig `yaml:"github"`
}

// GitHubAppConfig turns on GitHub-App tenant management
// (docs/multi-tenancy.md): each installation becomes a tenant, installation
// tokens authenticate git, repo permissions map to roles, and the
// installation webhooks keep it all in sync. Enabled when app_id is set.
type GitHubAppConfig struct {
	AppID            int64  `yaml:"app_id"`
	PrivateKeyEnv    string `yaml:"private_key_env"`  // PEM in an env var…
	PrivateKeyPath   string `yaml:"private_key_path"` // …or a mounted file
	WebhookSecretEnv string `yaml:"webhook_secret_env"`
	APIBase          string `yaml:"api_base"` // override for tests / GHE (default https://api.github.com)
}

func (g GitHubAppConfig) Enabled() bool { return g.AppID != 0 }

// DatabaseConfig locates the Postgres store (users, sessions, PR review
// state, collab logs). Production configs must use url_env so the DSN —
// which carries credentials — never lives in a file.
type DatabaseConfig struct {
	URL    string `yaml:"url"`     // local dev only (compose postgres, no secrets)
	URLEnv string `yaml:"url_env"` // env var holding the DSN (e.g. a Neon URL)
}

// DSN resolves the connection string; the env var wins when set.
func (d DatabaseConfig) DSN() (string, error) {
	if d.URLEnv != "" {
		if v := os.Getenv(d.URLEnv); v != "" {
			return v, nil
		}
		if d.URL == "" {
			return "", fmt.Errorf("database.url_env: %s is not set", d.URLEnv)
		}
	}
	if d.URL != "" {
		return d.URL, nil
	}
	return "", fmt.Errorf("database.url or database.url_env is required")
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
	// Grants: source names granted to the default tenant (stage 2).
	// Omitted/empty = all sources granted (self-host convenience).
	Grants  []string      `yaml:"grants"`
	Repos   []RepoConfig  `yaml:"repos"` // legacy shape — normalized into projects/sources
	Git     GitConfig     `yaml:"git"`
	Auth      AuthConfig      `yaml:"auth"`
	Session   SessionConfig   `yaml:"session"`
	Webhooks  WebhooksConfig  `yaml:"webhooks"`
	GitHubApp GitHubAppConfig `yaml:"github_app"`
	AI        AIConfig        `yaml:"ai"`
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
		c.Repos = append(c.Repos, RepoConfig{
			ID: p.ID, Mode: Writable, Remote: p.Remote, DefaultBranch: p.DefaultBranch,
			TokenEnv: p.TokenEnv, SyncInterval: p.SyncInterval,
			ProtectedBranches: p.ProtectedBranches, ContentRoot: p.ContentRoot,
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
	if !c.Auth.OIDC.Enabled && !c.Auth.GitHub.Enabled && !c.Auth.Local.Enabled {
		return fmt.Errorf("at least one auth method (oidc, github or local) must be enabled")
	}
	if c.Database.URL == "" && c.Database.URLEnv == "" {
		return fmt.Errorf("database.url or database.url_env is required (Postgres DSN)")
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
	for _, g := range c.Grants {
		found := false
		for _, src := range c.Sources {
			if src.Name == g {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("grants: unknown source %q", g)
		}
	}
	if c.Auth.OIDC.Enabled {
		o := c.Auth.OIDC
		if o.Issuer == "" || o.ClientID == "" {
			return fmt.Errorf("auth.oidc: issuer and client_id are required when enabled")
		}
	}
	if c.Auth.GitHub.Enabled {
		g := c.Auth.GitHub
		if g.ClientID == "" || g.ClientSecretEnv == "" {
			return fmt.Errorf("auth.github: client_id and client_secret_env are required when enabled")
		}
		if c.BaseURL == "" {
			return fmt.Errorf("auth.github: base_url is required (OAuth callback URL)")
		}
	}
	if c.Webhooks.GitHub.Enabled && c.Webhooks.GitHub.SecretEnv == "" {
		return fmt.Errorf("webhooks.github: secret_env is required when enabled")
	}
	if c.GitHubApp.Enabled() {
		if c.GitHubApp.PrivateKeyEnv == "" && c.GitHubApp.PrivateKeyPath == "" {
			return fmt.Errorf("github_app: private_key_env or private_key_path is required")
		}
		if c.GitHubApp.WebhookSecretEnv == "" {
			return fmt.Errorf("github_app: webhook_secret_env is required")
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
