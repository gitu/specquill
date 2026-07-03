// Package gitx executes the real git binary against per-repo bare clones and
// per-branch worktrees under the server data dir. It is the only package that
// touches git.
package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reqbase/server/internal/config"
)

type Manager struct {
	dataDir   string
	committer config.GitConfig
	repos     map[string]*Repo
	order     []string
	// Notify, when set, receives coarse change hints (kind, repo, branch).
	Notify func(kind, repo, branch string)
}

func (m *Manager) notify(kind, repo, branch string) {
	if m.Notify != nil {
		m.Notify(kind, repo, branch)
	}
}

type Repo struct {
	Cfg       config.RepoConfig
	gitDir    string // bare clone
	wtRoot    string // worktrees live here, one dir per branch
	committer config.GitConfig

	mu        sync.Mutex // repo-level ops: fetch, push, branch create, merge, worktree add/remove
	branchMu  map[string]*sync.Mutex
	branchMuL sync.Mutex

	lastFetchL sync.Mutex
	lastFetch  time.Time
}

func NewManager(cfg *config.Config) (*Manager, error) {
	m := &Manager{
		dataDir:   cfg.DataDir,
		committer: cfg.Git,
		repos:     map[string]*Repo{},
	}
	for _, rc := range cfg.Repos {
		r := &Repo{
			Cfg:       rc,
			gitDir:    filepath.Join(cfg.DataDir, "repos", rc.ID, "git"),
			wtRoot:    filepath.Join(cfg.DataDir, "repos", rc.ID, "worktrees"),
			committer: cfg.Git,
			branchMu:  map[string]*sync.Mutex{},
		}
		m.repos[rc.ID] = r
		m.order = append(m.order, rc.ID)
	}
	return m, nil
}

// Init clones any missing repos and prunes stale worktrees. Call at startup.
func (m *Manager) Init() error {
	for _, id := range m.order {
		if err := m.repos[id].ensure(); err != nil {
			return fmt.Errorf("repo %s: %w", id, err)
		}
	}
	return nil
}

func (m *Manager) Repo(id string) (*Repo, bool) {
	r, ok := m.repos[id]
	return r, ok
}

func (m *Manager) Repos() []*Repo {
	out := make([]*Repo, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.repos[id])
	}
	return out
}

func (r *Repo) Writable() bool { return r.Cfg.Mode == config.Writable }

func (r *Repo) LastFetch() time.Time {
	r.lastFetchL.Lock()
	defer r.lastFetchL.Unlock()
	return r.lastFetch
}

func (r *Repo) setLastFetch(t time.Time) {
	r.lastFetchL.Lock()
	r.lastFetch = t
	r.lastFetchL.Unlock()
}

func (r *Repo) lockBranch(branch string) *sync.Mutex {
	r.branchMuL.Lock()
	defer r.branchMuL.Unlock()
	mu, ok := r.branchMu[branch]
	if !ok {
		mu = &sync.Mutex{}
		r.branchMu[branch] = mu
	}
	return mu
}

func (r *Repo) ensure() error {
	if _, err := os.Stat(filepath.Join(r.gitDir, "HEAD")); err == nil {
		_, _ = run(r.gitDir, nil, "worktree", "prune")
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.gitDir), 0o755); err != nil {
		return err
	}
	if _, err := run("", nil, "clone", "--bare", r.Cfg.Remote, r.gitDir); err != nil {
		return err
	}
	// Writable repos keep local heads authoritative; remote state is tracked
	// under refs/remotes/origin for ahead/behind. Read-only repos fast-forward
	// their heads directly on fetch.
	refspec := "+refs/heads/*:refs/remotes/origin/*"
	if !r.Writable() {
		refspec = "+refs/heads/*:refs/heads/*"
	}
	if _, err := run(r.gitDir, nil, "config", "remote.origin.fetch", refspec); err != nil {
		return err
	}
	// populate refs/remotes/origin/* so ahead/behind works from the start
	if r.Writable() {
		if err := r.Fetch(); err != nil {
			return err
		}
	}
	r.setLastFetch(time.Now())
	return nil
}

// slug maps a branch name onto a filesystem-safe worktree directory name.
func slug(branch string) string {
	s := strings.NewReplacer("/", "__", ":", "_", " ", "_").Replace(branch)
	return s
}

// Worktree returns the checkout directory for branch, creating it lazily.
// Only valid on writable repos.
func (r *Repo) Worktree(branch string) (string, error) {
	if !r.Writable() {
		return "", fmt.Errorf("repo %s is read-only", r.Cfg.ID)
	}
	if !r.BranchExists(branch) {
		return "", fmt.Errorf("branch %q not found", branch)
	}
	dir := filepath.Join(r.wtRoot, slug(branch))
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return dir, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil { // raced
		return dir, nil
	}
	if err := os.MkdirAll(r.wtRoot, 0o755); err != nil {
		return "", err
	}
	if _, err := run(r.gitDir, nil, "worktree", "add", dir, branch); err != nil {
		return "", err
	}
	return dir, nil
}

func (r *Repo) BranchExists(branch string) bool {
	_, err := run(r.gitDir, nil, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// ResolveRef defaults empty refs to the configured default branch.
func (r *Repo) ResolveRef(ref string) string {
	if ref == "" {
		return r.Cfg.DefaultBranch
	}
	return ref
}

// safeRelPath validates a client-supplied repo path: relative, no traversal.
func safeRelPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("invalid path %q", p)
	}
	if strings.HasPrefix(clean, ".git/") || clean == ".git" {
		return "", fmt.Errorf("invalid path %q", p)
	}
	return clean, nil
}
