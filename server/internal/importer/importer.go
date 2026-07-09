// Package importer materializes non-git sources (url lists, OpenAPI specs,
// Confluence spaces) into read-only files that the gitx mirror repo commits as
// snapshots. Importers are pure fetch→files functions; the Runner owns the git
// side, scheduling and status. Credentials arrive already resolved from
// token_env — importers never see environment variable NAMES, only the token.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// Source is the importer's resolved view of a catalog source.
type Source struct {
	Name   string
	Kind   string
	Remote string   // importer endpoint (single page / spec URL / confluence base)
	Token  string   // resolved secret (may be empty); "email:token" enables Basic auth
	URLs   []string // url kind: explicit page list
	Space  string   // confluence kind: space key
}

// An Importer fetches a source's content as repo-relative path → content. It
// must be deterministic for identical upstream state so the mirror stays quiet.
type Importer func(ctx context.Context, hc *http.Client, src Source) (map[string]string, error)

// For returns the importer for a source kind.
func For(kind string) (Importer, error) {
	switch kind {
	case "url":
		return importURLs, nil
	case "openapi":
		return importOpenAPI, nil
	case "confluence":
		return importConfluence, nil
	}
	return nil, fmt.Errorf("no importer for source kind %q", kind)
}

// get performs an authenticated GET and returns the body, failing on non-2xx.
// A "user:token" Token becomes HTTP Basic auth (Atlassian Cloud); a bare token
// becomes a Bearer header.
func get(ctx context.Context, hc *http.Client, url, token, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if token != "" {
		if user, pass, ok := strings.Cut(token, ":"); ok {
			req.SetBasicAuth(user, pass)
		} else {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8MB per fetch
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return body, nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify turns an arbitrary label into a filesystem-safe, lowercase stem.
func slugify(s string) string {
	s = slugRe.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "page"
	}
	if len(s) > 80 {
		s = strings.Trim(s[:80], "-")
	}
	return s
}
