package gitx

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"
)

// credentialArgs configures git to take credentials from the child-process
// environment only — the token never appears on argv or in any config file.
// This is the single credentials seam.
//
// The token is passed PER OPERATION (forge-PAT mode: the requesting user's
// own token, carried down from the request context) — never stored on the
// repo or manager, so concurrent requests with different tokens cannot
// interfere. An empty token falls back to the repo's token_env, which is how
// local/dev deployments authenticate.
func (r *Repo) credentialArgs(token string) (args []string, env []string) {
	if token == "" && r.Cfg.TokenEnv != "" {
		token = os.Getenv(r.Cfg.TokenEnv)
	}
	if token == "" {
		return nil, nil
	}
	// The helper is HOST-SCOPED: git tells it which host is asking (the
	// credential protocol's host= line), and it answers only for the repo's
	// own remote host — a redirect or rewrite to any other host gets nothing.
	// Without this, the token would be offered to whatever host challenges.
	helper := `!f(){ h=""; while IFS= read -r l; do case "$l" in host=*) h="${l#host=}";; esac; done; ` +
		`if [ -z "$SPECQUILL_GIT_HOST" ] || [ "$h" = "$SPECQUILL_GIT_HOST" ]; then ` +
		`echo "username=${SPECQUILL_GIT_USER:-x-access-token}"; echo "password=${SPECQUILL_GIT_TOKEN}"; fi; };f`
	env = []string{
		"SPECQUILL_GIT_TOKEN=" + token,
		"SPECQUILL_GIT_HOST=" + remoteHost(r.Cfg.Remote),
	}
	return []string{"-c", "credential.helper=", "-c", "credential.helper=" + helper}, env
}

// remoteHost is the host[:port] an http(s) remote's credentials are scoped
// to; "" (helper answers unconditionally) for ssh/path remotes, where the
// token is never used anyway.
func remoteHost(remote string) string {
	u, err := url.Parse(remote)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return strings.ToLower(u.Host)
}

// Fetch updates remote-tracking state (writable) or heads (read-only),
// authenticating with token (empty = the repo's token_env).
func (r *Repo) Fetch(token string) error {
	if r.Cfg.Mirror {
		return nil // no remote — the importer.Runner drives mirror updates
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	args, env := r.credentialArgs(token)
	if _, err := run(r.gitDir, env, append(args, "fetch", "--prune", "origin")...); err != nil {
		return err
	}
	r.setLastFetch(time.Now())
	return nil
}

// Pull fast-forwards branch onto origin/<branch> after a fetch. It never
// merges: dirty worktrees and diverged branches return typed errors.
func (r *Repo) Pull(branch, token string) (head string, updated bool, err error) {
	branch, err = r.resolveRef(branch)
	if err != nil {
		return "", false, err
	}
	if err := r.Fetch(token); err != nil {
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
func (r *Repo) FFBranches() (updated []string) {
	if !r.Writable() || r.Cfg.Mirror {
		return nil
	}
	out, err := run(r.gitDir, nil, "for-each-ref", "--format=%(refname:short)%00%(objectname)", "refs/heads")
	if err != nil {
		log.Printf("sync %s: list branches: %v", r.Cfg.ID, err)
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
		if err := r.ResetBranchFF(branch, remoteSha); err != nil {
			log.Printf("sync %s: ff %s: %v", r.Cfg.ID, branch, err)
			continue
		}
		updated = append(updated, branch)
	}
	return updated
}

// Push publishes a branch to origin, authenticating with token (empty = the
// repo's token_env).
func (r *Repo) Push(branch, token string) error {
	branch, err := r.resolveRef(branch)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	args, env := r.credentialArgs(token)
	_, err = run(r.gitDir, env, append(args, "push", "origin", branch)...)
	return err
}
