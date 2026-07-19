package gitx

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// credentialEnvArgs configures git to take credentials from the child-process
// environment only — the token never appears on argv or in any config file.
// The Manager.TokenFor hook (e.g. GitHub App installation tokens, per
// tenant) takes precedence over the repo's token_env.
func (r *Repo) credentialArgsEnv() (args []string, env []string) {
	user, token := "", ""
	if r.mgr != nil && r.mgr.TokenFor != nil {
		if u, t, ok := r.mgr.TokenFor(r); ok {
			user, token = u, t
		}
	}
	if token == "" && r.Cfg.TokenEnv != "" {
		token = os.Getenv(r.Cfg.TokenEnv)
	}
	if token == "" {
		return nil, nil
	}
	helper := `!f(){ echo "username=${SPECQUILL_GIT_USER:-x-access-token}"; echo "password=${SPECQUILL_GIT_TOKEN}"; };f`
	env = []string{"SPECQUILL_GIT_TOKEN=" + token}
	if user != "" {
		env = append(env, "SPECQUILL_GIT_USER="+user)
	}
	return []string{"-c", "credential.helper=", "-c", "credential.helper=" + helper}, env
}

// Fetch updates remote-tracking state (writable) or heads (read-only).
func (r *Repo) Fetch() error {
	if r.Cfg.Mirror {
		return nil // no remote — the importer.Runner drives mirror updates
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	args, env := r.credentialArgsEnv()
	if _, err := run(r.gitDir, env, append(args, "fetch", "--prune", "origin")...); err != nil {
		return err
	}
	r.setLastFetch(time.Now())
	return nil
}

// Pull fast-forwards branch onto origin/<branch> after a fetch. It never
// merges: dirty worktrees and diverged branches return typed errors.
func (r *Repo) Pull(branch string) (head string, updated bool, err error) {
	branch, err = r.resolveRef(branch)
	if err != nil {
		return "", false, err
	}
	if err := r.Fetch(); err != nil {
		return "", false, err
	}
	cur, err := r.Head(branch)
	if err != nil {
		return "", false, err
	}
	remote, err := run(r.gitDir, nil, "rev-parse", "refs/remotes/origin/"+branch)
	if err != nil {
		return cur, false, nil // branch never pushed — nothing to pull
	}
	remoteSha := strings.TrimSpace(remote)
	if remoteSha == cur {
		return cur, false, nil
	}
	// only fast-forward: local must be an ancestor of remote
	if _, err := run(r.gitDir, nil, "merge-base", "--is-ancestor", cur, remoteSha); err != nil {
		return cur, false, fmt.Errorf("%w: %s has local commits not on origin", ErrDiverged, branch)
	}
	if err := r.ResetBranchFF(branch, remoteSha); err != nil {
		return cur, false, err
	}
	return remoteSha, true, nil
}

// Push publishes a branch to origin.
func (r *Repo) Push(branch string) error {
	branch, err := r.resolveRef(branch)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	args, env := r.credentialArgsEnv()
	_, err = run(r.gitDir, env, append(args, "push", "origin", branch)...)
	return err
}
