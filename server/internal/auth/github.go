package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"specquill/server/internal/config"
)

const ghOauthCookie = "specquill_oauth_gh"

// GitHub drives the OAuth2 authorization-code flow against github.com so
// users sign in with their GitHub account. GitHub OAuth apps are not OIDC
// (no discovery document, no id_token) — identity comes from the /user API
// after the code exchange.
type GitHub struct {
	cfg     config.GitHubAuthConfig
	secret  string
	baseURL string // the app's own base URL (redirect target)
	webBase string // https://github.com (overridable for tests / GHE)
	apiBase string // https://api.github.com
	client  *http.Client
}

func NewGitHub(cfg *config.Config) *GitHub {
	g := cfg.Auth.GitHub
	web := g.WebBase
	if web == "" {
		web = "https://github.com"
	}
	api := g.APIBase
	if api == "" {
		api = "https://api.github.com"
	}
	return &GitHub{
		cfg:     g,
		secret:  os.Getenv(g.ClientSecretEnv),
		baseURL: cfg.BaseURL,
		webBase: strings.TrimRight(web, "/"),
		apiBase: strings.TrimRight(api, "/"),
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// Begin redirects to GitHub's authorize page, stashing the state in a
// short-lived HttpOnly cookie.
func (g *GitHub) Begin(w http.ResponseWriter, r *http.Request, secure bool) {
	state := randB64(24)
	http.SetCookie(w, &http.Cookie{
		Name: ghOauthCookie, Value: state, Path: "/auth",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(10 * time.Minute),
	})
	q := url.Values{
		"client_id":    {g.cfg.ClientID},
		"redirect_uri": {g.baseURL + "/auth/github/callback"},
		"scope":        {"read:user user:email"},
		"state":        {state},
	}
	http.Redirect(w, r, g.webBase+"/login/oauth/authorize?"+q.Encode(), http.StatusFound)
}

// GitHubIdentity is the resolved GitHub user.
type GitHubIdentity struct {
	Subject string // stable: "github:<numeric id>"
	Login   string // the @handle — what allow/admin lists match on
	Name    string
	Email   string
}

// Finish validates the callback, exchanges the code, and resolves the user.
func (g *GitHub) Finish(w http.ResponseWriter, r *http.Request) (*GitHubIdentity, error) {
	cookie, err := r.Cookie(ghOauthCookie)
	if err != nil {
		return nil, fmt.Errorf("missing oauth state cookie")
	}
	http.SetCookie(w, &http.Cookie{Name: ghOauthCookie, Value: "", Path: "/auth", MaxAge: -1})
	if r.URL.Query().Get("state") != cookie.Value {
		return nil, fmt.Errorf("state mismatch")
	}

	// code → access token (GitHub returns JSON when asked)
	form := url.Values{
		"client_id":     {g.cfg.ClientID},
		"client_secret": {g.secret},
		"code":          {r.URL.Query().Get("code")},
		"redirect_uri":  {g.baseURL + "/auth/github/callback"},
	}
	req, _ := http.NewRequestWithContext(r.Context(), "POST", g.webBase+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := g.doJSON(req, &tok); err != nil {
		return nil, fmt.Errorf("code exchange: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("code exchange: %s", tok.Error)
	}

	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := g.api(r, tok.AccessToken, "/user", &user); err != nil {
		return nil, err
	}
	email := user.Email
	if email == "" {
		// profile email is often hidden — the user:email scope exposes the list
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := g.api(r, tok.AccessToken, "/user/emails", &emails); err != nil {
			return nil, err
		}
		for _, e := range emails {
			if e.Primary && e.Verified {
				email = e.Email
				break
			}
		}
		if email == "" && len(emails) > 0 {
			email = emails[0].Email
		}
	}
	if email == "" {
		return nil, fmt.Errorf("github account exposes no email; one is required for git authorship")
	}
	name := user.Name
	if name == "" {
		name = user.Login
	}
	return &GitHubIdentity{
		Subject: fmt.Sprintf("github:%d", user.ID),
		Login:   user.Login,
		Name:    name,
		Email:   email,
	}, nil
}

// Allowed reports whether the login passes the configured allowlist
// (an empty list admits any GitHub account — only sane behind a VPN).
func (g *GitHub) Allowed(login string) bool {
	if len(g.cfg.AllowedUsers) == 0 {
		return true
	}
	for _, u := range g.cfg.AllowedUsers {
		if strings.EqualFold(u, login) {
			return true
		}
	}
	return false
}

func (g *GitHub) api(r *http.Request, token, path string, out any) error {
	req, _ := http.NewRequestWithContext(r.Context(), "GET", g.apiBase+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if err := g.doJSON(req, out); err != nil {
		return fmt.Errorf("github %s: %w", path, err)
	}
	return nil
}

func (g *GitHub) doJSON(req *http.Request, out any) error {
	res, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %.200s", res.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}
