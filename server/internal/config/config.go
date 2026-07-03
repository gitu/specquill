// Package config loads and validates the reqbase server configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type RepoMode string

const (
	Writable RepoMode = "writable"
	ReadOnly RepoMode = "readonly"
)

type RepoConfig struct {
	ID                string        `yaml:"id"`
	Mode              RepoMode      `yaml:"mode"`
	Remote            string        `yaml:"remote"`
	DefaultBranch     string        `yaml:"default_branch"`
	TokenEnv          string        `yaml:"token_env"`
	SyncInterval      time.Duration `yaml:"sync_interval"`
	ProtectedBranches []string      `yaml:"protected_branches"` // default: [default_branch]
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

// DevUser auto-authenticates every request as this identity — honored only
// when the server runs with the -dev flag.
type DevUser struct {
	Name  string `yaml:"name"`
	Email string `yaml:"email"`
}

type AuthConfig struct {
	OIDC    OIDCConfig      `yaml:"oidc"`
	Local   LocalAuthConfig `yaml:"local"`
	DevUser *DevUser        `yaml:"dev_user"`
}

type SessionConfig struct {
	TTL          time.Duration `yaml:"ttl"`
	CookieSecure bool          `yaml:"cookie_secure"`
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
}

type Config struct {
	Listen  string        `yaml:"listen"`
	DataDir string        `yaml:"data_dir"`
	BaseURL string        `yaml:"base_url"`
	Repos   []RepoConfig  `yaml:"repos"`
	Git     GitConfig     `yaml:"git"`
	Auth    AuthConfig    `yaml:"auth"`
	Session SessionConfig `yaml:"session"`
	AI      AIConfig      `yaml:"ai"`
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
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// resolve relative paths against the config file's directory
	base := filepath.Dir(path)
	cfg.DataDir = absAgainst(base, cfg.DataDir)
	for i := range cfg.Repos {
		r := &cfg.Repos[i]
		if looksLikePath(r.Remote) {
			r.Remote = absAgainst(base, r.Remote)
		}
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data_dir is required")
	}
	if len(c.Repos) == 0 {
		return fmt.Errorf("at least one repo must be configured")
	}
	if !c.Auth.OIDC.Enabled && !c.Auth.Local.Enabled {
		return fmt.Errorf("at least one auth method (oidc or local) must be enabled")
	}
	if c.Git.CommitterName == "" || c.Git.CommitterEmail == "" {
		return fmt.Errorf("git.committer_name and git.committer_email are required")
	}
	writable := 0
	seen := map[string]bool{}
	for i := range c.Repos {
		r := &c.Repos[i]
		if r.ID == "" || r.Remote == "" {
			return fmt.Errorf("repo %d: id and remote are required", i)
		}
		if seen[r.ID] {
			return fmt.Errorf("duplicate repo id %q", r.ID)
		}
		seen[r.ID] = true
		switch r.Mode {
		case Writable:
			writable++
		case ReadOnly:
		default:
			return fmt.Errorf("repo %s: mode must be writable or readonly", r.ID)
		}
		if r.DefaultBranch == "" {
			r.DefaultBranch = "main"
		}
		if r.Mode == ReadOnly && r.SyncInterval == 0 {
			r.SyncInterval = 5 * time.Minute
		}
		if r.Mode == Writable && r.SyncInterval == 0 {
			r.SyncInterval = 2 * time.Minute
		}
		if len(r.ProtectedBranches) == 0 {
			r.ProtectedBranches = []string{r.DefaultBranch}
		}
	}
	if writable != 1 {
		return fmt.Errorf("exactly one writable repo is required (got %d)", writable)
	}
	if c.Auth.OIDC.Enabled {
		o := c.Auth.OIDC
		if o.Issuer == "" || o.ClientID == "" {
			return fmt.Errorf("auth.oidc: issuer and client_id are required when enabled")
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
