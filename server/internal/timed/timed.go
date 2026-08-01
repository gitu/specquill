// Package timed derives the workspace's timed dependencies (REQ-026) from a
// branch snapshot: documents carrying a validity window, bucketed against a
// date, with the readiness of everything that links to them.
//
// The SPA owns the same derivation (web/src/lib/derive.ts buildTimed) because
// it owns the document model; this is the server-side reading the AI assistant
// needs, since the speccy has no browser to ask. It is deliberately the
// narrower of the two: window keys, buckets, dependents and readiness — no
// entity icons, no graph. Keep the DEFAULTS below in step with
// web/src/lib/config.ts DEFAULT_TIMED (the config sample spells both out).
package timed

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"specquill/server/internal/docmodel"
	"specquill/server/internal/mdfm"
)

// Def is the `timed:` config section: which frontmatter keys carry a window,
// when a dependent counts as ready, how far ahead the horizon reaches, and
// which families join the timeline (empty = all).
type Def struct {
	Start         []string
	End           []string
	ReadyStatuses []string
	HorizonDays   int
	Kinds         []string
}

// Defaults mirror web/src/lib/config.ts DEFAULT_TIMED.
func Defaults() Def {
	return Def{
		Start:         []string{"starts", "effective_from", "valid_from"},
		End:           []string{"ends", "effective_until", "valid_until", "due"},
		ReadyStatuses: []string{"approved", "done"},
		HorizonDays:   90,
	}
}

// ParseDef reads the `timed:` section of a workspace config over the
// defaults, key by key — a config naming only `horizon_days` keeps the
// built-in key lists.
func ParseDef(configYml string) Def {
	def := Defaults()
	if strings.TrimSpace(configYml) == "" {
		return def
	}
	var raw struct {
		Timed struct {
			Start         []string `yaml:"start"`
			End           []string `yaml:"end"`
			ReadyStatuses []string `yaml:"ready_statuses"`
			HorizonDays   int      `yaml:"horizon_days"`
			Kinds         []string `yaml:"kinds"`
		} `yaml:"timed"`
	}
	if yaml.Unmarshal([]byte(configYml), &raw) != nil {
		return def
	}
	t := raw.Timed
	if len(t.Start) > 0 {
		def.Start = t.Start
	}
	if len(t.End) > 0 {
		def.End = t.End
	}
	if len(t.ReadyStatuses) > 0 {
		def.ReadyStatuses = t.ReadyStatuses
	}
	if t.HorizonDays > 0 {
		def.HorizonDays = t.HorizonDays
	}
	def.Kinds = t.Kinds
	return def
}

// Dep is a document depending on a timed one, and whether it is ready in time.
type Dep struct {
	Path   string
	Title  string
	Status string
	Ready  bool
}

