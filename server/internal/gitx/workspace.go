package gitx

import (
	"fmt"
)

// WorkspaceState describes a personal workspace branch after EnsureWorkspace.
type WorkspaceState struct {
	Branch  string `json:"branch"`
	Created bool   `json:"created"`
	State   string `json:"state"` // current | behind | ahead | diverged | dirty
	Base    string `json:"base"`  // default-branch head the state was computed against
}

var ErrDirtyWorktree = fmt.Errorf("worktree has uncommitted changes")
var ErrDiverged = fmt.Errorf("branch has diverged")

// EnsureWorkspace creates branch from the default branch when missing, or
// reuses it — fast-forwarding onto the default branch when that is safe
// (clean worktree, no own commits, noFF false). Never discards commits or
// dirty work.
func (r *Repo) EnsureWorkspace(branch string, noFF bool) (*WorkspaceState, error) {
	if !r.Writable() {
		return nil, fmt.Errorf("repo %s is read-only", r.Cfg.ID)
	}
	if r.Cfg.IsProtected(branch) {
		return nil, fmt.Errorf("%w: %s cannot be a workspace", ErrProtected, branch)
	}
	base, err := r.Head(r.Cfg.DefaultBranch)
	if err != nil {
		return nil, err
	}
	ws := &WorkspaceState{Branch: branch, Base: base, State: "current"}

	if !r.BranchExists(branch) {
		if err := r.CreateBranch(branch, r.Cfg.DefaultBranch); err != nil {
			return nil, err
		}
		ws.Created = true
		return ws, nil
	}

	ahead, behind := r.aheadBehindRefs("refs/heads/"+branch, "refs/heads/"+r.Cfg.DefaultBranch)
	dirty, err := r.worktreeDirty(branch)
	if err != nil {
		return nil, err
	}
	switch {
	case dirty:
		ws.State = "dirty" // uncommitted WIP is sacred — reuse untouched
		if behind > 0 && ahead > 0 {
			ws.State = "diverged"
		}
	case ahead > 0 && behind > 0:
		ws.State = "diverged"
	case ahead > 0:
		ws.State = "ahead"
	case behind > 0:
		// clean and strictly behind → safe to fast-forward onto main,
		// unless the caller forbids ref moves (live co-editing room)
		if noFF {
			ws.State = "behind"
			return ws, nil
		}
		if err := r.ResetBranchFF(branch, base); err != nil {
			ws.State = "behind"
			return ws, nil
		}
		ws.State = "current"
	}
	return ws, nil
}
