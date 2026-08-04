package recipe

import "strings"

// Glob matching for source file filters. path.Match cannot express `**`
// (it refuses to cross separators and treats the second star as a literal),
// and the server deliberately carries no glob dependency — go.mod is yaml,
// x/*, sqlite and nothing else. So: a small matcher, with exactly the
// semantics a file filter needs.
//
//	*   any run of characters within ONE path segment
//	?   exactly one character within one segment
//	**  any number of whole segments (including none)
//
// A pattern without a `/` matches the BASENAME anywhere in the tree
// (`*.kt` means `**/*.kt`) — that is what people mean when they write it,
// and requiring the `**/` prefix only produces silently-empty filters.
// Matching is case-sensitive: git paths are.
func match(pattern, path string) bool {
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "/") {
		pattern = "**/" + pattern
	}
	// a trailing slash names a folder: `app/` is everything under app/,
	// the same shape drift scopes already use for folder prefixes
	if strings.HasSuffix(pattern, "/") {
		pattern += "**"
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

// matchSegments walks pattern and path segment by segment. `**` recurses:
// it may consume any number of path segments, so it tries each split point
// and succeeds if any does.
func matchSegments(pat, seg []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// trailing `**` swallows whatever is left, including nothing
			if len(pat) == 1 {
				return true
			}
			for i := 0; i <= len(seg); i++ {
				if matchSegments(pat[1:], seg[i:]) {
					return true
				}
			}
			return false
		}
		if len(seg) == 0 {
			return false
		}
		if !matchSegment(pat[0], seg[0]) {
			return false
		}
		pat, seg = pat[1:], seg[1:]
	}
	return len(seg) == 0
}

// matchSegment matches ONE path segment against one pattern segment, where
// `*` spans any run of characters and `?` exactly one. Iterative backtracking
// on `*` (the classic two-pointer walk) so a pathological pattern cannot blow
// the stack.
func matchSegment(pat, s string) bool {
	var pi, si, star, mark int
	star = -1
	for si < len(s) {
		switch {
		case pi < len(pat) && (pat[pi] == '?' || pat[pi] == s[si]):
			pi++
			si++
		case pi < len(pat) && pat[pi] == '*':
			star, mark = pi, si
			pi++
		case star >= 0:
			// the last `*` must swallow one more character
			pi, mark = star+1, mark+1
			si = mark
		default:
			return false
		}
	}
	for pi < len(pat) && pat[pi] == '*' {
		pi++
	}
	return pi == len(pat)
}

// FileFilter narrows which files of a reference source the run's tools may
// see at all. Enforced by BUILDING a filtered snapshot (see the api package),
// not by asking the model nicely — list_files, search and read_file then
// cannot reach an excluded file.
type FileFilter struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
	// Describe is resolved by an AI pre-pass over the post-glob path list:
	// "all files that define persisted entities". Applied AFTER include/exclude,
	// so the globs bound what the pre-pass even considers.
	Describe string `yaml:"describe"`
}

// Empty reports whether the filter would keep everything (so callers can skip
// the work entirely).
func (f FileFilter) Empty() bool {
	return len(f.Include) == 0 && len(f.Exclude) == 0 && f.Describe == ""
}

// Match applies the glob half of the filter. No include patterns means
// "everything that is not excluded"; exclude always wins.
func (f FileFilter) Match(path string) bool {
	for _, p := range f.Exclude {
		if match(p, path) {
			return false
		}
	}
	if len(f.Include) == 0 {
		return true
	}
	for _, p := range f.Include {
		if match(p, path) {
			return true
		}
	}
	return false
}

// Apply narrows a path→content snapshot to the files the globs keep. The
// returned map is always a new one — a source's snapshot is shared and must
// never be mutated.
func (f FileFilter) Apply(files map[string]string) map[string]string {
	if len(f.Include) == 0 && len(f.Exclude) == 0 {
		return files
	}
	out := make(map[string]string, len(files))
	for p, c := range files {
		if f.Match(p) {
			out[p] = c
		}
	}
	return out
}

// merge layers a stage's filter over the recipe's: a stage that names its own
// include/exclude/describe replaces that half, anything it leaves out is
// inherited.
func (f FileFilter) merge(over *FileFilter) FileFilter {
	if over == nil {
		return f
	}
	out := f
	if len(over.Include) > 0 {
		out.Include = over.Include
	}
	if len(over.Exclude) > 0 {
		out.Exclude = over.Exclude
	}
	if over.Describe != "" {
		out.Describe = over.Describe
	}
	return out
}
