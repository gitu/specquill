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

	"specquill/server/internal/config"
)

type Manager struct {
	dataDir   string
	reposRoot string // clones live here, one dir per repo id
	committer config.GitConfig
	mu        sync.RWMutex // guards repos/order (AddRepo happens at runtime)
	repos     map[string]*Repo
	order     []string
	// Notify, when set, receives coarse change hints (kind, repoKey, branch).
	Notify func(kind, repo, branch string)

	// patToken is the forge PAT of the manager's user (per-user managers in
	// forge-PAT mode; empty on the shared manager, which falls back to the
	// repo's token_env). Refreshed from the session vault on every request.
	patMu    sync.RWMutex
	patToken string
}

// SetToken applies the caller's forge PAT to all of this manager's git
// operations (clone/fetch/push credentials).
func (m *Manager) SetToken(token string) {
	m.patMu.Lock()
	m.patToken = token
	m.patMu.Unlock()
}

func (m *Manager) token() string {
	m.patMu.RLock()
	defer m.patMu.RUnlock()
	return m.patToken
}

func (m *Manager) notify(kind, repo, branch string) {
	if m.Notify != nil {
		m.Notify(kind, repo, branch)
	}
}

type Repo struct {
	Cfg       config.RepoConfig
	key       string   // canonical repo id — store rows, event payloads
	mgr       *Manager // back-pointer: Notify hook
	gitDir    string   // bare clone
	wtRoot    string   // worktrees live here, one dir per branch
	committer config.GitConfig

	mu        sync.Mutex // repo-level ops: fetch, push, branch create, merge, worktree add/remove
	branchMu  map[string]*sync.Mutex
	branchMuL sync.Mutex

	lastFetchL sync.Mutex
	lastFetch  time.Time

	ensureMu sync.Mutex
	ensured  bool // clone verified present — EnsureCloned's fast path

	done chan struct{} // closed by Manager.RemoveRepo; stops the sync loop
}

func NewManager(cfg *config.Config) (*Manager, error) {
	m := &Manager{
		dataDir:   cfg.DataDir,
		reposRoot: filepath.Join(cfg.DataDir, "repos"),
		committer: cfg.Git,
		repos:     map[string]*Repo{},
	}
	for _, rc := range cfg.Repos {
		m.add(rc)
	}
	return m, nil
}

// NewUserManager builds a manager whose clones live under a per-user scope
// directory (forge-PAT mode: each user fetches with their own token, so no
// clone may be shared). Repos are registered but NOT cloned — EnsureCloned
// runs lazily, with the user's token, on first access.
func NewUserManager(cfg *config.Config, scope string) *Manager {
	m := &Manager{
		dataDir:   cfg.DataDir,
		reposRoot: filepath.Join(cfg.DataDir, "repos", scope),
		committer: cfg.Git,
		repos:     map[string]*Repo{},
	}
	for _, rc := range cfg.Repos {
		m.add(rc)
	}
	return m
}

