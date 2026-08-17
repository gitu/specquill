// Package refactor rewrites references when a document moves — the server
// twin of web/src/lib/refactor.ts. It detects every way a link can be
// authored (relative, root-relative, tolerant bare paths, with anchors,
// image embeds, typed frontmatter link lists) via the same resolution rules
// the SPA uses. Rewrites are string surgery on the raw bytes: untouched
// documents come back byte-identical, never YAML round-trips.
package refactor

import (
	"regexp"
	"sort"
	"strings"
)

// relLink returns the relative markdown path from a document's directory to
// a target path.
func relLink(fromDir, target string) string {
	var from []string
	if fromDir != "" {
		from = strings.Split(fromDir, "/")
	}
	to := strings.Split(target, "/")
	i := 0
	for i < len(from) && i < len(to)-1 && from[i] == to[i] {
		i++
	}
	return strings.Repeat("../", len(from)-i) + strings.Join(to[i:], "/")
}

// resolvePath resolves a relative href against a base directory (both
// root-relative, "/"-separated; ".." above the root is dropped).
func resolvePath(baseDir, rel string) string {
	var parts []string
	if baseDir != "" {
		parts = strings.Split(baseDir, "/")
	}
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		case ".", "":
			// skip
		default:
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "/")
}

// resolveDocHref normalizes a body-link href to a root-relative workspace
// path: "~source" paths pass through, "/abs" strips the slash, everything
// else resolves against the linking document's directory.
func resolveDocHref(dir, href string) string {
	clean := href
	if i := strings.IndexByte(clean, '#'); i >= 0 {
		clean = clean[:i]
	}
	if strings.HasPrefix(clean, "~") {
		return clean
	}
	if strings.HasPrefix(clean, "/") {
		return strings.TrimLeft(clean, "/")
	}
	return resolvePath(dir, clean)
}

