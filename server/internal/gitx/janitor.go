package gitx

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// LockDataDir takes an exclusive flock on the data dir so a second server
// instance cannot corrupt worktrees. The returned release func is optional —
// the lock dies with the process.
func LockDataDir(dataDir string) (release func(), err error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, ".specquill.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("data dir %s is locked by another specquill instance", dataDir)
	}
	return func() { _ = f.Close() }, nil
}

// StartSyncLoops fetches every repo on its configured interval (writable
// repos update remote-tracking refs; read-only repos fast-forward heads) and
// evicts idle clean worktrees. Runs until the process exits.
func (m *Manager) StartSyncLoops() {
	for _, r := range m.Repos() {
		if r.Cfg.SyncInterval > 0 {
			go func(r *Repo) {
				t := time.NewTicker(r.Cfg.SyncInterval)
				for range t.C {
					if err := r.Fetch(); err != nil {
						log.Printf("sync %s: %v", r.Cfg.ID, err)
						continue
					}
					m.notify("fetch", r.Key(), "")
				}
			}(r)
		}
	}
	go m.worktreeJanitor()
}

const worktreeIdleEviction = 24 * time.Hour

// worktreeJanitor removes worktrees untouched for a day — but never dirty
// ones (uncommitted work always survives).
func (m *Manager) worktreeJanitor() {
	t := time.NewTicker(time.Hour)
	for range t.C {
		for _, r := range m.Repos() {
			if !r.Writable() {
				continue
			}
			entries, err := os.ReadDir(r.wtRoot)
			if err != nil {
				continue
			}
			for _, e := range entries {
				dir := filepath.Join(r.wtRoot, e.Name())
				info, err := e.Info()
				if err != nil || time.Since(info.ModTime()) < worktreeIdleEviction {
					continue
				}
				// dirty check via the porcelain in that dir; skip on any doubt
				out, err := run(dir, nil, "status", "--porcelain")
				if err != nil || out != "" {
					continue
				}
				// never evict the default branch's worktree
				if e.Name() == slug(r.Cfg.DefaultBranch) {
					continue
				}
				r.mu.Lock()
				if _, err := run(r.gitDir, nil, "worktree", "remove", dir); err == nil {
					log.Printf("janitor: evicted idle worktree %s/%s", r.Cfg.ID, e.Name())
				}
				r.mu.Unlock()
			}
		}
	}
}
