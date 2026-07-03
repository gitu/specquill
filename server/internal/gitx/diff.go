package gitx

import (
	"fmt"
	"strconv"
	"strings"
)

type DiffLine struct {
	Op   string `json:"op"` // "+", "-", " "
	Text string `json:"text"`
}

type Hunk struct {
	Header string     `json:"header"`
	Lines  []DiffLine `json:"lines"`
}

type DiffFile struct {
	Path       string `json:"path"`
	OldPath    string `json:"oldPath,omitempty"`
	Status     string `json:"status"` // M | A | D | R
	Additions  int    `json:"additions"`
	Deletions  int    `json:"deletions"`
	BinaryLike bool   `json:"binaryLike"` // true: render as artifact, not text (.excalidraw etc.)
	Hunks      []Hunk `json:"hunks"`
}

// binaryLike marks files whose text diff is useless in review — the PR UI
// renders before/after previews instead.
func binaryLike(path string) bool {
	return strings.HasSuffix(path, ".excalidraw")
}

// DiffRange produces the structured three-dot (merge-base) diff target...source,
// which matches exactly what a merge would apply.
func (r *Repo) DiffRange(target, source string) ([]DiffFile, error) {
	spec := target + "..." + source
	raw, err := run(r.gitDir, nil, "diff", "--find-renames", "-U3", spec)
	if err != nil {
		return nil, err
	}
	files := parseUnifiedDiff(raw)
	if err := r.fillNumstat(files, spec); err != nil {
		return nil, err
	}
	return files, nil
}

// DiffWorktree diffs uncommitted changes in a branch worktree against HEAD,
// including untracked files (synthesized as all-additions).
func (r *Repo) DiffWorktree(branch string) ([]DiffFile, error) {
	branch = r.ResolveRef(branch)
	wt, err := r.Worktree(branch)
	if err != nil {
		return nil, err
	}
	mu := r.lockBranch(branch)
	mu.Lock()
	raw, err := run(wt, nil, "diff", "--find-renames", "-U3", "HEAD")
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	files := parseUnifiedDiff(raw)
	if numRaw, err := run(wt, nil, "diff", "--numstat", "-z", "HEAD"); err == nil {
		applyNumstat(files, numRaw)
	}
	untrackedRaw, _ := run(wt, nil, "ls-files", "--others", "--exclude-standard", "-z")
	mu.Unlock()

	for _, p := range strings.Split(untrackedRaw, "\x00") {
		if p == "" {
			continue
		}
		f := DiffFile{Path: p, Status: "A", BinaryLike: binaryLike(p)}
		if !f.BinaryLike {
			if content, _, err := r.File(branch, p); err == nil && len(content) <= maxSnapshotFileSize {
				lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
				h := Hunk{Header: fmt.Sprintf("@@ -0,0 +1,%d @@", len(lines))}
				for _, ln := range lines {
					h.Lines = append(h.Lines, DiffLine{Op: "+", Text: ln})
				}
				f.Additions = len(lines)
				f.Hunks = []Hunk{h}
			}
		}
		files = append(files, f)
	}
	return files, nil
}

func (r *Repo) fillNumstat(files []DiffFile, spec string) error {
	raw, err := run(r.gitDir, nil, "diff", "--numstat", "-z", "--find-renames", spec)
	if err != nil {
		return err
	}
	applyNumstat(files, raw)
	return nil
}

func applyNumstat(files []DiffFile, raw string) {
	byPath := map[string]*DiffFile{}
	for i := range files {
		byPath[files[i].Path] = &files[i]
	}
	// -z format: "adds\tdels\tpath\0" or for renames "adds\tdels\t\0old\0new\0"
	rec := strings.Split(raw, "\x00")
	for i := 0; i < len(rec); i++ {
		fields := strings.Split(rec[i], "\t")
		if len(fields) < 3 {
			continue
		}
		adds, _ := strconv.Atoi(fields[0])
		dels, _ := strconv.Atoi(fields[1])
		path := fields[2]
		if path == "" && i+2 < len(rec) { // rename: old, new follow
			path = rec[i+2]
			i += 2
		}
		if f, ok := byPath[path]; ok {
			f.Additions = adds
			f.Deletions = dels
		}
	}
}

// parseUnifiedDiff turns `git diff` output into structured files/hunks.
func parseUnifiedDiff(raw string) []DiffFile {
	var files []DiffFile
	var cur *DiffFile
	var hunk *Hunk
	flushHunk := func() {
		if cur != nil && hunk != nil {
			cur.Hunks = append(cur.Hunks, *hunk)
			hunk = nil
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushHunk()
			if cur != nil {
				files = append(files, *cur)
			}
			cur = &DiffFile{Status: "M"}
		case cur == nil:
			continue
		case strings.HasPrefix(line, "new file mode"):
			cur.Status = "A"
		case strings.HasPrefix(line, "deleted file mode"):
			cur.Status = "D"
		case strings.HasPrefix(line, "rename from "):
			cur.Status = "R"
			cur.OldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			cur.Path = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "Binary files "):
			cur.BinaryLike = true
		case strings.HasPrefix(line, "--- a/"):
			if cur.Path == "" {
				cur.Path = strings.TrimPrefix(line, "--- a/")
			}
		case strings.HasPrefix(line, "+++ b/"):
			cur.Path = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "@@"):
			flushHunk()
			hunk = &Hunk{Header: line}
		case hunk != nil && len(line) > 0 && (line[0] == '+' || line[0] == '-' || line[0] == ' '):
			hunk.Lines = append(hunk.Lines, DiffLine{Op: string(line[0]), Text: line[1:]})
		}
	}
	flushHunk()
	if cur != nil {
		files = append(files, *cur)
	}
	for i := range files {
		if binaryLike(files[i].Path) {
			files[i].BinaryLike = true
			files[i].Hunks = nil
		}
	}
	return files
}

// Head returns the commit sha of a branch.
func (r *Repo) Head(branch string) (string, error) {
	out, err := run(r.gitDir, nil, "rev-parse", "refs/heads/"+r.ResolveRef(branch))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
