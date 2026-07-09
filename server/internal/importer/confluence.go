package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// importConfluence mirrors every page of a Confluence space via the REST API,
// converting each page's storage-format XHTML to readable text. Remote is the
// wiki base (e.g. https://acme.atlassian.net/wiki); auth comes from the source's
// token — "email:api_token" for Atlassian Cloud (Basic), a bare PAT for
// Server/DC (Bearer). Credentials are env-sourced upstream; never logged.
func importConfluence(ctx context.Context, hc *http.Client, src Source) (map[string]string, error) {
	base := strings.TrimRight(src.Remote, "/")
	const limit = 50
	type page struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Body  struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
		Version struct {
			Number int `json:"number"`
		} `json:"version"`
	}

	files := map[string]string{}
	titles := map[string]string{} // stored path → title, for the index
	used := map[string]bool{}
	for start := 0; ; start += limit {
		u := fmt.Sprintf("%s/rest/api/content?spaceKey=%s&expand=body.storage,version&limit=%d&start=%d",
			base, src.Space, limit, start)
		body, err := get(ctx, hc, u, src.Token, "application/json")
		if err != nil {
			return nil, err
		}
		var resp struct {
			Results []page `json:"results"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("parse confluence response: %w", err)
		}
		for _, p := range resp.Results {
			name := slugify(p.Title) + ".md"
			for n := 1; used[name]; n++ {
				name = fmt.Sprintf("%s-%d.md", slugify(p.Title), n)
			}
			used[name] = true
			stored := "pages/" + name
			text := htmlToText(p.Body.Storage.Value)
			files[stored] = fmt.Sprintf("# %s\n\n<!-- confluence page %s, v%d -->\n\n%s\n",
				p.Title, p.ID, p.Version.Number, text)
			titles[stored] = p.Title
		}
		if len(resp.Results) < limit {
			break // last page
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("confluence space %s returned no pages", src.Space)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s (Confluence space %s)\n\nMirrored pages:\n\n", src.Name, src.Space)
	paths := make([]string, 0, len(titles))
	for p := range titles {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Fprintf(&b, "- [%s](%s)\n", titles[p], p)
	}
	files["index.md"] = b.String()
	return files, nil
}
