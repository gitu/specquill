package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MergeCheck struct {
	Mergeable bool     `json:"mergeable"`
	Conflicts []string `json:"conflicts"`
}

// CheckMerge dry-runs the merge via merge-tree without touching any worktree.
func (r *Repo) CheckMerge(target, source string) (*MergeCheck, error) {
	_, conflicts, err := r.mergeTree(target, source)
	if err != nil {
		return nil, err
	}
	return &MergeCheck{Mergeable: len(conflicts) == 0, Conflicts: conflicts}, nil
}

// mergeTree runs `git merge-tree --write-tree` and returns the merged tree
// oid, or the conflicted paths when the merge cannot be done cleanly.
func (r *Repo) mergeTree(target, source string) (tree string, conflicts []string, err error) {
	out, _, runErr := runFull(r.gitDir, nil, nil,
		"merge-tree", "--write-tree", "--no-messages", "--name-only",
		"refs/heads/"+target, "refs/heads/"+source)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if runErr != nil {
		ge, ok := runErr.(*GitError)
		if !ok || ge.ExitCode != 1 {
			return "", nil, runErr
		}
		// exit 1 = conflicts; line 1 is the (conflicted) tree, rest are paths
		if len(lines) > 1 {
			conflicts = lines[1:]
		}
		if len(conflicts) == 0 {
			conflicts = []string{"(unknown)"}
		}
		return "", conflicts, nil
	}
	return strings.TrimSpace(lines[0]), nil, nil
}

// Merge merges source into target (no worktree involved) and returns the new
// commit sha. strategy: "merge" (two-parent, --no-ff semantics) or "squash".
// The merging user is author and committer; the service identity lands as a
// Co-authored-by trailer.
func (r *Repo) Merge(target, source, message, authorName, authorEmail, strategy string) (string, *MergeCheck, error) {
	target, err := r.resolveRef(target)
	if err != nil {
		return "", nil, err
	}
	source, err = r.resolveRef(source)
	if err != nil {
		return "", nil, err
	}
	if !r.BranchExists(target) || !r.BranchExists(source) {
		return "", nil, fmt.Errorf("branch not found")
	}
	// refuse when the target worktree has uncommitted changes: the post-merge
	// reset would destroy them
	if dirty, err := r.worktreeDirty(target); err != nil {
		return "", nil, err
	} else if dirty {
		return "", nil, fmt.Errorf("target branch %s has uncommitted changes", target)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	oldTarget, err := r.Head(target)
	if err != nil {
		return "", nil, err
	}
	sourceSha, err := r.Head(source)
	if err != nil {
		return "", nil, err
	}
	tree, conflicts, err := r.mergeTree(target, source)
	if err != nil {
		return "", nil, err
	}
	if len(conflicts) > 0 {
		return "", &MergeCheck{Mergeable: false, Conflicts: conflicts}, nil
	}

	env := []string{
		"GIT_AUTHOR_NAME=" + authorName, "GIT_AUTHOR_EMAIL=" + authorEmail,
		"GIT_COMMITTER_NAME=" + authorName, "GIT_COMMITTER_EMAIL=" + authorEmail,
	}
	args := []string{"commit-tree", tree, "-p", oldTarget}
	if strategy != "squash" {
		args = append(args, "-p", sourceSha)
	}
	args = append(args, "-m", r.withServiceTrailer(message))
	out, err := run(r.gitDir, env, args...)
	if err != nil {
		return "", nil, err
	}
	newSha := strings.TrimSpace(out)

	// compare-and-swap the target ref, then sync its worktree if one exists
	if _, err := run(r.gitDir, nil, "update-ref", "refs/heads/"+target, newSha, oldTarget); err != nil {
		return "", nil, err
	}
	r.syncWorktree(target, newSha)
	return newSha, &MergeCheck{Mergeable: true}, nil
}

func (r *Repo) worktreeDirty(branch string) (bool, error) {
	dir := filepath.Join(r.wtRoot, slug(branch))
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false, nil // no worktree yet → nothing to lose
	}
	st, err := r.Status(branch)
	if err != nil {
		return false, err
	}
	return len(st.Dirty) > 0, nil
}

// syncWorktree hard-resets a clean worktree onto the branch's new head after
// the ref moved underneath it (merge commits happen in the bare repo).
func (r *Repo) syncWorktree(branch, sha string) {
	dir := filepath.Join(r.wtRoot, slug(branch))
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return
	}
	mu := r.lockBranch(branch)
	mu.Lock()
	defer mu.Unlock()
	_, _ = run(dir, nil, "reset", "--hard", sha)
}
