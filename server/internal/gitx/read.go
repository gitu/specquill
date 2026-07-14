package gitx

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type TreeEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

const maxSnapshotFileSize = 512 * 1024

// worktreeFor returns the branch worktree dir when ref is a branch of a
// writable repo — reads then reflect saved-but-uncommitted state. Otherwise ""
// and reads go through the object database.
func (r *Repo) worktreeFor(ref string) (string, error) {
	if !r.Writable() || !r.BranchExists(ref) {
		return "", nil
	}
	return r.Worktree(ref)
}

// Tree lists all files reachable at ref.
func (r *Repo) Tree(ref string) ([]TreeEntry, error) {
	ref = r.ResolveRef(ref)
	if wt, err := r.worktreeFor(ref); err != nil {
		return nil, err
	} else if wt != "" {
		mu := r.lockBranch(ref)
		mu.Lock()
		defer mu.Unlock()
		return walkWorktree(wt)
	}
	out, err := run(r.gitDir, nil, "ls-tree", "-r", "-z", "--long", ref)
	if err != nil {
		return nil, err
	}
	var entries []TreeEntry
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		// <mode> <type> <oid> <size>\t<path>
		tab := strings.IndexByte(rec, '\t')
		if tab < 0 {
			continue
		}
		meta := strings.Fields(rec[:tab])
		if len(meta) < 4 || meta[1] != "blob" {
			continue
		}
		size, _ := strconv.ParseInt(meta[3], 10, 64)
		entries = append(entries, TreeEntry{Path: rec[tab+1:], Size: size})
	}
	return entries, nil
}

func walkWorktree(root string) ([]TreeEntry, error) {
	var entries []TreeEntry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if name == ".git" { // worktree .git is a file
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		entries = append(entries, TreeEntry{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	return entries, err
}

// Snapshot returns path→content for every text file at ref (buildModel input).
func (r *Repo) Snapshot(ref string) (map[string]string, error) {
	ref = r.ResolveRef(ref)
	entries, err := r.Tree(ref)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	if wt, err := r.worktreeFor(ref); err != nil {
		return nil, err
	} else if wt != "" {
		mu := r.lockBranch(ref)
		mu.Lock()
		defer mu.Unlock()
		for _, e := range entries {
			if e.Size > maxSnapshotFileSize {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(wt, filepath.FromSlash(e.Path)))
			if err != nil {
				continue
			}
			if isText(raw) {
				files[e.Path] = string(raw)
			}
		}
		return files, nil
	}
	// bare read: one cat-file --batch process for all blobs
	var input bytes.Buffer
	var order []string
	for _, e := range entries {
		if e.Size > maxSnapshotFileSize {
			continue
		}
		fmt.Fprintf(&input, "%s:%s\n", ref, e.Path)
		order = append(order, e.Path)
	}
	out, _, err := runFull(r.gitDir, nil, input.Bytes(), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	buf := []byte(out)
	for _, path := range order {
		nl := bytes.IndexByte(buf, '\n')
		if nl < 0 {
			break
		}
		header := string(buf[:nl])
		buf = buf[nl+1:]
		parts := strings.Fields(header)
		// "<oid> <type> <size>" or "<name> missing"
		if len(parts) == 3 && parts[1] == "blob" {
			size, _ := strconv.Atoi(parts[2])
			if size <= len(buf) {
				raw := buf[:size]
				if isText(raw) {
					files[path] = string(raw)
				}
				buf = buf[size:]
				if len(buf) > 0 && buf[0] == '\n' {
					buf = buf[1:]
				}
			}
		}
	}
	return files, nil
}

// File returns one file's content and blob sha at ref.
func (r *Repo) File(ref, path string) (content string, sha string, err error) {
	ref = r.ResolveRef(ref)
	path, err = safeRelPath(path)
	if err != nil {
		return "", "", err
	}
	if wt, werr := r.worktreeFor(ref); werr != nil {
		return "", "", werr
	} else if wt != "" {
		mu := r.lockBranch(ref)
		mu.Lock()
		defer mu.Unlock()
		raw, rerr := os.ReadFile(filepath.Join(wt, filepath.FromSlash(path)))
		if rerr != nil {
			return "", "", fmt.Errorf("not found: %s", path)
		}
		oid, herr := runFull2(wt, nil, raw, "hash-object", "-t", "blob", "--stdin")
		if herr != nil {
			return "", "", herr
		}
		return string(raw), strings.TrimSpace(oid), nil
	}
	oid, err := run(r.gitDir, nil, "rev-parse", ref+":"+path)
	if err != nil {
		return "", "", fmt.Errorf("not found: %s@%s", path, ref)
	}
	blob, err := run(r.gitDir, nil, "cat-file", "blob", strings.TrimSpace(oid))
	if err != nil {
		return "", "", err
	}
	return blob, strings.TrimSpace(oid), nil
}

// HistoryEntry is one commit touching a file.
type HistoryEntry struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Email   string `json:"email"`
	Date    string `json:"date"` // ISO 8601 author date
	Subject string `json:"subject"`
}

// FileHistory lists the commits touching path on ref, newest first, renames
// followed (--follow), capped at limit (default 100).
func (r *Repo) FileHistory(ref, path string, limit int) ([]HistoryEntry, error) {
	ref = r.ResolveRef(ref)
	path, err := safeRelPath(path)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out, err := run(r.gitDir, nil, "log", "--follow", fmt.Sprintf("-n%d", limit),
		"--pretty=format:%H%x1f%an%x1f%ae%x1f%aI%x1f%s", ref, "--", path)
	if err != nil {
		return nil, err
	}
	var entries []HistoryEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if f := strings.SplitN(line, "\x1f", 5); len(f) == 5 {
			entries = append(entries, HistoryEntry{SHA: f[0], Author: f[1], Email: f[2], Date: f[3], Subject: f[4]})
		}
	}
	return entries, nil
}

// FileAt reads a file from the object database at ref — never the worktree.
// Used as the committed baseline for uncommitted-change visualization.
func (r *Repo) FileAt(ref, path string) (content string, sha string, err error) {
	ref = r.ResolveRef(ref)
	path, err = safeRelPath(path)
	if err != nil {
		return "", "", err
	}
	oid, err := run(r.gitDir, nil, "rev-parse", ref+":"+path)
	if err != nil {
		return "", "", fmt.Errorf("not found: %s@%s", path, ref)
	}
	blob, err := run(r.gitDir, nil, "cat-file", "blob", strings.TrimSpace(oid))
	if err != nil {
		return "", "", err
	}
	return blob, strings.TrimSpace(oid), nil
}

// ArchiveZip returns the tree at ref as a zip archive, produced by git
// itself (binaries included, no worktree needed). A non-empty subdir scopes
// the archive to that subtree with paths relative to it — the OKF-bundle
// download for content-root (monorepo) projects.
func (r *Repo) ArchiveZip(ref, subdir string) ([]byte, error) {
	tree := ref
	if subdir != "" {
		tree = ref + ":" + subdir
	}
	out, err := run(r.gitDir, nil, "archive", "--format=zip", tree)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func runFull2(dir string, env []string, stdin []byte, args ...string) (string, error) {
	out, _, err := runFull(dir, env, stdin, args...)
	return out, err
}

// isText treats content as text when its first 8KB contain no NUL byte.
func isText(raw []byte) bool {
	probe := raw
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	return !bytes.ContainsRune(probe, 0)
}