// Item is one timed document with its derived state.
type Item struct {
	Path, ID, Title, Status string
	Kind                    string // family key, derived from the config's folders
	Start, End              string // yyyy-mm-dd ("" when absent)
	StartKey, EndKey        string // which configured key each was read from
	State                   string // pending | active | expiring | expired
	Days                    int    // days to the governing date (negative = past)
	Governing               string
	Deps                    []Dep
	ReadyCount              int
	AtRisk                  bool
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// Build derives the timeline from a snapshot (path → content) and the
// workspace config, against `today`.
func Build(files map[string]string, configYml string, today time.Time) []Item {
	def := ParseDef(configYml)
	folders := entityFolders(configYml)
	ready := set(def.ReadyStatuses)
	kinds := set(def.Kinds)
	day := today.Truncate(24 * time.Hour)

	// pass 1: every markdown document's frontmatter, once
	type doc struct {
		fm     string
		links  []string // every path-looking frontmatter link value, resolved
		title  string
		status string
		id     string
	}
	docs := map[string]doc{}
	for path, content := range files {
		if !strings.HasSuffix(path, ".md") || strings.HasPrefix(path, ".") {
			continue
		}
		fm, _, _ := mdfm.Split(content)
		if fm == "" {
			continue
		}
		d := doc{fm: fm, title: scalar(fm, "title"), status: scalar(fm, "status"), id: scalar(fm, "id")}
		dir := ""
		if i := strings.LastIndex(path, "/"); i >= 0 {
			dir = path[:i]
		}
		for _, field := range docmodel.LinkFields {
			for _, v := range list(fm, field) {
				d.links = append(d.links, resolveFmRef(files, dir, v))
			}
		}
		// untyped body links count as dependents too — the SPA's backlink
		// index includes them ("in text"), and a spec that only cites a
		// requirement in prose still depends on it
		if !reservedMd(path) {
			for _, v := range bodyLinks(content) {
				d.links = append(d.links, resolveFmRef(files, dir, v))
			}
		}
		docs[path] = d
	}

	var out []Item
	for path, d := range docs {
		kind := kindOf(path, folders)
		if len(kinds) > 0 && !kinds[kind] {
			continue
		}
		startKey, start := firstKey(d.fm, def.Start)
		endKey, end := firstKey(d.fm, def.End)
		if start == "" && end == "" {
			continue
		}
		it := Item{
			Path: path, ID: d.id, Title: d.title, Status: d.status, Kind: kind,
			Start: start, End: end, StartKey: startKey, EndKey: endKey,
		}
		it.State, it.Days, it.Governing = state(start, end, day, def.HorizonDays)

		for depPath, dep := range docs {
			if depPath == path {
				continue
			}
			for _, l := range dep.links {
				if l != path {
					continue
				}
				isReady := ready[dep.status]
				it.Deps = append(it.Deps, Dep{Path: depPath, Title: dep.title, Status: dep.status, Ready: isReady})
				if isReady {
					it.ReadyCount++
				}
				break
			}
		}
		sort.Slice(it.Deps, func(i, j int) bool { return it.Deps[i].Path < it.Deps[j].Path })
		it.AtRisk = (it.State == "pending" || it.State == "expiring") && it.Days <= def.HorizonDays &&
			(!ready[it.Status] || it.ReadyCount < len(it.Deps))
		out = append(out, it)
	}
	// pending first, then soonest — the same reading order as the timeline view
	order := map[string]int{"pending": 0, "active": 1, "expiring": 2, "expired": 3}
	sort.Slice(out, func(i, j int) bool {
		if order[out[i].State] != order[out[j].State] {
			return order[out[i].State] < order[out[j].State]
		}
		if abs(out[i].Days) != abs(out[j].Days) {
			return abs(out[i].Days) < abs(out[j].Days)
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func state(start, end string, today time.Time, horizon int) (string, int, string) {
	toStart, hasStart := days(today, start)
	toEnd, hasEnd := days(today, end)
	switch {
	case hasStart && toStart > 0:
		return "pending", toStart, start
	case hasEnd && toEnd < 0:
		return "expired", toEnd, end
	case hasEnd && toEnd <= horizon:
		return "expiring", toEnd, end
	case hasEnd:
		return "active", toEnd, end
	default:
		return "active", toStart, start
	}
}

func days(today time.Time, date string) (int, bool) {
	if !dateRe.MatchString(date) {
		return 0, false
	}
	t, err := time.Parse("2006-01-02", date[:10])
	if err != nil {
		return 0, false
	}
	return int(t.Sub(today).Hours() / 24), true
}

// Text renders the timeline the way the assistant reads best: one line per
// document, unfinished dependents named underneath.
func Text(items []Item, def Def, today time.Time) string {
	if len(items) == 0 {
		return "No document carries a validity window. Documents join the timeline by " +
			"carrying one of these frontmatter keys: " + strings.Join(def.Start, "/") +
			" (start) or " + strings.Join(def.End, "/") + " (end)."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %d timed documents · horizon %dd · ready = %s\n",
		today.Format("2006-01-02"), len(items), def.HorizonDays, strings.Join(def.ReadyStatuses, "/"))
	for _, it := range items {
		window := it.Start + " → " + it.End
		if it.End == "" {
			window = "from " + it.Start
		} else if it.Start == "" {
			window = "until " + it.End
		}
		risk := ""
		if it.AtRisk {
			risk = "  AT RISK"
		}
		fmt.Fprintf(&b, "%-8s %5dd  %s  %s  status=%s  window=%s  dependents %d/%d ready%s\n",
			strings.ToUpper(it.State), it.Days, it.Path, orDash(it.Title), orDash(it.Status),
			window, it.ReadyCount, len(it.Deps), risk)
		for _, d := range it.Deps {
			if !d.Ready {
				fmt.Fprintf(&b, "    not ready: %s (%s)\n", d.Path, orDash(d.Status))
			}
		}
	}
	return b.String()
}

// ---------------------------------------------------------------- helpers

var (
	bodyLinkRe = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	fenceRe    = regexp.MustCompile("(?s)```.*?```")
	reservedRe = regexp.MustCompile(`(^|/)(index|log)\.md$`)
	schemeRe   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z+.-]*:`)
)

// reservedMd: OKF index/log files are generated listings, not documents that
// depend on anything (the SPA skips them the same way).
func reservedMd(path string) bool { return reservedRe.MatchString(path) }

// bodyLinks returns the markdown links in a document body, fences stripped so
// code samples do not fabricate dependents. External URLs and ~source refs
// drop out.
func bodyLinks(content string) []string {
	_, body, _ := mdfm.Split(content)
	var out []string
	for _, m := range bodyLinkRe.FindAllStringSubmatch(fenceRe.ReplaceAllString(body, ""), -1) {
		t := strings.SplitN(m[1], "#", 2)[0]
		if t == "" || !strings.HasSuffix(t, ".md") || strings.HasPrefix(t, "~") || schemeRe.MatchString(t) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func firstKey(fm string, keys []string) (string, string) {
	for _, k := range keys {
		if v := scalar(fm, k); dateRe.MatchString(v) {
			return k, v[:10]
		}
	}
	return "", ""
}

// entityFolders maps a folder prefix to the family key the config gives it,
// so `kinds:` can narrow the timeline. Documents outside any configured
// folder get their folder name as kind.
func entityFolders(configYml string) map[string]string {
	out := map[string]string{
		"regulations/": "regulation", "requirements/": "requirement", "specs/": "spec",
		"data-mappings/": "data_mapping", "diagrams/": "diagram", "work-items/": "work_item",
	}
	var raw struct {
		Entities map[string]struct {
			Folder string `yaml:"folder"`
			Hidden bool   `yaml:"hidden"`
		} `yaml:"entities"`
	}
	if yaml.Unmarshal([]byte(configYml), &raw) != nil {
		return out
	}
	for kind, e := range raw.Entities {
		if e.Hidden || e.Folder == "" {
			continue
		}
		folder := e.Folder
		if !strings.HasSuffix(folder, "/") {
			folder += "/"
		}
		out[folder] = kind
	}
	return out
}

func kindOf(path string, folders map[string]string) string {
	best, bestLen := "", -1
	for folder, kind := range folders {
		if strings.HasPrefix(path, folder) && len(folder) > bestLen {
			best, bestLen = kind, len(folder)
		}
	}
	if best != "" {
		return best
	}
	if i := strings.Index(path, "/"); i > 0 {
		return path[:i]
	}
	return ""
}

// resolveFmRef mirrors the SPA's tolerant frontmatter-link resolution
// (web/src/lib/model.ts): root-relative is the canonical form the pickers
// write, so try the value as-is against the snapshot first and fall back to
// document-relative. Resolving strictly relative — as body links do — would
// turn `implements: requirements/REQ-042.md` on specs/txn.md into
// specs/requirements/REQ-042.md and silently lose every dependent.
func resolveFmRef(files map[string]string, dir, ref string) string {
	bare := strings.TrimPrefix(strings.SplitN(ref, "#", 2)[0], "/")
	if _, ok := files[bare]; ok {
		return bare
	}
	if rel := docmodel.ResolveHref(dir, ref); rel != bare {
		if _, ok := files[rel]; ok {
			return rel
		}
	}
	return bare
}

func scalar(fm, key string) string {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*(.+)$`).FindStringSubmatch(fm)
	if m == nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(m[1]), `"'`)
}

// list reads a frontmatter list in either shape (inline [a, b] or block).
func list(fm, key string) []string {
	if m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:\s*\[(.*)\]\s*$`).FindStringSubmatch(fm); m != nil {
		var out []string
		for _, v := range strings.Split(m[1], ",") {
			if v = strings.Trim(strings.TrimSpace(v), `"'`); v != "" {
				out = append(out, strings.SplitN(v, "#", 2)[0])
			}
		}
		return out
	}
	m := regexp.MustCompile(`(?ms)^` + regexp.QuoteMeta(key) + `:\s*\n(.*?)(?:^\S|\z)`).FindStringSubmatch(fm)
	if m == nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(m[1], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		if v := strings.Trim(strings.TrimSpace(line[2:]), `"'`); v != "" && !strings.Contains(v, ": ") {
			out = append(out, strings.SplitN(v, "#", 2)[0])
		}
	}
	return out
}

func set(vs []string) map[string]bool {
	m := make(map[string]bool, len(vs))
	for _, v := range vs {
		m[v] = true
	}
	return m
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
