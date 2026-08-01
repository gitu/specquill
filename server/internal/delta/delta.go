// Package delta reads what CHANGED inside a document between two versions —
// not as diff lines, but in the terms the workspace is written in: which
// frontmatter properties moved (a status going draft → approved), which
// normative statements were added, dropped or reworded, which sections came
// and went.
//
// Deliberately pure: two strings in, a struct out. Git supplies the versions
// (api/changes.go reads both blobs from the object database), the SPA renders
// the result, and the quick-tier AI summary is built from it.
package delta

import (
	"regexp"
	"sort"
	"strings"

	"specquill/server/internal/mdfm"
)

// PropChange is one frontmatter key whose value moved. Before/After are the
// rendered scalar (lists join with ", "); an empty side means the key was
// added or removed.
type PropChange struct {
	Key    string `json:"key"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// StmtChange is one normative statement (a `> **REQ-042.3 · MUST** — …`
// blockquote) that appeared, disappeared or was reworded.
type StmtChange struct {
	ID     string `json:"id"`
	Op     string `json:"op"` // added | removed | modified
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// DocDelta is the semantic change of one document in one commit.
type DocDelta struct {
	Path       string       `json:"path"`
	Status     string       `json:"status"` // A | M | D | R, from git
	Props      []PropChange `json:"props,omitempty"`
	Statements []StmtChange `json:"statements,omitempty"`
	Sections   []string     `json:"sections,omitempty"` // "+ Heading" / "- Heading"
	// no readable structure (binary, non-markdown, or prose-only edits) —
	// the client falls back to the text diff
	Plain bool `json:"plain,omitempty"`
}

// Empty reports whether anything structural was detected — the SPA falls back
// to the text diff when nothing was.
func (d DocDelta) Empty() bool {
	return len(d.Props) == 0 && len(d.Statements) == 0 && len(d.Sections) == 0
}

// statementRe matches the workspace's normative statement shape, as the
// requirements skill writes it: `> **REQ-042.3 · MUST** — <text>`. The
// separator is a middle dot in practice; a hyphen or bare space parses too so
// hand-written variants are not silently invisible.
var statementRe = regexp.MustCompile(`(?m)^\s*>\s*\*\*\s*([A-Za-z][\w.-]*)\s*(?:·|-|\|)?\s*(MUST|MUST NOT|SHALL|SHALL NOT|SHOULD|SHOULD NOT|MAY)\s*\*\*\s*(?:[—–-]\s*)?(.*)$`)

var headingRe = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+?)\s*$`)

// Diff compares two versions of a markdown document. Either side may be empty
// (an added or deleted file). Non-markdown content yields a Plain delta.
func Diff(path, status, before, after string) DocDelta {
	d := DocDelta{Path: path, Status: status}
	if !strings.HasSuffix(strings.ToLower(path), ".md") {
		d.Plain = true
		return d
	}
	fmB, bodyB, _ := mdfm.Split(before)
	fmA, bodyA, _ := mdfm.Split(after)
	d.Props = propChanges(fmB, fmA)
	d.Statements = stmtChanges(bodyB, bodyA)
	d.Sections = sectionChanges(bodyB, bodyA)
	if d.Empty() {
		d.Plain = true
	}
	return d
}

// propChanges compares two frontmatter blocks key by key. Values render the
// way the properties panel shows them (lists joined), which is what both the
// UI and the summary prompt want; ordering follows the AFTER document so a
// reader sees changes in document order, with removed keys last.
func propChanges(before, after string) []PropChange {
	b, a := parseProps(before), parseProps(after)
	var out []PropChange
	seen := map[string]bool{}
	for _, k := range keyOrder(after) {
		seen[k] = true
		if b[k] != a[k] {
			out = append(out, PropChange{Key: k, Before: b[k], After: a[k]})
		}
	}
	var gone []string
	for k := range b {
		if !seen[k] {
			gone = append(gone, k)
		}
	}
	sort.Strings(gone)
	for _, k := range gone {
		out = append(out, PropChange{Key: k, Before: b[k]})
	}
	return out
}

