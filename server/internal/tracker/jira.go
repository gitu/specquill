// Package tracker files work items on issue trackers that are not git forges.
// Jira only, for now — GitHub/GitLab issues live in internal/forge next to
// the other forge calls.
package tracker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Jira talks to the Jira REST v2 API — v2, not v3, on purpose: it accepts a
// plain-text description (no ADF document) and is served by both Jira Cloud
// and Server/DC.
type Jira struct {
	base       string // web base, e.g. https://acme.atlassian.net
	projectKey string
	issueType  string
	credential string // "email:api_token" → Basic (Cloud); bare token → Bearer (Server/DC PAT)
	hc         *http.Client
}

// NewJira builds a client. issueType defaults to "Task".
func NewJira(baseURL, projectKey, issueType, credential string) *Jira {
	if issueType == "" {
		issueType = "Task"
	}
	return &Jira{
		base: strings.TrimSuffix(baseURL, "/"), projectKey: projectKey,
		issueType: issueType, credential: credential,
		hc: &http.Client{Timeout: 15 * time.Second},
	}
}

// authorize follows the importer convention for Atlassian credentials: a
// value containing ':' is email:api_token (HTTP Basic, Jira Cloud), a bare
// value is a personal access token (Bearer, Jira Server/DC).
func (j *Jira) authorize(req *http.Request) {
	if j.credential == "" {
		return
	}
	if strings.Contains(j.credential, ":") {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(j.credential)))
	} else {
		req.Header.Set("Authorization", "Bearer "+j.credential)
	}
}

func (j *Jira) do(ctx context.Context, method, url string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	j.authorize(req)
	resp, err := j.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		msg := strings.TrimSpace(string(raw))
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("jira: %s: %s", resp.Status, msg)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// BrowseURL is the human URL for an issue key.
func (j *Jira) BrowseURL(key string) string { return j.base + "/browse/" + key }

// CreateIssue files an issue and returns its key and browse URL.
func (j *Jira) CreateIssue(ctx context.Context, title, description string, labels []string) (key, browseURL string, err error) {
	// Jira labels must not contain spaces
	clean := make([]string, 0, len(labels))
	for _, l := range labels {
		if l = strings.ReplaceAll(strings.TrimSpace(l), " ", "-"); l != "" {
			clean = append(clean, l)
		}
	}
	payload := map[string]any{
		"fields": map[string]any{
			"project":     map[string]string{"key": j.projectKey},
			"summary":     title,
			"description": description,
			"issuetype":   map[string]string{"name": j.issueType},
			"labels":      clean,
		},
	}
	var out struct {
		Key string `json:"key"`
	}
	if err := j.do(ctx, http.MethodPost, j.base+"/rest/api/2/issue", payload, &out); err != nil {
		return "", "", err
	}
	return out.Key, j.BrowseURL(out.Key), nil
}

// FindIssue returns the existing issue carrying marker in its description
// (JQL over the drift label), or "" when none exists.
func (j *Jira) FindIssue(ctx context.Context, label, marker string) (key, browseURL string, err error) {
	jql := fmt.Sprintf(`labels = %q AND description ~ %q`, label, marker)
	q := j.base + "/rest/api/2/search?maxResults=50&fields=key&jql=" + url.QueryEscape(jql)
	var out struct {
		Issues []struct {
			Key string `json:"key"`
		} `json:"issues"`
	}
	if err := j.do(ctx, http.MethodGet, q, nil, &out); err != nil {
		return "", "", err
	}
	if len(out.Issues) == 0 {
		return "", "", nil
	}
	return out.Issues[0].Key, j.BrowseURL(out.Issues[0].Key), nil
}
