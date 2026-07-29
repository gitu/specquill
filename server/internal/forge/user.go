package forge

// Forge-PAT support: identity, project role and merge-request creation with a
// user-supplied personal access token. This is the write-capable counterpart
// to forge.go's read-only review threads — used by PAT login (identity+role)
// and the "propose changes" flow (push + open MR/PR).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// User is the identity behind a personal access token, as reported by the
// forge's /user endpoint.
type User struct {
	// Subject is the forge's stable numeric user id, stringified — the
	// (provider, subject) key for the users table.
	Subject string
	Login   string
	Name    string
	Email   string
	// Scopes lists the token's scopes when the forge reports them (GitHub
	// classic PATs via the X-OAuth-Scopes header); empty otherwise.
	Scopes []string
}

// CurrentUser verifies the client's token by asking the forge who owns it.
// Email is mandatory (git authorship): GitHub identities with a private email
// fall back to /user/emails, then to the noreply address.
func (c *Client) CurrentUser(ctx context.Context) (*User, error) {
	if c.kind == KindGitHub {
		return c.githubUser(ctx)
	}
	return c.gitlabUser(ctx)
}

func (c *Client) githubUser(ctx context.Context) (*User, error) {
	var gh struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	hdr, err := c.getJSONHeader(ctx, c.apiBase+"/user", &gh)
	if err != nil {
		return nil, err
	}
	u := &User{Subject: strconv.FormatInt(gh.ID, 10), Login: gh.Login, Name: gh.Name, Email: gh.Email}
	if raw := hdr.Get("X-OAuth-Scopes"); raw != "" {
		for _, s := range strings.Split(raw, ",") {
			u.Scopes = append(u.Scopes, strings.TrimSpace(s))
		}
	}
	if u.Name == "" {
		u.Name = u.Login
	}
	if u.Email == "" { // profile email hidden — ask for the verified primary
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if err := c.getJSON(ctx, c.apiBase+"/user/emails", &emails); err == nil {
			for _, e := range emails {
				if e.Primary && e.Verified {
					u.Email = e.Email
					break
				}
			}
		}
	}
	if u.Email == "" { // still hidden — GitHub's documented noreply form
		u.Email = fmt.Sprintf("%d+%s@users.noreply.github.com", gh.ID, gh.Login)
	}
	return u, nil
}

func (c *Client) gitlabUser(ctx context.Context) (*User, error) {
	var gl struct {
		ID          int64  `json:"id"`
		Username    string `json:"username"`
		Name        string `json:"name"`
		Email       string `json:"email"`
		CommitEmail string `json:"commit_email"`
	}
	if err := c.getJSON(ctx, c.apiBase+"/user", &gl); err != nil {
		return nil, err
	}
	u := &User{Subject: strconv.FormatInt(gl.ID, 10), Login: gl.Username, Name: gl.Name}
	if u.Name == "" {
		u.Name = gl.Username
	}
	u.Email = gl.CommitEmail
	if u.Email == "" {
		u.Email = gl.Email
	}
	if u.Email == "" {
		return nil, fmt.Errorf("gitlab reported no email for the token's user; email is required for git authorship")
	}
	return u, nil
}

// ProjectRole maps the token owner's permission on the client's project onto
// the deployment role ladder: "admin", "maintainer", "editor", "viewer" or
// "none". An error means the project was unreachable with this token — the
// caller should treat that as no access.
func (c *Client) ProjectRole(ctx context.Context) (string, error) {
	if c.kind == KindGitHub {
		var repo struct {
			Permissions struct {
				Admin    bool `json:"admin"`
				Maintain bool `json:"maintain"`
				Push     bool `json:"push"`
				Pull     bool `json:"pull"`
			} `json:"permissions"`
		}
		if err := c.getJSON(ctx, c.apiBase+"/repos/"+c.project, &repo); err != nil {
			return "none", err
		}
		p := repo.Permissions
		switch {
		case p.Admin:
			return "admin", nil
		case p.Maintain:
			return "maintainer", nil
		case p.Push:
			return "editor", nil
		case p.Pull:
			return "viewer", nil
		}
		return "none", nil
	}
	var proj struct {
		Permissions struct {
			ProjectAccess *struct {
				AccessLevel int `json:"access_level"`
			} `json:"project_access"`
			GroupAccess *struct {
				AccessLevel int `json:"access_level"`
			} `json:"group_access"`
		} `json:"permissions"`
	}
	if err := c.getJSON(ctx, c.apiBase+"/projects/"+url.PathEscape(c.project), &proj); err != nil {
		return "none", err
	}
	level := 0
	if a := proj.Permissions.ProjectAccess; a != nil && a.AccessLevel > level {
		level = a.AccessLevel
	}
	if a := proj.Permissions.GroupAccess; a != nil && a.AccessLevel > level {
		level = a.AccessLevel
	}
	switch { // GitLab access levels: 10 guest, 20 reporter, 30 developer, 40 maintainer, 50 owner
	case level >= 50:
		return "admin", nil
	case level >= 40:
		return "maintainer", nil
	case level >= 30:
		return "editor", nil
	case level >= 10:
		return "viewer", nil
	}
	return "none", nil
}

// CreateRequest opens a merge request / pull request from source onto target.
// Idempotent: when the branch already has an open request it is returned with
// created=false, so "propose" can be pressed twice without error.
func (c *Client) CreateRequest(ctx context.Context, source, target, title, body string) (req *Request, created bool, err error) {
	if existing, err := c.OpenRequest(ctx, source); err == nil && existing != nil {
		return existing, false, nil
	}
	if title == "" {
		title = "Merge " + source + " into " + target
	}
	if c.kind == KindGitHub {
		var pr struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			State   string `json:"state"`
			HTMLURL string `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		payload := map[string]string{"title": title, "head": source, "base": target, "body": body}
		if err := c.postJSON(ctx, c.apiBase+"/repos/"+c.project+"/pulls", payload, &pr); err != nil {
			return nil, false, err
		}
		return &Request{
			Number: pr.Number, Title: pr.Title, State: pr.State,
			Author: pr.User.Login, URL: pr.HTMLURL, Comments: []Comment{},
		}, true, nil
	}
	var mr struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		State  string `json:"state"`
		WebURL string `json:"web_url"`
		Author struct {
			Username string `json:"username"`
		} `json:"author"`
	}
	payload := map[string]string{
		"source_branch": source, "target_branch": target, "title": title, "description": body,
	}
	if err := c.postJSON(ctx, c.apiBase+"/projects/"+url.PathEscape(c.project)+"/merge_requests", payload, &mr); err != nil {
		return nil, false, err
	}
	return &Request{
		Number: mr.IID, Title: mr.Title, State: mr.State,
		Author: mr.Author.Username, URL: mr.WebURL, Comments: []Comment{},
	}, true, nil
}

func (c *Client) postJSON(ctx context.Context, url string, payload, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)
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
		return statusError(resp, string(body))
	}
	return json.Unmarshal(body, out)
}

// getJSONHeader is getJSON, additionally exposing the response headers
// (GitHub reports token scopes there).
func (c *Client) getJSONHeader(ctx context.Context, url string, out any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.authorize(req)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.Header, err
	}
	if resp.StatusCode/100 != 2 {
		return resp.Header, statusError(resp, string(body))
	}
	return resp.Header, json.Unmarshal(body, out)
}
