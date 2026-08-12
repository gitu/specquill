package gitx

import (
	"fmt"
	"log"
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

// FFBranches fast-forwards every clean local branch strictly behind its
// remote-tracking ref (call after a Fetch). Diverged and dirty branches are
// skipped with a log line — the UI surfaces them, they are not errors here.
// hold, when non-nil, vetoes branches whose refs must not move (live
// co-editing rooms).
func (r *Repo) FFBranches(hold func(branch string) bool) (updated []string) {
	if !r.Writable() || r.Cfg.Mirror {
		return nil
	}
	out, err := run(r.gitDir, nil, "for-each-ref", "--format=%(refname:short)%00%(objectname)", "refs/heads")
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		branch, cur := parts[0], parts[1]
		remote, err := run(r.gitDir, nil, "rev-parse", "refs/remotes/origin/"+branch)
		if err != nil {
			continue // never pushed — nothing to follow
		}
		remoteSha := strings.TrimSpace(remote)
		if remoteSha == cur {
			continue
		}
		if _, err := run(r.gitDir, nil, "merge-base", "--is-ancestor", cur, remoteSha); err != nil {
			log.Printf("sync %s: %s diverged from origin, left alone", r.Cfg.ID, branch)
			continue
		}
		if hold != nil && hold(branch) {
			continue // a live room owns this branch right now
		}
		if err := r.ResetBranchFF(branch, remoteSha); err != nil {
			log.Printf("sync %s: ff %s: %v", r.Cfg.ID, branch, err)
			continue
		}
		updated = append(updated, branch)
	}
	return updated
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
