package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"specquill/server/internal/docmodel"
	"specquill/server/internal/project"
)

// Link verification: every markdown link in a branch snapshot (reserved
// index/log files INCLUDED — their listings must stay navigable), classified
// and checked per class:
//
//	internal — resolves inside the workspace; target must exist on the branch
//	source   — ~<source>/<path>; the source must be granted (and selected, if
//	           the in-repo config selects) and the file must exist at its
//	           default branch
//	external — http(s); a bounded HEAD/GET probe, private addresses refused
//	           (SSRF guard), disabled with ?external=0
type linkResult struct {
	File   string `json:"file"`
	Href   string `json:"href"`
	Target string `json:"target,omitempty"`
	Kind   string `json:"kind"`   // internal | source | external
	Status string `json:"status"` // ok | broken | skipped
	Detail string `json:"detail,omitempty"`
}

type linkCounts struct {
	OK      int `json:"ok"`
	Broken  int `json:"broken"`
	Skipped int `json:"skipped,omitempty"`
}

var (
	lcLink   = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	lcFence  = regexp.MustCompile("(?s)```.*?```")
	lcScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

const (
	lcMaxExternal  = 64
	lcExternalPar  = 6
	lcProbeTimeout = 5 * time.Second
)

func (s *Server) getLinkCheck(w http.ResponseWriter, r *http.Request, repo *project.Project) {
	ref := repo.ResolveRef(r.URL.Query().Get("ref"))
	files, err := repo.Snapshot(ref)
	if err != nil {
		gitFail(w, err)
		return
	}
	// existence set from the tree — the snapshot alone misses binary assets
	exists := map[string]bool{}
	if entries, err := repo.Tree(ref); err == nil {
		for _, e := range entries {
			exists[e.Path] = true
		}
	}
	for p := range files {
		exists[p] = true
	}

	checkExternal := r.URL.Query().Get("external") != "0" && r.URL.Query().Get("external") != "false"

	// allowed sources + their snapshots, resolved lazily on first ~link
	var srcOnce sync.Once
	var srcAllowed map[string]bool
	var srcFiles map[string]map[string]string
	loadSources := func() {
		srcOnce.Do(func() {
			srcAllowed, srcFiles = map[string]bool{}, map[string]map[string]string{}
			t := s.tenantQuiet(r)
			if t == nil {
				return
			}
			granted, err := s.store.TenantGrantedSources(t.ID)
			if err != nil {
				return
			}
			kinds := map[string]string{}
			for _, src := range granted {
				kinds[src.Name] = src.Kind
			}
			names := make([]string, 0, len(kinds))
			// selection ∩ grants when the in-repo config selects; all grants
			// otherwise (same fallback as the tree's reference section)
			if yml, _, err := repo.FileAt(repo.Cfg.DefaultBranch, ".specquill/config.yml"); err == nil {
				if cfg, err := project.ParseConfig(yml); err == nil {
					if refs, _ := project.EffectiveReferences(cfg, kinds); len(refs) > 0 {
						for _, ref := range refs {
							names = append(names, ref.Source)
						}
					} else if cfg.References == nil {
						for n := range kinds {
							names = append(names, n)
						}
					}
				}
			} else {
				for n := range kinds {
					names = append(names, n)
				}
			}
			for _, n := range names {
				srcAllowed[n] = true
				if gr, ok := s.git.Repo(repo.Repo.Tenant() + "/" + n); ok {
					if snap := s.sourceSnapshot(repo.Repo.Tenant()+"/"+n, gr); snap != nil {
						srcFiles[n] = snap
					}
				}
			}
		})
	}

	var results []linkResult
	externalSeen := map[string][]int{} // url -> result indexes awaiting probe

	for _, p := range sortedKeys(files) {
		if !strings.HasSuffix(p, ".md") {
			continue
		}
		dir := ""
		if i := strings.LastIndex(p, "/"); i >= 0 {
			dir = p[:i]
		}
		seen := map[string]bool{}
		body := lcFence.ReplaceAllString(files[p], "")
		for _, m := range lcLink.FindAllStringSubmatch(body, -1) {
			href := m[1]
			bare := strings.SplitN(href, "#", 2)[0]
			if bare == "" || seen[href] {
				continue // pure anchor / duplicate in this file
			}
			seen[href] = true
			if lcScheme.MatchString(bare) {
				if !strings.HasPrefix(bare, "http://") && !strings.HasPrefix(bare, "https://") {
					continue // mailto:, data:, … — nothing to verify
				}
				res := linkResult{File: p, Href: href, Kind: "external", Status: "skipped", Detail: "external checks disabled"}
				if checkExternal {
					res.Status, res.Detail = "", ""
					externalSeen[bare] = append(externalSeen[bare], len(results))
				}
				results = append(results, res)
				continue
			}
			target := docmodel.ResolveHref(dir, bare)
			if strings.HasPrefix(target, "~") {
				name, rest, _ := strings.Cut(strings.TrimPrefix(target, "~"), "/")
				loadSources()
				res := linkResult{File: p, Href: href, Target: target, Kind: "source", Status: "ok"}
				if !srcAllowed[name] {
					res.Status, res.Detail = "broken", "source not granted/selected for this project"
				} else if snap := srcFiles[name]; snap == nil {
					res.Status, res.Detail = "skipped", "source repository unavailable"
				} else if _, ok := snap[rest]; !ok {
					res.Status, res.Detail = "broken", "file missing in source"
				}
				results = append(results, res)
				continue
			}
			// only targets that name a file are verifiable
			if !strings.Contains(target[strings.LastIndex(target, "/")+1:], ".") {
				continue
			}
			res := linkResult{File: p, Href: href, Target: target, Kind: "internal", Status: "ok"}
			if !exists[target] {
				res.Status, res.Detail = "broken", "no such file on this branch"
			}
			results = append(results, res)
		}
	}

	if checkExternal && len(externalSeen) > 0 {
		probeExternal(r.Context(), externalSeen, results)
	}

	counts := map[string]*linkCounts{"internal": {}, "source": {}, "external": {}}
	var problems []linkResult
	for _, res := range results {
		c := counts[res.Kind]
		switch res.Status {
		case "ok":
			c.OK++
		case "skipped":
			c.Skipped++
		default:
			c.Broken++
			problems = append(problems, res)
		}
	}
	jsonOK(w, map[string]any{"ref": ref, "counts": counts, "problems": problems})
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// probeExternal HEADs (falling back to GET) each unique URL with a bounded
// budget and writes the outcome into every result that referenced it.
func probeExternal(ctx context.Context, byURL map[string][]int, results []linkResult) {
	urls := make([]string, 0, len(byURL))
	for u := range byURL {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	if len(urls) > lcMaxExternal {
		for _, u := range urls[lcMaxExternal:] {
			for _, i := range byURL[u] {
				results[i].Status, results[i].Detail = "skipped", fmt.Sprintf("over the %d-URL probe cap", lcMaxExternal)
			}
		}
		urls = urls[:lcMaxExternal]
	}
	client := &http.Client{Timeout: lcProbeTimeout, Transport: &http.Transport{DialContext: safeDial}}
	sem := make(chan struct{}, lcExternalPar)
	var wg sync.WaitGroup
	for _, u := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			status, detail := probeURL(ctx, client, u)
			for _, i := range byURL[u] {
				results[i].Status, results[i].Detail = status, detail
			}
		}(u)
	}
	wg.Wait()
}

func probeURL(ctx context.Context, client *http.Client, url string) (string, string) {
	try := func(method string) (int, error) {
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("User-Agent", "specquill-linkcheck/1.0")
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		resp.Body.Close()
		return resp.StatusCode, nil
	}
	code, err := try(http.MethodHead)
	if err == nil && (code == http.StatusMethodNotAllowed || code == http.StatusForbidden) {
		code, err = try(http.MethodGet)
	}
	switch {
	case err != nil && strings.Contains(err.Error(), "private address"):
		return "skipped", "private address refused"
	case err != nil:
		return "broken", "unreachable: " + trimErr(err)
	case code >= 400:
		return "broken", fmt.Sprintf("HTTP %d", code)
	default:
		return "ok", ""
	}
}

func trimErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 {
		s = s[i+2:]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// safeDial refuses loopback/private/link-local targets — workspace content
// must not turn the link checker into an internal-network probe.
func safeDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := (&net.Resolver{}).LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return nil, fmt.Errorf("dial %s: private address refused", host)
		}
	}
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}
