package forge

// Issue creation for drift findings (REQ: drift work items). The only write
// besides CreateRequest — issues carry a fingerprint marker in their body so
// re-filing finds the existing issue instead of duplicating it.

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Issue is a created (or re-found) forge issue.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

// DriftLabel tags every issue specquill files, and scopes the marker search.
const DriftLabel = "specquill-drift"

// CreateIssue opens an issue on the client's project.
func (c *Client) CreateIssue(ctx context.Context, title, body string, labels []string) (*Issue, error) {
	if c == nil {
		return nil, fmt.Errorf("forge not configured")
	}
	if c.kind == KindGitHub {
		var out struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			State   string `json:"state"`
			HTMLURL string `json:"html_url"`
		}
		payload := map[string]any{"title": title, "body": body, "labels": labels}
		if err := c.postJSON(ctx, fmt.Sprintf("%s/repos/%s/issues", c.apiBase, c.project), payload, &out); err != nil {
			return nil, err
		}
		return &Issue{Number: out.Number, Title: out.Title, State: out.State, URL: out.HTMLURL}, nil
	}
	var out struct {
		IID    int    `json:"iid"`
		Title  string `json:"title"`
		State  string `json:"state"`
		WebURL string `json:"web_url"`
	}
	payload := map[string]any{"title": title, "description": body, "labels": strings.Join(labels, ",")}
	if err := c.postJSON(ctx, fmt.Sprintf("%s/projects/%s/issues", c.apiBase, url.PathEscape(c.project)), payload, &out); err != nil {
		return nil, err
	}
	return &Issue{Number: out.IID, Title: out.Title, State: out.State, URL: out.WebURL}, nil
}

// FindIssueByMarker returns the existing issue whose body carries marker, or
// nil. Scoped to the DriftLabel so the scan stays one page of our own issues,
// any state — a closed issue still short-circuits a re-file (the drift
// resurfaced; the conversation lives on the tracker).
func (c *Client) FindIssueByMarker(ctx context.Context, marker string) (*Issue, error) {
	if c == nil {
		return nil, fmt.Errorf("forge not configured")
	}
	if c.kind == KindGitHub {
		var issues []struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			State   string `json:"state"`
			Body    string `json:"body"`
			HTMLURL string `json:"html_url"`
			PR      any    `json:"pull_request"` // /issues lists PRs too — skip them
		}
		q := fmt.Sprintf("%s/repos/%s/issues?state=all&labels=%s&per_page=100", c.apiBase, c.project, url.QueryEscape(DriftLabel))
		if err := c.getJSON(ctx, q, &issues); err != nil {
			return nil, err
		}
		for _, is := range issues {
			if is.PR == nil && strings.Contains(is.Body, marker) {
				return &Issue{Number: is.Number, Title: is.Title, State: is.State, URL: is.HTMLURL}, nil
			}
		}
		return nil, nil
	}
	var issues []struct {
		IID         int    `json:"iid"`
		Title       string `json:"title"`
		State       string `json:"state"`
		Description string `json:"description"`
		WebURL      string `json:"web_url"`
	}
	q := fmt.Sprintf("%s/projects/%s/issues?labels=%s&per_page=100", c.apiBase, url.PathEscape(c.project), url.QueryEscape(DriftLabel))
	if err := c.getJSON(ctx, q, &issues); err != nil {
		return nil, err
	}
	for _, is := range issues {
		if strings.Contains(is.Description, marker) {
			return &Issue{Number: is.IID, Title: is.Title, State: is.State, URL: is.WebURL}, nil
		}
	}
	return nil, nil
}