var frontmatterRe = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n?(.*)\z`)

// stripFrontmatter splits a document into frontmatter and body, consuming
// one newline after the closing "---" so assemble is its exact inverse.
func stripFrontmatter(md string) (fm, body string) {
	if m := frontmatterRe.FindStringSubmatch(md); m != nil {
		return m[1], m[2]
	}
	return "", md
}

// assemble reassembles a markdown file from frontmatter + body —
// byte-identical to the original for an untouched stripFrontmatter pair.
func assemble(fm, body string) string {
	if strings.TrimSpace(fm) == "" {
		return body
	}
	return "---\n" + fm + "\n---\n" + body
}

var (
	bodyLinkRe = regexp.MustCompile(`\]\([^)\s]+\)`)
	schemeRe   = regexp.MustCompile(`(?i)^[a-z][a-z+.-]*:`)
)

// Frontmatter link values sit inside YAML flow/block lists or plain scalars
// (legacy driver-map ref: included): a path counts as a reference only when
// delimited by these characters (or the string edge / an anchor suffix).
const (
	fmBefore = " \t\r\n\f[,\"'"
	fmAfter  = " \t\r\n\f],\"'#"
)

// RewriteRefs rewrites every reference to from inside one document: body
// links become RELATIVE links to the new location; typed frontmatter entries
// (stored root-relative, optional #anchor) get the new root-relative path.
// The bool reports whether anything was rewritten (false = doc unchanged).
func RewriteRefs(doc, docPath, from, to string) (string, bool) {
	dir := ""
	if i := strings.LastIndexByte(docPath, '/'); i >= 0 {
		dir = docPath[:i]
	}
	n := 0
	fm, body := stripFrontmatter(doc)
	newBody := bodyLinkRe.ReplaceAllStringFunc(body, func(all string) string {
		href := all[2 : len(all)-1] // between "](" and ")"
		target, anchor := href, ""
		if h := strings.IndexByte(href, '#'); h >= 0 {
			target, anchor = href[:h], href[h:]
		}
		if target == "" || schemeRe.MatchString(target) || resolveDocHref(dir, target) != from {
			return all
		}
		n++
		return "](" + relLink(dir, to) + anchor + ")"
	})
	// frontmatter values are canonically root-relative, but doc-relative and
	// leading-"/" spellings resolve too (mirroring the SPA's resolveFmRef) —
	// all of them normalize to the canonical root-relative new path
	newFm := fm
	for _, pat := range fmSpellings(dir, from) {
		newFm = rewriteFrontmatter(newFm, pat, to, &n)
	}
	if n == 0 {
		return doc, false
	}
	if fm != "" {
		return assemble(newFm, newBody), true
	}
	return newBody, true
}

// fmSpellings lists the ways a frontmatter value can spell the moved path
// from a document in dir: canonical root-relative, "/"-prefixed, and the
// doc-relative form (which for a same-dir target is the bare filename —
// only matched from that dir, so it cannot hit unrelated files).
func fmSpellings(dir, from string) []string {
	out := []string{from, "/" + from}
	if rel := relLink(dir, from); rel != from {
		out = append(out, rel)
	}
	return out
}

// rewriteFrontmatter replaces delimited occurrences of from in the raw
// frontmatter string (a manual scan — RE2 has no lookahead, and the trailing
// delimiter must not be consumed so a #anchor survives the rewrite).
func rewriteFrontmatter(fm, from, to string, n *int) string {
	if fm == "" || from == "" {
		return fm
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(fm[i:], from)
		if j < 0 {
			break
		}
		j += i
		end := j + len(from)
		beforeOK := j == 0 || strings.IndexByte(fmBefore, fm[j-1]) >= 0
		afterOK := end == len(fm) || strings.IndexByte(fmAfter, fm[end]) >= 0
		if beforeOK && afterOK {
			b.WriteString(fm[i:j])
			b.WriteString(to)
			*n++
			i = end
		} else {
			b.WriteString(fm[i : j+1])
			i = j + 1
		}
	}
	b.WriteString(fm[i:])
	return b.String()
}

var fmRelRe = regexp.MustCompile(`(^|[ \t\[,"'])((?:\.\./|\./)[^ \t\],"'#\n]+)`)

// RebaseLinks rewrites the MOVED document's own outbound links so they keep
// resolving from its new location: relative body links (doc links, images —
// the embedded *.excalidraw.png / *.mermaid diagrams in particular) are
// re-relativized against the new directory, and relative frontmatter values
// normalize to the canonical root-relative form. Root-relative and external
// links need no change and are left byte-identical. exists() decides whether
// a resolved target is real — unresolvable links stay untouched rather than
// being rewritten into different-but-still-broken ones.
func RebaseLinks(doc, fromPath, toPath string, exists func(rel string) bool) (string, bool) {
	oldDir, newDir := "", ""
	if i := strings.LastIndexByte(fromPath, '/'); i >= 0 {
		oldDir = fromPath[:i]
	}
	if i := strings.LastIndexByte(toPath, '/'); i >= 0 {
		newDir = toPath[:i]
	}
	if oldDir == newDir {
		return doc, false // rename in place — every relative link still holds
	}
	n := 0
	fm, body := stripFrontmatter(doc)
	newBody := bodyLinkRe.ReplaceAllStringFunc(body, func(all string) string {
		href := all[2 : len(all)-1] // between "](" and ")"
		target, anchor := href, ""
		if h := strings.IndexByte(href, '#'); h >= 0 {
			target, anchor = href[:h], href[h:]
		}
		if target == "" || schemeRe.MatchString(target) ||
			strings.HasPrefix(target, "~") || strings.HasPrefix(target, "/") {
			return all
		}
		resolved := resolvePath(oldDir, target)
		if !exists(resolved) {
			return all
		}
		next := relLink(newDir, resolved)
		if next == target {
			return all
		}
		n++
		return "](" + next + anchor + ")"
	})
	// clearly-relative frontmatter values (./ or ../) resolved from the old
	// directory become canonical root-relative paths
	newFm := fmRelRe.ReplaceAllStringFunc(fm, func(all string) string {
		m := fmRelRe.FindStringSubmatch(all)
		pre, token := m[1], m[2]
		target, anchor := token, ""
		if h := strings.IndexByte(token, '#'); h >= 0 {
			target, anchor = token[:h], token[h:]
		}
		resolved := resolvePath(oldDir, target)
		if !exists(resolved) {
			return all
		}
		n++
		return pre + resolved + anchor
	})
	if n == 0 {
		return doc, false
	}
	if fm != "" {
		return assemble(newFm, newBody), true
	}
	return newBody, true
}

// ReferencingDocs lists the markdown documents in a snapshot that reference
// target (the candidates for rewrite), sorted.
func ReferencingDocs(files map[string]string, target string) []string {
	out := []string{}
	for p, content := range files {
		if !strings.HasSuffix(p, ".md") || p == target {
			continue
		}
		if _, changed := RewriteRefs(content, p, target, target+".tmp"); changed {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
