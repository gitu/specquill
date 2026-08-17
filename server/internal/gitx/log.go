package gitx

// Repository-wide history: the raw material of the change feed. FileHistory
// (read.go) answers "what happened to THIS document" with --follow; this one
// answers "what changed in the workspace" and therefore cannot use --follow
// (it is single-path only) — renames come back as R entries instead.

import (
	"fmt"
	"regexp"
	"strings"
)

// CommitFile is one path a commit touched. Status is git's name-status letter
// (A added, M modified, D deleted, R renamed); OldPath is set for renames.
type CommitFile struct {
	Status  string `json:"status"`
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
}

// Commit is one commit with the paths it touched. Parent is the FIRST parent —
// the diff baseline the change feed reads against. It travels in the payload
// because ValidRef rejects `sha^` syntax, so callers cannot derive it.
type Commit struct {
	SHA     string       `json:"sha"`
	Parent  string       `json:"parent,omitempty"`
	Author  string       `json:"author"`
	Email   string       `json:"email"`
	Date    string       `json:"date"` // ISO 8601 author date
	Subject string       `json:"subject"`
	Files   []CommitFile `json:"files"`
}

// EmptyTree is git's well-known empty-tree object — the baseline a root
// commit (no parent) diffs against.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// sinceRe: a plain calendar date. `--since` reaches git's argv, and its date
// parser accepts free-form strings ("yesterday", "@{...}"), so the API keeps
// the shape narrow rather than trusting the parser.
var sinceRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ValidSince reports whether a client-supplied --since value is acceptable.
func ValidSince(s string) bool { return s == "" || sinceRe.MatchString(s) }

// Log lists commits on ref, newest first, with the paths each touched.
// `since` (YYYY-MM-DD, optional) bounds the window, `limit` the count
// (default 50, max 200), `dir` an optional path prefix — the project's
// content root in a monorepo.
func (r *Repo) Log(ref, since string, limit int, dir string) ([]Commit, error) {
	ref, err := r.resolveRef(ref)
	if err != nil {
		return nil, err
	}
	if !ValidSince(since) {
		return nil, fmt.Errorf("invalid since %q", since)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	// -z terminates every field AND every name-status record with NUL, so a
	// path containing a newline cannot fake a record boundary. Each commit
	// starts with a \x01 sentinel of our own.
	args := []string{"log", "--name-status", "-z", "--find-renames", "--no-merges",
		fmt.Sprintf("-n%d", limit), "--pretty=format:\x01%H\x1f%P\x1f%an\x1f%ae\x1f%aI\x1f%s"}
	if since != "" {
		args = append(args, "--since="+since)
	}
	args = append(args, ref)
	if dir != "" {
		clean, err := safeRelPath(dir)
		if err != nil {
			return nil, err
		}
		args = append(args, "--", clean)
	}
	out, err := run(r.gitDir, nil, args...)
	if err != nil {
		return nil, err
	}
	return parseLog(out), nil
}

// parseLog reads `git log --name-status -z` output. Records are NUL-separated;
// a record beginning with \x01 opens a new commit, the header fields are
// \x1f-separated, and the status/path records that follow belong to it.
func parseLog(out string) []Commit {
	var commits []Commit
	var cur *Commit
	rec := strings.Split(out, "\x00")
	for i := 0; i < len(rec); i++ {
		f := rec[i]
		// git puts the commit header on the same record as the first status
		// letter, separated by a newline
		if idx := strings.Index(f, "\x01"); idx >= 0 {
			head := f[idx+1:]
			rest := ""
			if nl := strings.IndexByte(head, '\n'); nl >= 0 {
				head, rest = head[:nl], head[nl+1:]
			}
			parts := strings.SplitN(head, "\x1f", 6)
			if len(parts) < 6 {
				continue
			}
			if cur != nil {
				commits = append(commits, *cur)
			}
			parent := ""
			if p := strings.Fields(parts[1]); len(p) > 0 {
				parent = p[0] // first parent — the diff baseline
			}
			cur = &Commit{SHA: parts[0], Parent: parent, Author: parts[2], Email: parts[3], Date: parts[4], Subject: parts[5]}
			f = rest
		}
		if cur == nil || f == "" {
			continue
		}
		// a status record is the letter alone; the path(s) follow as their
		// own records (two of them for renames: old, new)
		status := strings.TrimSpace(f)
		if status == "" {
			continue
		}
		letter := status[:1]
		switch letter {
		case "R", "C":
			if i+2 < len(rec) {
				cur.Files = append(cur.Files, CommitFile{Status: "R", OldPath: rec[i+1], Path: rec[i+2]})
				i += 2
			}
		case "A", "M", "D", "T":
			if i+1 < len(rec) {
				cur.Files = append(cur.Files, CommitFile{Status: letter, Path: rec[i+1]})
				i++
			}
		}
	}
	if cur != nil {
		commits = append(commits, *cur)
	}
	return commits
}

// DiffCommit is the two-dot diff a single commit introduced: parent..sha.
// (DiffRange is deliberately three-dot — that is what a MERGE would apply,
// which is the wrong question for one commit.) An empty parent means a root
// commit and diffs against the empty tree.
func (r *Repo) DiffCommit(parent, sha string) ([]DiffFile, error) {
	sha, err := r.resolveRef(sha)
	if err != nil {
		return nil, err
	}
	if parent == "" {
		parent = EmptyTree
	} else if parent, err = r.resolveRef(parent); err != nil {
		return nil, err
	}
	spec := parent + ".." + sha
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
