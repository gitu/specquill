// Package project is the content-root choke point (config-split plan, D2):
// a Project is a writable workspace — a gitx repo plus an optional
// content_root subfolder (monorepo case). The API serves *project-relative*
// paths; this wrapper is the ONLY place that maps them onto full repo paths
// (MapIn) and back (MapOut). Store rows and git operations always use full
// repo paths; the wire format is always project-relative.
package project

import (
	"fmt"
	"strings"

	"specquill/server/internal/gitx"
)

type Project struct {
	*gitx.Repo
	ID          string
	ContentRoot string // "" = repo root (today's degenerate case, identity mapping)
	// ReadOnly marks a granted source browsed through the project API
	// (pseudo-project): reads work, every write path refuses.
	ReadOnly bool
}

// New wraps a repo as a project rooted at contentRoot.
func New(repo *gitx.Repo, id, contentRoot string, readOnly bool) *Project {
	return &Project{Repo: repo, ID: id, ContentRoot: strings.Trim(contentRoot, "/"), ReadOnly: readOnly}
}

// Writable reports whether the project accepts writes (shadowing the repo's
// mode with the pseudo-project flag).
func (p *Project) Writable() bool { return !p.ReadOnly && p.Repo.Writable() }

// MapIn converts a project-relative path to the full repo path, refusing
// traversal and escapes from the content root.
func (p *Project) MapIn(rel string) (string, error) {
	clean, err := safeRel(rel)
	if err != nil {
		return "", err
	}
	if p.ContentRoot == "" {
		return clean, nil
	}
	return p.ContentRoot + "/" + clean, nil
}

// MapOut converts a full repo path to project-relative; ok=false when the
// path lies outside the content root.
func (p *Project) MapOut(full string) (string, bool) {
	if p.ContentRoot == "" {
		return full, true
	}
	rest, ok := strings.CutPrefix(full, p.ContentRoot+"/")
	if !ok || rest == "" {
		return "", false
	}
	return rest, true
}

// safeRel validates a client-supplied project path: relative, no traversal,
// no .git. (Mirrors gitx.safeRelPath, which still guards the repo layer.)
func safeRel(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	parts := strings.Split(rel, "/")
	out := make([]string, 0, len(parts))
	for _, seg := range parts {
		switch seg {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("invalid path %q", rel)
		}
		out = append(out, seg)
	}
	if len(out) == 0 {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	clean := strings.Join(out, "/")
	if out[0] == ".git" {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	return clean, nil
}

// ---------------------------------------------------------------- reads

func (p *Project) Tree(ref string) ([]gitx.TreeEntry, error) {
	entries, err := p.Repo.Tree(ref)
	if err != nil || p.ContentRoot == "" {
		return entries, err
	}
	out := make([]gitx.TreeEntry, 0, len(entries))
	for _, e := range entries {
		if rel, ok := p.MapOut(e.Path); ok {
			e.Path = rel
			out = append(out, e)
		}
	}
	return out, nil
}

func (p *Project) Snapshot(ref string) (map[string]string, error) {
	files, err := p.Repo.Snapshot(ref)
	if err != nil || p.ContentRoot == "" {
		return files, err
	}
	out := make(map[string]string, len(files))
	for full, content := range files {
		if rel, ok := p.MapOut(full); ok {
			out[rel] = content
		}
	}
	return out, nil
}

func (p *Project) File(ref, rel string) (string, string, error) {
	full, err := p.MapIn(rel)
	if err != nil {
		return "", "", err
	}
	return p.Repo.File(ref, full)
}

func (p *Project) FileAt(ref, rel string) (string, string, error) {
	full, err := p.MapIn(rel)
	if err != nil {
		return "", "", err
	}
	return p.Repo.FileAt(ref, full)
}

// ---------------------------------------------------------------- writes

// ArchiveZip zips the project's content at ref (paths project-relative).
func (p *Project) ArchiveZip(ref string) ([]byte, error) {
	return p.Repo.ArchiveZip(ref, p.ContentRoot)
}

func (p *Project) writeGuard() error {
	if p.ReadOnly {
		return fmt.Errorf("repo %s is read-only", p.ID)
	}
	return nil
}

func (p *Project) SaveFile(branch, rel, content, baseSha string) (string, error) {
	if err := p.writeGuard(); err != nil {
		return "", err
	}
	full, err := p.MapIn(rel)
	if err != nil {
		return "", err
	}
	return p.Repo.SaveFile(branch, full, content, baseSha)
}

func (p *Project) MoveFile(branch, from, to string) error {
	if err := p.writeGuard(); err != nil {
		return err
	}
	fullFrom, err := p.MapIn(from)
	if err != nil {
		return err
	}
	fullTo, err := p.MapIn(to)
	if err != nil {
		return err
	}
	return p.Repo.MoveFile(branch, fullFrom, fullTo)
}

func (p *Project) FileHistory(ref, rel string, limit int) ([]gitx.HistoryEntry, error) {
	full, err := p.MapIn(rel)
	if err != nil {
		return nil, err
	}
	return p.Repo.FileHistory(ref, full, limit)
}

func (p *Project) DeleteFile(branch, rel string) error {
	if err := p.writeGuard(); err != nil {
		return err
	}
	full, err := p.MapIn(rel)
	if err != nil {
		return err
	}
	return p.Repo.DeleteFile(branch, full)
}

func (p *Project) Commit(branch, message, authorName, authorEmail string, rels []string) (string, error) {
	if err := p.writeGuard(); err != nil {
		return "", err
	}
	paths := make([]string, 0, len(rels))
	for _, rel := range rels {
		full, err := p.MapIn(rel)
		if err != nil {
			return "", err
		}
		paths = append(paths, full)
	}
	// no explicit paths + a content root: commit only the project subtree,
	// never sibling content of the shared repo
	if len(paths) == 0 && p.ContentRoot != "" {
		paths = []string{p.ContentRoot}
	}
	return p.Repo.Commit(branch, message, authorName, authorEmail, paths)
}

// ---------------------------------------------------------------- status/diff

func (p *Project) Status(branch string) (*gitx.StatusResult, error) {
	st, err := p.Repo.Status(branch)
	if err != nil || p.ContentRoot == "" || st == nil {
		return st, err
	}
	dirty := st.Dirty[:0]
	for _, f := range st.Dirty {
		if rel, ok := p.MapOut(f.Path); ok {
			f.Path = rel
			dirty = append(dirty, f)
		}
	}
	st.Dirty = dirty
	return st, nil
}

func (p *Project) mapDiff(files []gitx.DiffFile) []gitx.DiffFile {
	if p.ContentRoot == "" {
		return files
	}
	out := make([]gitx.DiffFile, 0, len(files))
	for _, f := range files {
		rel, ok := p.MapOut(f.Path)
		if !ok {
			continue
		}
		f.Path = rel
		if f.OldPath != "" {
			if old, ok := p.MapOut(f.OldPath); ok {
				f.OldPath = old
			}
		}
		out = append(out, f)
	}
	return out
}

func (p *Project) DiffWorktree(branch string) ([]gitx.DiffFile, error) {
	files, err := p.Repo.DiffWorktree(branch)
	return p.mapDiff(files), err
}

func (p *Project) DiffRange(target, source string) ([]gitx.DiffFile, error) {
	files, err := p.Repo.DiffRange(target, source)
	return p.mapDiff(files), err
}
