package gitx

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// credentialArgsEnv configures git to take credentials from the child-process
// environment only — the token never appears on argv or in any config file.
// This is the single credentials seam. The manager's PAT (forge-PAT mode:
// the requesting user's own token, refreshed per request) wins; otherwise
// the repo's token_env names the env var holding the deployment token.
func (r *Repo) credentialArgsEnv() (args []string, env []string) {
	token := ""
	if r.mgr != nil {
		token = r.mgr.token()
	}
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
