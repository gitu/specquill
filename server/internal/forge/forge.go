// Package forge reads merge-request review threads from a git host so they
// can be shown next to the branch they are about.
//
// SpecQuill has no in-app review flow — reviewed merges are delegated to the
// forge (see repo-product/docs/specs/specs/merging.md). This package closes the
// resulting gap: an author editing on `ws/<user>` can see the comments their
// reviewer left on the corresponding merge request without leaving the tool.
//
// Deliberately READ-ONLY and opt-in. It is not a return of the GitHub
// integration that was removed: no login, no app install, no webhooks, no
// bearing on authorization — just an authenticated GET against a host the
// deployment already pushes to.
package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Kinds of forge this package can read.
const (
	KindGitHub = "github"
	KindGitLab = "gitlab"
)

// Config is the per-project opt-in. An empty Kind disables the feature.
type Config struct {
	Kind    string `yaml:"kind"`     // github | gitlab
	BaseURL string `yaml:"base_url"` // API base for self-hosted; derived from the remote otherwise
	// Project overrides the "owner/repo" (GitHub) or full path (GitLab)
	// otherwise derived from the git remote — needed when the remote does not
	// name it plainly, e.g. an ssh host alias like `git@gh:acme/specs`.
	Project  string `yaml:"project"`
	TokenEnv string `yaml:"token_env"` // env var holding the API token; defaults to the repo's
}

func (c Config) Enabled() bool { return c.Kind == KindGitHub || c.Kind == KindGitLab }

// Comment is one review note. Path/Line are set for comments anchored to a
// diff line; general discussion leaves them zero.
type Comment struct {
	Author    string `json:"author"`
	Body      string `json:"body"`
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line,omitempty"`
	CreatedAt string `json:"createdAt"`
	URL       string `json:"url,omitempty"`
}

// Request is the open merge request for a branch, with its comments.
type Request struct {
	Number   int       `json:"number"`
	Title    string    `json:"title"`
	State    string    `json:"state"`
	Author   string    `json:"author"`
	URL      string    `json:"url"`
	Comments []Comment `json:"comments"`
}

type Client struct {
	kind    string
	apiBase string
	project string // "owner/repo" (GitHub) or the full path (GitLab, may nest)
	token   string
	hc      *http.Client
}

