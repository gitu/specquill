// Package project is the content-root choke point (config-split plan, D2):
// a Project is a writable workspace — a gitx repo plus an optional
// content_root subfolder (monorepo case). The API serves *project-relative*
// paths; this wrapper is the ONLY place that maps them onto full repo paths
// (MapIn) and back (MapOut). Store rows and git operations always use full
// repo paths; the wire format is always project-relative.
package project

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"strings"

	"specquill/server/internal/gitx"
	"specquill/server/internal/okf"
	"specquill/server/internal/refactor"
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
// When the content opted into OKF, log.md is generated ON THE FLY from git
// history and injected into the archive — the change log is a bundle-export
// artifact, never a file materialized in the repo. Injection is best-effort:
// a bundle without a log is still valid, so failures fall back to the plain
// archive rather than breaking the download.
func (p *Project) ArchiveZip(ref string) ([]byte, error) {
	raw, err := p.Repo.ArchiveZip(ref, p.ContentRoot)
	if err != nil {
		return nil, err
	}
	idx, _, err := p.FileAt(ref, "index.md")
	if err != nil || !okf.EnabledContent(idx) {
		return raw, nil
	}
	entries, err := p.Repo.OKFLogEntries(ref, p.ContentRoot)
	if err != nil || len(entries) == 0 {
		return raw, nil
	}
	withLog, err := zipWithFile(raw, "log.md", okf.RenderLog(entries))
	if err != nil {
		return raw, nil
	}
	return withLog, nil
}

// zipWithFile returns the archive with name's content set — replacing an
// existing entry (bundles pre-dating on-the-fly logs carried a committed
// log.md) or appending a new one.
func zipWithFile(raw []byte, name, content string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if f.Name == name {
			continue
		}
		if err := zw.Copy(f); err != nil {
			return nil, err
		}
	}
	w, err := zw.Create(name)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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

// MoveFileRewriting moves a file and rewrites every document referencing it
// to the new location — the server-side consolidation of what used to be a
// client-driven PUT loop. Rewrites are ordinary worktree saves guarded by
// each document's current blob sha, so a concurrent edit surfaces as
// gitx.ErrStale instead of being clobbered. Paths in and out are
// project-relative (refactor operates on the project's own path space).
func (p *Project) MoveFileRewriting(branch, from, to string) (rewritten []string, err error) {
	from, err = safeRel(from)
	if err != nil {
		return nil, err
	}
	to, err = safeRel(to)
	if err != nil {
		return nil, err
	}
	if err := p.MoveFile(branch, from, to); err != nil {
		return nil, err
	}
	files, err := p.Snapshot(branch)
	if err != nil {
		return nil, err
	}
	exists := func(rel string) bool { _, ok := files[rel]; return ok }
	// the moved document's OWN relative links (embedded diagrams and images
	// in particular) must keep resolving from its new directory
	if strings.HasSuffix(to, ".md") {
		if content, sha, ferr := p.File(branch, to); ferr == nil {
			if next, changed := refactor.RebaseLinks(content, from, to, exists); changed {
				if _, serr := p.SaveFile(branch, to, next, sha); serr != nil {
					return nil, serr
				}
			}
		}
	}
	rewritten = []string{}
	for _, rel := range refactor.ReferencingDocs(files, from) {
		// fresh read: the rewrite must apply to the branch's current content,
		// and the returned sha is the staleness precondition for the save
		content, sha, err := p.File(branch, rel)
		if err != nil {
			return rewritten, err
		}
		next, changed := refactor.RewriteRefs(content, rel, from, to)
		if !changed {
			continue
		}
		if _, err := p.SaveFile(branch, rel, next, sha); err != nil {
			return rewritten, err
		}
		rewritten = append(rewritten, rel)
	}
	return rewritten, nil
}

