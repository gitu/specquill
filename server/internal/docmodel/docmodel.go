// Package docmodel scans a workspace tree into a lightweight document model
// for the CLI (validate / graph / export). The server itself never parses
// frontmatter — the SPA computes its own model client-side; this package
// exists so the CLI works on a plain checkout with no server running.
package docmodel

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"specquill/server/internal/okf"
)

// LinkFields are the typed frontmatter link lists that build traceability.
var LinkFields = []string{"implements", "satisfies", "maps_to", "verifies", "drives"}

type Doc struct {
	Path        string              `json:"path"`
	Type        string              `json:"type,omitempty"`
	ID          string              `json:"id,omitempty"`
	Title       string              `json:"title"`
	Status      string              `json:"status,omitempty"`
	Description string              `json:"description,omitempty"`
	Links       map[string][]string `json:"links,omitempty"`      // typed frontmatter links (incl. drivers refs)
	References  []string            `json:"references,omitempty"` // untyped body links, bundle-relative
}

var (
	fmRe      = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)
	bodyLink  = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	fenceRe   = regexp.MustCompile("(?s)```.*?```")
	driverRef = regexp.MustCompile(`(?m)^\s+ref:\s*(.+)$`)
)

func scalar(fm, key string) string {
	m := regexp.MustCompile(`(?m)^` + key + `:\s*(.+)$`).FindStringSubmatch(fm)
	if m == nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(m[1]), `"'`)
}

func list(fm, key string) []string {
	// inline: key: [a, b]
	if m := regexp.MustCompile(`(?m)^` + key + `:\s*\[(.*?)\]`).FindStringSubmatch(fm); m != nil {
		var out []string
		for _, it := range strings.Split(m[1], ",") {
			if v := strings.Trim(strings.TrimSpace(it), `"'`); v != "" {
				out = append(out, v)
			}
		}
		return out
	}
	// block list
	m := regexp.MustCompile(`(?ms)^` + key + `:\s*\n(.*?)(?:^\S|\z)`).FindStringSubmatch(fm)
	if m == nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(m[1], "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "- "); ok {
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			// skip map items ("- type: regulatory"); drivers are handled below
			if v != "" && !regexp.MustCompile(`^[\w-]+:`).MatchString(v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// Scan walks root and parses every concept file (reserved OKF files and
// hidden directories are skipped).
func Scan(root string) ([]Doc, error) {
	var docs []Doc
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || okf.Reserved(d.Name()) {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		docs = append(docs, parse(rel, string(b)))
		return nil
	})
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	return docs, err
}

func parse(rel, content string) Doc {
	doc := Doc{Path: rel, Title: strings.TrimSuffix(filepath.Base(rel), ".md"), Links: map[string][]string{}}
	fm, body := "", content
	if m := fmRe.FindStringSubmatch(content); m != nil {
		fm, body = m[1], content[len(m[0]):]
	}
	if t := scalar(fm, "title"); t != "" {
		doc.Title = t
	}
	doc.Type = scalar(fm, "type")
	doc.ID = scalar(fm, "id")
	doc.Status = scalar(fm, "status")
	doc.Description = scalar(fm, "description")
	for _, f := range LinkFields {
		if vs := list(fm, f); len(vs) > 0 {
			doc.Links[f] = vs
		}
	}
	// drivers: block of {type, ref} maps — collect the refs
	if m := regexp.MustCompile(`(?ms)^drivers:\s*\n(.*?)(?:^\S|\z)`).FindStringSubmatch(fm); m != nil {
		for _, r := range driverRef.FindAllStringSubmatch(m[1], -1) {
			doc.Links["drivers"] = append(doc.Links["drivers"], strings.Trim(strings.TrimSpace(r[1]), `"'`))
		}
	}
	// untyped body links, resolved bundle-relative (external URLs skipped)
	dir := ""
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		dir = rel[:i]
	}
	seen := map[string]bool{}
	for _, m := range bodyLink.FindAllStringSubmatch(fenceRe.ReplaceAllString(body, ""), -1) {
		t := strings.SplitN(m[1], "#", 2)[0]
		if t == "" || !strings.HasSuffix(t, ".md") || regexp.MustCompile(`^[a-zA-Z][a-zA-Z+.-]*:`).MatchString(t) {
			continue
		}
		target := t
		if strings.HasPrefix(t, "/") {
			target = t[1:]
		} else {
			target = resolve(dir, t)
		}
		if target != rel && !seen[target] {
			seen[target] = true
			doc.References = append(doc.References, target)
		}
	}
	return doc
}

func resolve(dir, rel string) string {
	parts := []string{}
	if dir != "" {
		parts = strings.Split(dir, "/")
	}
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		case ".", "":
		default:
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}

// BrokenLinks returns "<from>: <field> -> <target>" for every typed link or
// body reference whose .md target does not exist in the tree. Non-path
// driver refs (prose like "Ops T+1 SLA") and #anchors are ignored.
func BrokenLinks(root string, docs []Doc) []string {
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		return err == nil
	}
	var out []string
	check := func(from, field, target string) {
		t := strings.SplitN(target, "#", 2)[0]
		if !strings.HasSuffix(t, ".md") {
			return // prose refs and field anchors are not files
		}
		if !exists(t) {
			out = append(out, from+": "+field+" -> "+t)
		}
	}
	for _, d := range docs {
		for field, targets := range d.Links {
			for _, t := range targets {
				check(d.Path, field, t)
			}
		}
		for _, r := range d.References {
			check(d.Path, "reference", r)
		}
	}
	sort.Strings(out)
	return out
}
