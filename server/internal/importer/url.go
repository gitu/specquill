package importer

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
)

// importURLs fetches each configured page and stores it under a stable,
// path-derived name. HTML is reduced to readable text so the copilot grounds on
// prose, not markup; markdown/text passes through untouched. An index.md lists
// every mirrored page with its origin URL.
func importURLs(ctx context.Context, hc *http.Client, src Source) (map[string]string, error) {
	urls := src.URLs
	if len(urls) == 0 && src.Remote != "" {
		urls = []string{src.Remote}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("url source %s has no urls", src.Name)
	}
	files := map[string]string{}
	names := map[string]string{} // url → stored path, for the index
	used := map[string]bool{}
	for _, u := range urls {
		body, err := get(ctx, hc, u, src.Token, "text/markdown, text/plain, text/html;q=0.9")
		if err != nil {
			return nil, err
		}
		content := string(body)
		name := urlFileName(u)
		if looksHTML(content) {
			content = htmlToText(content)
		}
		// de-dup colliding names deterministically
		base := strings.TrimSuffix(name, ".md")
		for n := 1; used[name]; n++ {
			name = fmt.Sprintf("%s-%d.md", base, n)
		}
		used[name] = true
		files["pages/"+name] = "<!-- source: " + u + " -->\n\n" + strings.TrimSpace(content) + "\n"
		names[u] = "pages/" + name
	}

	var b strings.Builder
	b.WriteString("# " + src.Name + "\n\nMirrored pages:\n\n")
	keys := make([]string, 0, len(names))
	for u := range names {
		keys = append(keys, u)
	}
	sort.Strings(keys)
	for _, u := range keys {
		fmt.Fprintf(&b, "- [%s](%s)\n", u, names[u])
	}
	files["index.md"] = b.String()
	return files, nil
}

// urlFileName derives `<last-path-segment>.md` (or the host) from a URL.
func urlFileName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return slugify(raw) + ".md"
	}
	stem := path.Base(strings.TrimSuffix(u.Path, "/"))
	if stem == "" || stem == "." || stem == "/" {
		stem = u.Host
	}
	stem = strings.TrimSuffix(stem, path.Ext(stem))
	return slugify(stem) + ".md"
}

func looksHTML(s string) bool {
	head := strings.ToLower(strings.TrimSpace(s))
	if len(head) > 200 {
		head = head[:200]
	}
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html") || strings.Contains(head, "<body")
}