// MoveFolderRewriting moves a whole folder (one git mv on the directory) and
// rewrites every reference to any file it contained — including references
// between the moved files themselves, which keep working at their new
// root-relative paths. Returns the number of files moved and the
// project-relative paths of the rewritten documents.
func (p *Project) MoveFolderRewriting(branch, from, to string) (moved int, rewritten []string, err error) {
	from = strings.Trim(from, "/")
	to = strings.Trim(to, "/")
	if from, err = safeRel(from); err != nil {
		return 0, nil, err
	}
	if to, err = safeRel(to); err != nil {
		return 0, nil, err
	}
	if to == from || strings.HasPrefix(to+"/", from+"/") {
		return 0, nil, fmt.Errorf("cannot move %s into itself", from)
	}
	// enumerate the old→new pairs BEFORE the move — they drive the rewrite
	before, err := p.Snapshot(branch)
	if err != nil {
		return 0, nil, err
	}
	prefix := from + "/"
	var pairs [][2]string
	for rel := range before {
		if strings.HasPrefix(rel, prefix) {
			pairs = append(pairs, [2]string{rel, to + "/" + rel[len(prefix):]})
		}
	}
	if len(pairs) == 0 {
		return 0, nil, fmt.Errorf("not found: %s/", from)
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i][0] < pairs[j][0] })
	if err := p.MoveFile(branch, from, to); err != nil {
		return 0, nil, err
	}
	files, err := p.Snapshot(branch)
	if err != nil {
		return len(pairs), nil, err
	}
	rels := make([]string, 0, len(files))
	for rel := range files {
		if strings.HasSuffix(rel, ".md") {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	exists := func(rel string) bool { _, ok := files[rel]; return ok }
	oldOf := map[string]string{}
	for _, pr := range pairs {
		oldOf[pr[1]] = pr[0]
	}
	rewritten = []string{}
	for _, rel := range rels {
		// fresh read: the rewrite must apply to the branch's current content,
		// and the returned sha is the staleness precondition for the save
		content, sha, err := p.File(branch, rel)
		if err != nil {
			return len(pairs), rewritten, err
		}
		next, changedAny := content, false
		// moved documents first rebase their OWN relative links (embedded
		// diagrams, images, doc links) against the new directory
		if old, moved := oldOf[rel]; moved {
			if out, changed := refactor.RebaseLinks(next, old, rel, exists); changed {
				next, changedAny = out, true
			}
		}
		for _, pr := range pairs {
			if out, changed := refactor.RewriteRefs(next, rel, pr[0], pr[1]); changed {
				next, changedAny = out, true
			}
		}
		if !changedAny {
			continue
		}
		if _, err := p.SaveFile(branch, rel, next, sha); err != nil {
			return len(pairs), rewritten, err
		}
		rewritten = append(rewritten, rel)
	}
	return len(pairs), rewritten, nil
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

// Discard rejects uncommitted worktree changes (project-relative paths; empty
// = everything pending). With a content root only the project subtree is
// cleared, never sibling content of the shared repo.
func (p *Project) Discard(branch string, rels []string) error {
	if err := p.writeGuard(); err != nil {
		return err
	}
	paths := make([]string, 0, len(rels))
	for _, rel := range rels {
		full, err := p.MapIn(rel)
		if err != nil {
			return err
		}
		paths = append(paths, full)
	}
	if len(paths) == 0 && p.ContentRoot != "" {
		paths = []string{p.ContentRoot}
	}
	return p.Repo.Discard(branch, paths)
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

// DiffCommit is one commit's own diff, project-relative.
func (p *Project) DiffCommit(parent, sha string) ([]gitx.DiffFile, error) {
	files, err := p.Repo.DiffCommit(parent, sha)
	return p.mapDiff(files), err
}

// Log is the workspace's commit history: repo commits restricted to the
// project's content root, with paths mapped to the project-relative wire
// form. A commit whose files all fall outside the root drops out entirely —
// in a monorepo, another team's commits are not this workspace's history.
func (p *Project) Log(ref, since string, limit int) ([]gitx.Commit, error) {
	commits, err := p.Repo.Log(ref, since, limit, p.ContentRoot)
	if err != nil {
		return nil, err
	}
	out := make([]gitx.Commit, 0, len(commits))
	for _, c := range commits {
		files := make([]gitx.CommitFile, 0, len(c.Files))
		for _, f := range c.Files {
			rel, ok := p.MapOut(f.Path)
			if !ok {
				continue
			}
			f.Path = rel
			if f.OldPath != "" {
				if old, ok := p.MapOut(f.OldPath); ok {
					f.OldPath = old
				} else {
					f.OldPath = "" // renamed in from outside the workspace
				}
			}
			files = append(files, f)
		}
		if len(files) == 0 {
			continue
		}
		c.Files = files
		out = append(out, c)
	}
	return out, nil
}
