package gitx

import (
	"fmt"
	"strconv"
	"strings"
)

type Branch struct {
	Name      string `json:"name"`
	Head      string `json:"head"`
	IsDefault bool   `json:"isDefault"`
	Ahead     int    `json:"ahead"`  // vs remote-tracking ref, writable repos only
	Behind    int    `json:"behind"` //
}

func (r *Repo) Branches() ([]Branch, error) {
	out, err := run(r.gitDir, nil, "for-each-ref", "--format=%(refname:short)%00%(objectname)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var branches []Branch
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		b := Branch{Name: parts[0], Head: parts[1], IsDefault: parts[0] == r.Cfg.DefaultBranch}
		if r.Writable() {
			b.Ahead, b.Behind = r.aheadBehind(b.Name)
		}
		branches = append(branches, b)
	}
	return branches, nil
}

// aheadBehindRefs counts commits each side of a...b has that the other lacks
// (0,0 when either ref is missing).
func (r *Repo) aheadBehindRefs(a, b string) (ahead, behind int) {
	out, err := run(r.gitDir, nil, "rev-list", "--left-right", "--count", a+"..."+b)
	if err != nil {
		return 0, 0
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0
	}
	ahead, _ = strconv.Atoi(fields[0])
	behind, _ = strconv.Atoi(fields[1])
	return ahead, behind
}

// aheadBehind compares a local branch to its remote-tracking ref.
func (r *Repo) aheadBehind(branch string) (ahead, behind int) {
	return r.aheadBehindRefs("refs/heads/"+branch, "refs/remotes/origin/"+branch)
}

// ResetBranchFF moves branch to toSha (CAS on the current head) and hard-syncs
// its worktree. Refuses when the worktree has uncommitted changes.
func (r *Repo) ResetBranchFF(branch, toSha string) error {
	if dirty, err := r.worktreeDirty(branch); err != nil {
		return err
	} else if dirty {
		return fmt.Errorf("%w: %s", ErrDirtyWorktree, branch)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, err := r.Head(branch)
	if err != nil {
		return err
	}
	if cur == toSha {
		return nil
	}
	if _, err := run(r.gitDir, nil, "update-ref", "refs/heads/"+branch, toSha, cur); err != nil {
		return err
	}
	r.syncWorktree(branch, toSha)
	return nil
}

// CreateBranch creates a new branch pointing at from (a branch name or sha).
func (r *Repo) CreateBranch(name, from string) error {
	if !r.Writable() {
		return fmt.Errorf("repo %s is read-only", r.Cfg.ID)
	}
	if r.BranchExists(name) {
		return fmt.Errorf("branch %q already exists", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, err := run(r.gitDir, nil, "branch", name, r.ResolveRef(from))
	return err
}