// New builds a client for a project's remote. Returns nil (no error) when the
// config is disabled, so callers can treat "not configured" as "no panel".
func New(cfg Config, remote, token string) (*Client, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	host, path, err := parseRemote(remote)
	if cfg.Project != "" {
		path = strings.Trim(cfg.Project, "/")
		if err != nil { // an explicit project makes the remote's shape irrelevant
			host, err = "", nil
		}
	}
	if err != nil {
		return nil, err
	}
	if cfg.Kind == KindGitHub && strings.Count(path, "/") != 1 {
		return nil, fmt.Errorf("github remote must be owner/repo, got %q", path)
	}
	base := strings.TrimSuffix(cfg.BaseURL, "/")
	if base == "" && host == "" {
		return nil, fmt.Errorf("forge.base_url is required when the remote is not a forge URL")
	}
	if base == "" {
		switch cfg.Kind {
		case KindGitHub:
			// github.com uses a dedicated API host; GHE serves it under /api/v3
			if host == "github.com" {
				base = "https://api.github.com"
			} else {
				base = "https://" + host + "/api/v3"
			}
		case KindGitLab:
			base = "https://" + host + "/api/v4"
		}
	}
	return &Client{
		kind: cfg.Kind, apiBase: base, project: path, token: token,
		hc: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// parseRemote splits a git remote into host and project path, accepting
// https://, ssh:// and scp-like `git@host:owner/repo` forms. Local paths have
// no host and are rejected — there is no forge behind them.
func parseRemote(remote string) (host, path string, err error) {
	r := strings.TrimSpace(remote)
	r = strings.TrimSuffix(strings.TrimSuffix(r, "/"), ".git")
	switch {
	case strings.HasPrefix(r, "https://"), strings.HasPrefix(r, "http://"), strings.HasPrefix(r, "ssh://"):
		rest := r[strings.Index(r, "://")+3:]
		if at := strings.LastIndex(rest, "@"); at >= 0 { // ssh://git@host/owner/repo
			rest = rest[at+1:]
		}
		h, p, ok := strings.Cut(rest, "/")
		if !ok || h == "" || p == "" {
			return "", "", fmt.Errorf("remote %q has no project path", remote)
		}
		return stripPort(h), strings.Trim(p, "/"), nil
	case strings.Contains(r, "@") && strings.Contains(r, ":"): // git@host:owner/repo
		rest := r[strings.Index(r, "@")+1:]
		h, p, ok := strings.Cut(rest, ":")
		if !ok || h == "" || p == "" {
			return "", "", fmt.Errorf("remote %q has no project path", remote)
		}
		return stripPort(h), strings.Trim(p, "/"), nil
	default:
		return "", "", fmt.Errorf("remote %q is not a forge URL", remote)
	}
}

func stripPort(h string) string {
	if i := strings.LastIndex(h, ":"); i > 0 {
		return h[:i]
	}
	return h
}

// OpenRequest returns the open merge request whose source is branch, with its
// comments — or nil when the branch has none.
func (c *Client) OpenRequest(ctx context.Context, branch string) (*Request, error) {
	if c == nil {
		return nil, nil
	}
	if c.kind == KindGitHub {
		return c.githubRequest(ctx, branch)
	}
	return c.gitlabRequest(ctx, branch)
}

func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		// GitLab PATs go in PRIVATE-TOKEN; GitHub takes a bearer token
		if c.kind == KindGitLab {
			req.Header.Set("PRIVATE-TOKEN", c.token)
		} else {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
	}
	if c.kind == KindGitHub {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		// the URL is safe to echo (no token in it); the body may carry the
		// forge's own explanation, which is the useful part
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(firstLine(string(body))))
	}
	return json.Unmarshal(body, out)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// ---------------------------------------------------------------- github

func (c *Client) githubRequest(ctx context.Context, branch string) (*Request, error) {
	owner, _, _ := strings.Cut(c.project, "/")
	var prs []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	q := fmt.Sprintf("%s/repos/%s/pulls?state=open&head=%s",
		c.apiBase, c.project, url.QueryEscape(owner+":"+branch))
	if err := c.getJSON(ctx, q, &prs); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	pr := prs[0]
	out := &Request{
		Number: pr.Number, Title: pr.Title, State: pr.State,
		Author: pr.User.Login, URL: pr.HTMLURL, Comments: []Comment{},
	}

	// review comments carry a file + line; issue comments are the general thread
	var review []struct {
		Body      string `json:"body"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		CreatedAt string `json:"created_at"`
		HTMLURL   string `json:"html_url"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("%s/repos/%s/pulls/%d/comments", c.apiBase, c.project, pr.Number), &review); err != nil {
		return nil, err
	}
	for _, r := range review {
		out.Comments = append(out.Comments, Comment{
			Author: r.User.Login, Body: r.Body, Path: r.Path, Line: r.Line,
			CreatedAt: r.CreatedAt, URL: r.HTMLURL,
		})
	}
	var issue []struct {
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		HTMLURL   string `json:"html_url"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("%s/repos/%s/issues/%d/comments", c.apiBase, c.project, pr.Number), &issue); err != nil {
		return nil, err
	}
	for _, r := range issue {
		out.Comments = append(out.Comments, Comment{
			Author: r.User.Login, Body: r.Body, CreatedAt: r.CreatedAt, URL: r.HTMLURL,
		})
	}
	return out, nil
}

// ---------------------------------------------------------------- gitlab

func (c *Client) gitlabRequest(ctx context.Context, branch string) (*Request, error) {
	id := url.PathEscape(c.project) // group/subgroup/repo → URL-encoded id
	var mrs []struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		State  string `json:"state"`
		WebURL string `json:"web_url"`
		Author struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	q := fmt.Sprintf("%s/projects/%s/merge_requests?state=opened&source_branch=%s",
		c.apiBase, id, url.QueryEscape(branch))
	if err := c.getJSON(ctx, q, &mrs); err != nil {
		return nil, err
	}
	if len(mrs) == 0 {
		return nil, nil
	}
	mr := mrs[0]
	out := &Request{
		Number: mr.IID, Title: mr.Title, State: mr.State,
		Author: mr.Author.Username, URL: mr.WebURL, Comments: []Comment{},
	}

	var notes []struct {
		Body      string `json:"body"`
		System    bool   `json:"system"`
		CreatedAt string `json:"created_at"`
		Author    struct {
			Username string `json:"username"`
		} `json:"author"`
		Position *struct {
			NewPath string `json:"new_path"`
			NewLine int    `json:"new_line"`
		} `json:"position"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("%s/projects/%s/merge_requests/%d/notes?sort=asc", c.apiBase, id, mr.IID), &notes); err != nil {
		return nil, err
	}
	for _, n := range notes {
		if n.System {
			continue // "assigned to", "changed title" … not review feedback
		}
		cm := Comment{Author: n.Author.Username, Body: n.Body, CreatedAt: n.CreatedAt, URL: mr.WebURL}
		if n.Position != nil {
			cm.Path, cm.Line = n.Position.NewPath, n.Position.NewLine
		}
		out.Comments = append(out.Comments, cm)
	}
	return out, nil
}
