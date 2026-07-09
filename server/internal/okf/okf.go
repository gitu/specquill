// Package okf implements Open Knowledge Format (v0.1) producer support:
// a workspace opts in by declaring `okf_version` in the frontmatter of its
// root index.md; reqbase then keeps the derived reserved files current —
// index.md per directory (grouped concept listings) and log.md (change
// history). Spec: github.com/GoogleCloudPlatform/knowledge-catalog/okf.
package okf

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Version is the spec version this producer emits.
const Version = "0.1"

// Reserved reports whether name is an OKF reserved filename (not a concept).
func Reserved(name string) bool { return name == "index.md" || name == "log.md" }

// Enabled reports whether the tree at root opted into OKF generation:
// its root index.md declares okf_version in frontmatter.
func Enabled(root string) bool {
	b, err := os.ReadFile(filepath.Join(root, "index.md"))
	if err != nil {
		return false
	}
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		return false
	}
	end := strings.Index(s[4:], "\n---")
	if end < 0 {
		return false
	}
	return strings.Contains(s[:end+4], "okf_version:")
}

var (
	fmRe    = regexp.MustCompile(`(?s)^---\n(.*?)\n---\n?`)
	titleRe = regexp.MustCompile(`(?m)^title:\s*(.+)$`)
	descRe  = regexp.MustCompile(`(?m)^description:\s*(.+)$`)
)

type concept struct {
	rel   string // bundle-relative path, slash-separated
	title string
	desc  string
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	return strings.Trim(s, `"'`)
}

func readConcept(root, rel string) concept {
	c := concept{rel: rel, title: strings.TrimSuffix(filepath.Base(rel), ".md")}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return c
	}
	if m := fmRe.FindSubmatch(b); m != nil {
		if t := titleRe.FindSubmatch(m[1]); t != nil {
			c.title = unquote(string(t[1]))
		}
		if d := descRe.FindSubmatch(m[1]); d != nil {
			c.desc = unquote(string(d[1]))
		}
	}
	return c
}

// GenerateIndexes (re)writes the root index.md and one index.md per
// directory that directly contains concept files. Hidden directories are
// skipped. Only files whose content actually changed are written; their
// bundle-relative paths are returned.
func GenerateIndexes(root string) ([]string, error) {
	byDir := map[string][]concept{} // "" = root
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".md") || Reserved(name) {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		dir := ""
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			dir = rel[:i]
		}
		byDir[dir] = append(byDir[dir], readConcept(root, rel))
		return nil
	})
	if err != nil {
		return nil, err
	}

	entry := func(c concept) string {
		if c.desc != "" {
			return fmt.Sprintf("- [%s](/%s) — %s\n", c.title, c.rel, c.desc)
		}
		return fmt.Sprintf("- [%s](/%s)\n", c.title, c.rel)
	}

	dirs := make([]string, 0, len(byDir))
	for d := range byDir {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	for d := range byDir {
		sort.Slice(byDir[d], func(i, j int) bool { return byDir[d][i].rel < byDir[d][j].rel })
	}

	var changed []string
	write := func(rel, content string) error {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if cur, err := os.ReadFile(abs); err == nil && string(cur) == content {
			return nil
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			return err
		}
		changed = append(changed, rel)
		return nil
	}

	// root index.md: preserve existing frontmatter (it carries okf_version),
	// regenerate the body
	head := "---\nokf_version: \"" + Version + "\"\n---\n"
	if b, err := os.ReadFile(filepath.Join(root, "index.md")); err == nil {
		if m := fmRe.FindString(string(b)); m != "" {
			head = m
		}
	}
	var b strings.Builder
	b.WriteString(head)
	b.WriteString("\n# Index\n")
	if rootDocs := byDir[""]; len(rootDocs) > 0 {
		b.WriteString("\n")
		for _, c := range rootDocs {
			b.WriteString(entry(c))
		}
	}
	for _, d := range dirs {
		fmt.Fprintf(&b, "\n## %s\n\n", d)
		for _, c := range byDir[d] {
			b.WriteString(entry(c))
		}
	}
	if err := write("index.md", b.String()); err != nil {
		return changed, err
	}

	// one index.md per directory with direct concepts
	for _, d := range dirs {
		var s strings.Builder
		fmt.Fprintf(&s, "# %s\n\n", d)
		for _, c := range byDir[d] {
			s.WriteString(entry(c))
		}
		if err := write(d+"/index.md", s.String()); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// LogEntry is one change in log.md.
type LogEntry struct {
	Date    string // ISO 8601 date (YYYY-MM-DD)
	Author  string
	Subject string // first line of the commit message
}

// actionWord picks the bold verb the spec's log format leads entries with.
func actionWord(subject string) string {
	s := strings.ToLower(subject)
	switch {
	case strings.HasPrefix(s, "add"), strings.HasPrefix(s, "create"), strings.HasPrefix(s, "new"):
		return "Added"
	case strings.HasPrefix(s, "remove"), strings.HasPrefix(s, "delete"), strings.HasPrefix(s, "drop"):
		return "Removed"
	case strings.HasPrefix(s, "merge"):
		return "Merged"
	default:
		return "Updated"
	}
}

// WriteLog renders log.md from entries (expected newest first) and writes it
// when changed, returning whether it did.
func WriteLog(root string, entries []LogEntry) (bool, error) {
	var b strings.Builder
	b.WriteString("# Log\n")
	last := ""
	for _, e := range entries {
		if e.Date != last {
			fmt.Fprintf(&b, "\n## %s\n\n", e.Date)
			last = e.Date
		}
		fmt.Fprintf(&b, "- **%s** %s (%s)\n", actionWord(e.Subject), e.Subject, e.Author)
	}
	abs := filepath.Join(root, "log.md")
	content := b.String()
	if cur, err := os.ReadFile(abs); err == nil && string(cur) == content {
		return false, nil
	}
	return true, os.WriteFile(abs, []byte(content), 0o644)
}

// Validate returns conformance violations (OKF v0.1 §9) for the tree at
// root: every non-reserved .md must carry parseable frontmatter with a
// non-empty type. Empty result = conformant bundle.
func Validate(root string) ([]string, error) {
	var violations []string
	typeRe := regexp.MustCompile(`(?m)^type:\s*\S`)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || Reserved(d.Name()) {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		m := fmRe.FindSubmatch(b)
		if m == nil {
			violations = append(violations, filepath.ToSlash(rel)+": no frontmatter")
			return nil
		}
		if !typeRe.Match(m[1]) {
			violations = append(violations, filepath.ToSlash(rel)+": missing type")
		}
		return nil
	})
	return violations, err
}
