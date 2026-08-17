package gitx

import (
	"fmt"
	"sync"

	"specquill/server/internal/config"
)

// Fleet hands out per-user Managers in forge-PAT mode: every user works on
// their own clones (fetched with their own token), so nothing one user's
// token can reach ever leaks into another user's view. Managers are created
// lazily and live for the process lifetime; their clones are lazy too.
type Fleet struct {
	cfg *config.Config
	mu  sync.Mutex
	m   map[string]*Manager
	// Notify is copied onto every manager the fleet creates.
	Notify func(kind, repo, branch string)
}

func NewFleet(cfg *config.Config) *Fleet {
	return &Fleet{cfg: cfg, m: map[string]*Manager{}}
}

// ForUser returns (creating if needed) the manager scoped to one user id.
func (f *Fleet) ForUser(userID int64) *Manager {
	scope := fmt.Sprintf("u%d", userID)
	f.mu.Lock()
	defer f.mu.Unlock()
	mgr, ok := f.m[scope]
	if !ok {
		mgr = NewUserManager(f.cfg, scope)
		mgr.Notify = f.Notify
		f.m[scope] = mgr
	}
	return mgr
}

// Invalidate tells a live manager (if any) that a clone was removed on disk,
// so its next access re-clones instead of trusting the cached ensure state.
func (f *Fleet) Invalidate(scope, repoID string) {
	f.mu.Lock()
	mgr := f.m[scope]
	f.mu.Unlock()
	if mgr == nil {
		return
	}
	if r, ok := mgr.Repo(repoID); ok {
		r.Invalidate()
	}
}