// parseProps is a deliberately small frontmatter reader: top-level scalars and
// lists (inline or block), rendered flat. It is not a YAML parser — nested
// structures collapse to their scalar leaves, which is enough to say "this key
// moved" without dragging the whole document model into the server.
func parseProps(fm string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(fm) == "" {
		return out
	}
	lines := strings.Split(fm, "\n")
	topRe := regexp.MustCompile(`^([A-Za-z_][\w-]*):\s*(.*)$`)
	for i := 0; i < len(lines); i++ {
		m := topRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		key, rest := m[1], strings.TrimSpace(m[2])
		switch {
		case strings.HasPrefix(rest, "["):
			out[key] = joinList(strings.Split(strings.Trim(rest, "[]"), ","))
		case rest == ">" || rest == "|":
			var buf []string
			for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t")) {
				buf = append(buf, strings.TrimSpace(lines[i+1]))
				i++
			}
			out[key] = strings.Join(buf, " ")
		case rest != "":
			out[key] = unquote(rest)
		default:
			var items []string
			for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], " ") || strings.HasPrefix(lines[i+1], "\t")) {
				it := strings.TrimSpace(lines[i+1])
				items = append(items, strings.TrimPrefix(it, "- "))
				i++
			}
			out[key] = joinList(items)
		}
	}
	return out
}

func keyOrder(fm string) []string {
	var out []string
	seen := map[string]bool{}
	topRe := regexp.MustCompile(`^([A-Za-z_][\w-]*):`)
	for _, ln := range strings.Split(fm, "\n") {
		if m := topRe.FindStringSubmatch(ln); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func joinList(items []string) string {
	var vals []string
	for _, it := range items {
		if v := unquote(strings.TrimSpace(it)); v != "" {
			vals = append(vals, v)
		}
	}
	return strings.Join(vals, ", ")
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// stmtChanges pairs normative statements by their id: same id, different text
// = a reworded requirement, which is the change a reviewer most wants to see.
func stmtChanges(before, after string) []StmtChange {
	b, bOrder := statements(before)
	a, aOrder := statements(after)
	var out []StmtChange
	for _, id := range aOrder {
		prev, had := b[id]
		switch {
		case !had:
			out = append(out, StmtChange{ID: id, Op: "added", After: a[id]})
		case prev != a[id]:
			out = append(out, StmtChange{ID: id, Op: "modified", Before: prev, After: a[id]})
		}
	}
	for _, id := range bOrder {
		if _, still := a[id]; !still {
			out = append(out, StmtChange{ID: id, Op: "removed", Before: b[id]})
		}
	}
	return out
}

// statements walks the body line by line so a statement WRAPPED over several
// blockquote lines is read whole — a reword on its second line is exactly the
// change this is here to catch.
func statements(body string) (map[string]string, []string) {
	out := map[string]string{}
	var order []string
	cur := ""
	for _, line := range strings.Split(body, "\n") {
		if m := statementRe.FindStringSubmatch(line); m != nil {
			id, keyword, text := m[1], m[2], strings.TrimSpace(m[3])
			if _, dup := out[id]; !dup {
				order = append(order, id)
			}
			out[id] = keyword + " — " + text
			cur = id
			continue
		}
		trimmed := strings.TrimSpace(line)
		if cur != "" && strings.HasPrefix(trimmed, ">") {
			if rest := strings.TrimSpace(strings.TrimPrefix(trimmed, ">")); rest != "" {
				out[cur] = strings.TrimSpace(out[cur] + " " + rest)
				continue
			}
		}
		cur = "" // any non-continuation line ends the statement
	}
	return out, order
}

func sectionChanges(before, after string) []string {
	b, a := headings(before), headings(after)
	var out []string
	for _, h := range a {
		if !contains(b, h) {
			out = append(out, "+ "+h)
		}
	}
	for _, h := range b {
		if !contains(a, h) {
			out = append(out, "- "+h)
		}
	}
	return out
}

func headings(body string) []string {
	var out []string
	// fenced code can contain #-lines that are not headings
	body = regexp.MustCompile("(?s)```.*?```").ReplaceAllString(body, "")
	for _, m := range headingRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[2])
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Summarize renders a delta as compact prose lines — the shape both the
// commit-summary prompt and a plain-text client want.
func (d DocDelta) Summarize() string {
	var b strings.Builder
	b.WriteString(d.Status + " " + d.Path + "\n")
	for _, p := range d.Props {
		switch {
		case p.Before == "":
			b.WriteString("  + " + p.Key + ": " + p.After + "\n")
		case p.After == "":
			b.WriteString("  - " + p.Key + " (was " + p.Before + ")\n")
		default:
			b.WriteString("  ~ " + p.Key + ": " + p.Before + " → " + p.After + "\n")
		}
	}
	for _, s := range d.Statements {
		switch s.Op {
		case "added":
			b.WriteString("  + " + s.ID + " " + s.After + "\n")
		case "removed":
			b.WriteString("  - " + s.ID + " " + s.Before + "\n")
		default:
			b.WriteString("  ~ " + s.ID + ": " + s.Before + " → " + s.After + "\n")
		}
	}
	for _, s := range d.Sections {
		b.WriteString("  " + s + " (section)\n")
	}
	return b.String()
}