// add registers a repo without cloning (see ensure/Init).
func (m *Manager) add(rc config.RepoConfig) *Repo {
	key := rc.ID
	root := filepath.Join(m.reposRoot, rc.ID)
	r := &Repo{
		Cfg:       rc,
		key:       key,
		mgr:       m,
		gitDir:    filepath.Join(root, "git"),
		wtRoot:    filepath.Join(root, "worktrees"),
		committer: m.committer,
		branchMu:  map[string]*sync.Mutex{},
		done:      make(chan struct{}),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.repos[key] = r
	m.order = append(m.order, key)
	return r
}

// AddRepo registers a repo at runtime, clones it, and starts its sync loop.
// Idempotent per id: an existing registration is returned as-is.
func (m *Manager) AddRepo(rc config.RepoConfig) (*Repo, error) {
	if r, ok := m.Repo(rc.ID); ok {
		return r, nil
	}
	r := m.add(rc)
	if err := r.EnsureCloned(); err != nil {
		m.RemoveRepo(r.key)
		return nil, err
	}
	m.startSyncLoop(r)
	return r, nil
}

// RegisterRepo registers a repo without cloning and without a sync loop — the
// forge-PAT path, where clones are lazy (EnsureCloned with the user's token)
// and fetches happen on user activity only. Idempotent per id.
func (m *Manager) RegisterRepo(rc config.RepoConfig) *Repo {
	if r, ok := m.Repo(rc.ID); ok {
		return r
	}
	return m.add(rc)
}

// RemoveRepo drops a repo from the registry and stops its sync loop. The
// clone stays on disk (it is a cache; re-adding reuses it).
func (m *Manager) RemoveRepo(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.repos[key]
	if !ok {
		return
	}
	close(r.done)
	delete(m.repos, key)
	for i, k := range m.order {
		if k == key {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
}

// Init clones any missing repos and prunes stale worktrees. Call at startup.
func (m *Manager) Init() error {
	for _, r := range m.Repos() {
		if err := r.EnsureCloned(); err != nil {
			return err
		}
	}
	return nil
}

// Repo looks up by canonical key (the repo id).
func (m *Manager) Repo(key string) (*Repo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.repos[key]
	return r, ok
}

func (m *Manager) Repos() []*Repo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Repo, 0, len(m.order))
	for _, key := range m.order {
		out = append(out, m.repos[key])
	}
	return out
}

// Key is the canonical repo identifier (the repo id) — what lands in store
// rows and event payloads.
func (r *Repo) Key() string { return r.key }

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

// EnsureCloned makes sure the bare clone exists, cloning with the manager's
// current credentials when it does not. Cheap after the first success.
func (r *Repo) EnsureCloned() error {
	r.ensureMu.Lock()
	defer r.ensureMu.Unlock()
	if r.ensured {
		return nil
	}
	if err := r.ensure(); err != nil {
		return fmt.Errorf("repo %s: %w", r.key, err)
	}
	return nil
}

func (r *Repo) ensure() error {
	if _, err := os.Stat(filepath.Join(r.gitDir, "HEAD")); err == nil {
		_, _ = run(r.gitDir, nil, "worktree", "prune")
		r.ensured = true
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.gitDir), 0o755); err != nil {
		return err
	}
	// mirror repos have no remote: init an empty bare repo whose default branch
	// the importer.Runner will populate. No clone, no fetch.
	if r.Cfg.Mirror {
		_, err := run("", nil, "init", "--bare", "-b", r.Cfg.DefaultBranch, r.gitDir)
		if err == nil {
			r.setLastFetch(time.Now())
			r.ensured = true
		}
		return err
	}
	args, env := r.credentialArgsEnv()
	if _, err := run("", env, append(args, "clone", "--bare", "--", r.Cfg.Remote, r.gitDir)...); err != nil {
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
	r.ensured = true
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
	branch, err := r.resolveRef(branch)
	if err != nil {
		return "", err
	}
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

// resolveRef defaults empty refs to the configured default branch and
// rejects anything git could misparse: option lookalikes (leading "-"),
// traversal (".."), and meta characters. Every gitx entry point taking a
// client-supplied ref funnels through here before the value reaches git
// argv or a filesystem path.
func (r *Repo) resolveRef(ref string) (string, error) {
	if ref == "" {
		return r.Cfg.DefaultBranch, nil
	}
	if !ValidRef(ref) {
		return "", fmt.Errorf("invalid ref %q", ref)
	}
	return ref, nil
}

// safeRelPath validates a client-supplied repo path: relative, no traversal.
func safeRelPath(p string) (string, error) {
	// ".." anywhere is rejected outright — no repo file legitimately needs
	// it, and it keeps the traversal check independent of Clean's rewriting
	if p == "" || strings.Contains(p, "..") {
		return "", fmt.Errorf("invalid path %q", p)
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if !filepath.IsLocal(clean) {
		return "", fmt.Errorf("invalid path %q", p)
	}
	if strings.HasPrefix(clean, ".git/") || clean == ".git" {
		return "", fmt.Errorf("invalid path %q", p)
	}
	return clean, nil
}
