package gitx

// Clone reclamation (REQ-025.6/.9): per-user clones are caches of the forge —
// safe to remove when idle, dangerous only while they hold state that exists
// nowhere else. This file inspects and removes clones straight on the
// filesystem, so the janitor covers users whose in-memory managers are long
// gone (clones survive restarts, managers do not).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CloneStat describes one on-disk clone in a user scope.
type CloneStat struct {
	Scope  string `json:"-"`
	RepoID string `json:"repoId"`
	Dir    string `json:"-"`
	Bytes  int64  `json:"bytes"`
	// Unsynced marks state that exists nowhere else: a dirty worktree or a
	// local branch with commits the origin does not have. Doubt (a git error
	// during inspection) counts as unsynced — reclamation must never guess.
	Unsynced bool `json:"unsynced"`
	ModTime  time.Time `json:"-"`
}

// ScanScope walks one per-user clones root (…/repos/u<id>) and returns every
// clone found there.
func ScanScope(scopeDir string) []CloneStat {
	entries, err := os.ReadDir(scopeDir)
	if err != nil {
		return nil
	}
	out := []CloneStat{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(scopeDir, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "git", "HEAD")); err != nil {
			continue
		}
		info, _ := e.Info()
		st := CloneStat{
			Scope: filepath.Base(scopeDir), RepoID: e.Name(), Dir: dir,
			Bytes: DirSize(dir), Unsynced: cloneUnsynced(dir),
		}
		if info != nil {
			st.ModTime = info.ModTime()
		}
		out = append(out, st)
	}
	return out
}

// DirSize sums the bytes under path (best-effort; unreadable entries count 0).
func DirSize(path string) int64 {
	var total int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// cloneUnsynced reports whether a clone holds state the forge does not:
// dirty worktrees, or (on writable clones) local branches ahead of — or
// unknown to — origin. Read-only clones mirror origin heads directly and are
// never unsynced. Any inspection doubt returns true: reclamation may only
// proceed on a provably clean clone.
func cloneUnsynced(cloneDir string) bool {
	wtRoot := filepath.Join(cloneDir, "worktrees")
	if entries, err := os.ReadDir(wtRoot); err == nil {
		for _, e := range entries {
			out, err := run(filepath.Join(wtRoot, e.Name()), nil, "status", "--porcelain")
			if err != nil || strings.TrimSpace(out) != "" {
				return true
			}
		}
	}
	gitDir := filepath.Join(cloneDir, "git")
	refspec, err := run(gitDir, nil, "config", "remote.origin.fetch")
	if err != nil {
		// no origin (importer mirrors): nothing upstream holds this content —
		// never reclaim automatically
		return true
	}
	if strings.Contains(refspec, ":refs/heads/") {
		return false // read-only clone: heads mirror origin, nothing local
	}
	heads, err := run(gitDir, nil, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return true
	}
	for _, branch := range strings.Fields(heads) {
		if _, err := run(gitDir, nil, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch); err != nil {
			return true // origin never saw this branch
		}
		ahead, err := run(gitDir, nil, "rev-list", "--count", "refs/remotes/origin/"+branch+".."+branch)
		if err != nil || strings.TrimSpace(ahead) != "0" {
			return true
		}
	}
	return false
}

// reclaimMu serializes removals — RemoveAll racing a lazy re-clone of the
// same directory would corrupt both.
var reclaimMu sync.Mutex

// ReclaimClone removes one clone directory. force overrides the unsynced
// guard (the user confirmed the discard, REQ-025.5); without it an unsynced
// clone is refused.
func ReclaimClone(cloneDir string, force bool) error {
	reclaimMu.Lock()
	defer reclaimMu.Unlock()
	if _, err := os.Stat(filepath.Join(cloneDir, "git", "HEAD")); err != nil {
		return fmt.Errorf("no clone at %s", cloneDir)
	}
	if !force && cloneUnsynced(cloneDir) {
		return fmt.Errorf("clone holds unsynced work: %w", ErrUnsynced)
	}
	return os.RemoveAll(cloneDir)
}

// ErrUnsynced marks a reclamation refused because the clone holds state that
// exists nowhere else.
var ErrUnsynced = fmt.Errorf("unsynced")
