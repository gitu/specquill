package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SnapshotMirror replaces the entire tree of a mirror repo's default branch
// with files and commits it under the service identity. It writes straight to
// the bare repo via a throwaway index (no worktree) and is idempotent:
// byte-identical content yields the same tree and NO new commit (changed=false),
// so an unchanged upstream never churns history. Only valid on mirror repos.
func (r *Repo) SnapshotMirror(message string, files map[string]string) (sha string, changed bool, err error) {
	if !r.Cfg.Mirror {
		return "", false, fmt.Errorf("repo %s is not a mirror", r.Cfg.ID)
	}
	if len(files) == 0 {
		return "", false, fmt.Errorf("refusing to snapshot an empty import")
	}
	branch := r.Cfg.DefaultBranch
	mu := r.lockBranch(branch)
	mu.Lock()
	defer mu.Unlock()

	// build a fresh index in a temp file so we never touch a worktree
	idx := filepath.Join(r.gitDir, "specquill-import.index")
	defer os.Remove(idx)
	env := []string{"GIT_INDEX_FILE=" + idx}
	if _, err := run(r.gitDir, env, "read-tree", "--empty"); err != nil {
		return "", false, err
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		clean, err := safeRelPath(p)
		if err != nil {
			return "", false, err
		}
		blob, _, err := runFull(r.gitDir, nil, []byte(files[p]), "hash-object", "-w", "--stdin")
		if err != nil {
			return "", false, err
		}
		blob = strings.TrimSpace(blob)
		if _, err := run(r.gitDir, env, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+clean); err != nil {
			return "", false, err
		}
	}
	tree, err := run(r.gitDir, env, "write-tree")
	if err != nil {
		return "", false, err
	}
	tree = strings.TrimSpace(tree)

	parent := ""
	if cur, err := run(r.gitDir, nil, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		parent = strings.TrimSpace(cur)
		if curTree, err := run(r.gitDir, nil, "rev-parse", parent+"^{tree}"); err == nil && strings.TrimSpace(curTree) == tree {
			return parent, false, nil // upstream unchanged — no new commit
		}
	}

	// the mirror is machine-generated: the service identity is author AND
	// committer (no human author, unlike workspace commits)
	cenv := []string{
		"GIT_AUTHOR_NAME=" + r.committer.CommitterName, "GIT_AUTHOR_EMAIL=" + r.committer.CommitterEmail,
		"GIT_COMMITTER_NAME=" + r.committer.CommitterName, "GIT_COMMITTER_EMAIL=" + r.committer.CommitterEmail,
	}
	args := []string{"commit-tree", tree, "-m", message}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	out, _, err := runFull(r.gitDir, cenv, nil, args...)
	if err != nil {
		return "", false, err
	}
	commit := strings.TrimSpace(out)
	if _, err := run(r.gitDir, nil, "update-ref", "refs/heads/"+branch, commit); err != nil {
		return "", false, err
	}
	r.setLastFetch(time.Now())
	return commit, true, nil
}
