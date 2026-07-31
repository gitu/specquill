package forge

// Dynamic-project support (REQ-025): repository resolution, search and
// manifest reads against a forge HOST (not a preconfigured project), always
// with the requesting user's own token — the corpus is exactly what that
// token can reach.

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RepoInfo describes one forge repository as dynamic projects need it.
type RepoInfo struct {
	// ID is the forge's STABLE numeric repository id, stringified — the
	// identity anchor (REQ-025.4): spellings and renames converge on it.
	ID            string `json:"id"`
	Path          string `json:"path"` // canonical owner/repo (github) / full path (gitlab)
	Remote        string `json:"remote"`
	DefaultBranch string `json:"defaultBranch"`
	WebURL        string `json:"webUrl"`
	// Role is the token owner's permission mapped onto the authz ladder
	// (viewer/editor/maintainer/admin/none). Empty in search results — the
	// cheap listing does not probe permissions per repo.
	Role string `json:"role,omitempty"`
}

// HostAPIBase derives the REST API base from a forge kind and web base URL —
// the host-level twin of the per-project derivation in config.
func HostAPIBase(kind, webBase string) string {
	base := strings.TrimSuffix(webBase, "/")
	switch kind {
	case KindGitHub:
		if base == "" {
			return "https://api.github.com"
		}
		return base + "/api/v3"
	case KindGitLab:
		if base == "" {
			return "https://gitlab.com/api/v4"
		}
		return base + "/api/v4"
	}
	return base
}

// NewHost builds a client bound to a forge host for repository resolution
// and search. Unlike New it carries no project — paths travel per call.
func NewHost(kind, webBase, token string) *Client {
	return &Client{
		kind: kind, apiBase: HostAPIBase(kind, webBase), token: token,
		hc: &http.Client{Timeout: 15 * time.Second},
	}
}

// ResolveRepo resolves an owner/repo (GitHub) or full path (GitLab) through
// the forge API: stable id, canonical path (renames converge here), clone
// URL, default branch and the token owner's permission. A StatusError with
// 404/403 means the token cannot see the repository.
func (c *Client) ResolveRepo(ctx context.Context, path string) (*RepoInfo, error) {
	if c.kind == KindGitHub {
		var gh struct {
			ID            int64  `json:"id"`
			FullName      string `json:"full_name"`
			CloneURL      string `json:"clone_url"`
			DefaultBranch string `json:"default_branch"`
			HTMLURL       string `json:"html_url"`
			Permissions   struct {
				Admin    bool `json:"admin"`
				Maintain bool `json:"maintain"`
				Push     bool `json:"push"`
				Pull     bool `json:"pull"`
			} `json:"permissions"`
		}
		if err := c.getJSON(ctx, c.apiBase+"/repos/"+path, &gh); err != nil {
			return nil, err
		}
		role := "none"
		switch {
		case gh.Permissions.Admin:
			role = "admin"
		case gh.Permissions.Maintain:
			role = "maintainer"
		case gh.Permissions.Push:
			role = "editor"
		case gh.Permissions.Pull:
			role = "viewer"
		}
		return &RepoInfo{
			ID: strconv.FormatInt(gh.ID, 10), Path: gh.FullName, Remote: gh.CloneURL,
			DefaultBranch: gh.DefaultBranch, WebURL: gh.HTMLURL, Role: role,
		}, nil
	}
	var gl struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		HTTPURL           string `json:"http_url_to_repo"`
		DefaultBranch     string `json:"default_branch"`
		WebURL            string `json:"web_url"`
		Permissions       struct {
			ProjectAccess *struct {
				AccessLevel int `json:"access_level"`
			} `json:"project_access"`
			GroupAccess *struct {
				AccessLevel int `json:"access_level"`
			} `json:"group_access"`
		} `json:"permissions"`
	}
	if err := c.getJSON(ctx, c.apiBase+"/projects/"+url.PathEscape(path), &gl); err != nil {
		return nil, err
	}
	level := 0
	if a := gl.Permissions.ProjectAccess; a != nil && a.AccessLevel > level {
		level = a.AccessLevel
	}
	if a := gl.Permissions.GroupAccess; a != nil && a.AccessLevel > level {
		level = a.AccessLevel
	}
	role := "none"
	switch {
	case level >= 50:
		role = "admin"
	case level >= 40:
		role = "maintainer"
	case level >= 30:
		role = "editor"
	case level >= 10:
		role = "viewer"
	}
	return &RepoInfo{
		ID: strconv.FormatInt(gl.ID, 10), Path: gl.PathWithNamespace, Remote: gl.HTTPURL,
		DefaultBranch: gl.DefaultBranch, WebURL: gl.WebURL, Role: role,
	}, nil
}

