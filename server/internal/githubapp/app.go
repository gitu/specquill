// Package githubapp speaks the GitHub App server-to-server API: short-lived
// RS256 app JWTs mint per-installation access tokens (cached until shortly
// before expiry), which authenticate git operations, installation repo
// listings and permission lookups. One App instance serves every tenant;
// tokens never cross installations.
package githubapp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"specquill/server/internal/config"
)

type App struct {
	appID   int64
	key     *rsa.PrivateKey
	apiBase string
	client  *http.Client

	mu     sync.Mutex
	tokens map[int64]instToken  // per-installation, cached
	repoIn map[string]repoInst  // repo full name → covering installation
}

type instToken struct {
	token   string
	expires time.Time
}

type repoInst struct {
	id      int64 // 0 = not covered by any installation
	expires time.Time
}

func New(cfg config.GitHubAppConfig) (*App, error) {
	pemBytes := []byte(os.Getenv(cfg.PrivateKeyEnv))
	if len(pemBytes) == 0 && cfg.PrivateKeyPath != "" {
		b, err := os.ReadFile(cfg.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("github_app: read private key: %w", err)
		}
		pemBytes = b
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("github_app: private key is not PEM")
	}
	var key *rsa.PrivateKey
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		key = k
	} else if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("github_app: private key is not RSA")
		}
		key = rk
	} else {
		return nil, fmt.Errorf("github_app: cannot parse private key")
	}
	base := cfg.APIBase
	if base == "" {
		base = "https://api.github.com"
	}
	return &App{
		appID:   cfg.AppID,
		key:     key,
		apiBase: strings.TrimRight(base, "/"),
		client:  &http.Client{Timeout: 15 * time.Second},
		tokens:  map[int64]instToken{},
		repoIn:  map[string]repoInst{},
	}, nil
}

// appJWT builds the 10-minute RS256 JWT GitHub requires for app-level calls.
func (a *App) appJWT() (string, error) {
	b64 := func(v any) string {
		j, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(j)
	}
	now := time.Now()
	signing := b64(map[string]string{"alg": "RS256", "typ": "JWT"}) + "." +
		b64(map[string]int64{
			"iat": now.Add(-60 * time.Second).Unix(), // clock-skew grace
			"exp": now.Add(9 * time.Minute).Unix(),
			"iss": a.appID,
		})
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// InstallationToken returns a valid access token for the installation,
// minting a fresh one when the cached token is within 5 minutes of expiry.
func (a *App) InstallationToken(installationID int64) (string, error) {
	a.mu.Lock()
	if t, ok := a.tokens[installationID]; ok && time.Until(t.expires) > 5*time.Minute {
		a.mu.Unlock()
		return t.token, nil
	}
	a.mu.Unlock()

	jwt, err := a.appJWT()
	if err != nil {
		return "", err
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := a.call("POST", fmt.Sprintf("/app/installations/%d/access_tokens", installationID), jwt, &out); err != nil {
		return "", fmt.Errorf("installation token: %w", err)
	}
	a.mu.Lock()
	a.tokens[installationID] = instToken{token: out.Token, expires: out.ExpiresAt}
	a.mu.Unlock()
	return out.Token, nil
}

// Repo is an installation repository (a tenant's candidate).
type Repo struct {
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	Description   string `json:"description"`
}

// Repos lists every repository the installation grants access to.
func (a *App) Repos(installationID int64) ([]Repo, error) {
	tok, err := a.InstallationToken(installationID)
	if err != nil {
		return nil, err
	}
	var all []Repo
	for page := 1; ; page++ {
		var out struct {
			TotalCount   int    `json:"total_count"`
			Repositories []Repo `json:"repositories"`
		}
		if err := a.call("GET", fmt.Sprintf("/installation/repositories?per_page=100&page=%d", page), tok, &out); err != nil {
			return nil, fmt.Errorf("installation repos: %w", err)
		}
		all = append(all, out.Repositories...)
		if len(all) >= out.TotalCount || len(out.Repositories) == 0 {
			return all, nil
		}
	}
}

// Permission resolves a user's effective permission on a repository:
// "admin", "write", "read" or "none".
func (a *App) Permission(installationID int64, fullName, login string) (string, error) {
	tok, err := a.InstallationToken(installationID)
	if err != nil {
		return "", err
	}
	var out struct {
		Permission string `json:"permission"`
	}
	err = a.call("GET", "/repos/"+fullName+"/collaborators/"+login+"/permission", tok, &out)
	if err != nil {
		// 404 = not a collaborator at all
		if strings.Contains(err.Error(), "status 404") {
			return "none", nil
		}
		return "", fmt.Errorf("permission %s on %s: %w", login, fullName, err)
	}
	if out.Permission == "" {
		return "none", nil
	}
	return out.Permission, nil
}

// RepoInstallation resolves which installation of this app covers a
// repository ("owner/name") — how config-tenant repos ride installation
// tokens instead of a PAT. Hits are cached for an hour, misses (app not
// installed on the repo) for 5 minutes; a miss returns ErrNotInstalled so
// callers can fall back to token_env.
func (a *App) RepoInstallation(fullName string) (int64, error) {
	key := strings.ToLower(fullName)
	a.mu.Lock()
	if e, ok := a.repoIn[key]; ok && time.Now().Before(e.expires) {
		a.mu.Unlock()
		if e.id == 0 {
			return 0, ErrNotInstalled
		}
		return e.id, nil
	}
	a.mu.Unlock()

	jwt, err := a.appJWT()
	if err != nil {
		return 0, err
	}
	var out struct {
		ID int64 `json:"id"`
	}
	err = a.call("GET", "/repos/"+fullName+"/installation", jwt, &out)
	if err != nil {
		if strings.Contains(err.Error(), "status 404") {
			a.mu.Lock()
			a.repoIn[key] = repoInst{id: 0, expires: time.Now().Add(5 * time.Minute)}
			a.mu.Unlock()
			return 0, ErrNotInstalled
		}
		return 0, fmt.Errorf("repo installation %s: %w", fullName, err)
	}
	a.mu.Lock()
	a.repoIn[key] = repoInst{id: out.ID, expires: time.Now().Add(time.Hour)}
	a.mu.Unlock()
	return out.ID, nil
}

// ErrNotInstalled: the app has no installation covering the repository.
var ErrNotInstalled = fmt.Errorf("github app: not installed on this repository")

// RepoFromRemote extracts "owner/name" from a github.com remote URL —
// https or ssh form. Non-GitHub remotes return ok=false (they keep using
// token_env credentials).
func RepoFromRemote(remote string) (string, bool) {
	var path string
	switch {
	case strings.HasPrefix(remote, "https://github.com/"):
		path = strings.TrimPrefix(remote, "https://github.com/")
	case strings.HasPrefix(remote, "git@github.com:"):
		path = strings.TrimPrefix(remote, "git@github.com:")
	case strings.HasPrefix(remote, "ssh://git@github.com/"):
		path = strings.TrimPrefix(remote, "ssh://git@github.com/")
	default:
		return "", false
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	if parts := strings.Split(path, "/"); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		return parts[0] + "/" + parts[1], true
	}
	return "", false
}

func (a *App) call(method, path, bearer string, out any) error {
	req, err := http.NewRequest(method, a.apiBase+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("status %d: %.200s", res.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}