// SearchRepos lists repositories the token can reach, filtered by query.
// Bounded to one page — a picker, not an exporter.
func (c *Client) SearchRepos(ctx context.Context, query string) ([]RepoInfo, error) {
	if c.kind == KindGitHub {
		// /user/repos (not /search) so the corpus is exactly the token's
		// affiliation — private repos included, nothing beyond it
		var repos []struct {
			ID            int64  `json:"id"`
			FullName      string `json:"full_name"`
			CloneURL      string `json:"clone_url"`
			DefaultBranch string `json:"default_branch"`
			HTMLURL       string `json:"html_url"`
		}
		if err := c.getJSON(ctx, c.apiBase+"/user/repos?per_page=100&sort=pushed", &repos); err != nil {
			return nil, err
		}
		out := []RepoInfo{}
		q := strings.ToLower(strings.TrimSpace(query))
		for _, r := range repos {
			if q != "" && !strings.Contains(strings.ToLower(r.FullName), q) {
				continue
			}
			out = append(out, RepoInfo{
				ID: strconv.FormatInt(r.ID, 10), Path: r.FullName, Remote: r.CloneURL,
				DefaultBranch: r.DefaultBranch, WebURL: r.HTMLURL,
			})
		}
		return out, nil
	}
	var repos []struct {
		ID                int64  `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		HTTPURL           string `json:"http_url_to_repo"`
		DefaultBranch     string `json:"default_branch"`
		WebURL            string `json:"web_url"`
	}
	u := c.apiBase + "/projects?membership=true&order_by=last_activity_at&per_page=100"
	if q := strings.TrimSpace(query); q != "" {
		u += "&search=" + url.QueryEscape(q)
	}
	if err := c.getJSON(ctx, u, &repos); err != nil {
		return nil, err
	}
	out := []RepoInfo{}
	for _, r := range repos {
		out = append(out, RepoInfo{
			ID: strconv.FormatInt(r.ID, 10), Path: r.PathWithNamespace, Remote: r.HTTPURL,
			DefaultBranch: r.DefaultBranch, WebURL: r.WebURL,
		})
	}
	return out, nil
}

// RepoFile fetches one file from a repository at ref through the forge API —
// how the workspace manifest is read WITHOUT materializing a clone
// (REQ-025.1: the manifest decides openability before any disk is spent).
// A StatusError with 404 means the file (or the repo) is not there.
func (c *Client) RepoFile(ctx context.Context, path, ref, file string) (string, error) {
	if c.kind == KindGitHub {
		var gh struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}
		u := fmt.Sprintf("%s/repos/%s/contents/%s?ref=%s", c.apiBase, path, file, url.QueryEscape(ref))
		if err := c.getJSON(ctx, u, &gh); err != nil {
			return "", err
		}
		if gh.Encoding != "base64" {
			return "", fmt.Errorf("unexpected content encoding %q", gh.Encoding)
		}
		raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(gh.Content, "\n", ""))
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	u := fmt.Sprintf("%s/projects/%s/repository/files/%s/raw?ref=%s",
		c.apiBase, url.PathEscape(path), url.PathEscape(file), url.QueryEscape(ref))
	return c.getText(ctx, u)
}

func (c *Client) getText(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	c.authorize(req)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode/100 != 2 {
		return "", statusError(resp, string(body))
	}
	return string(body), nil
}
