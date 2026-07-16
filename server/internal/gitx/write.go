package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FileStatus struct {
	Path  string `json:"path"`
	State string `json:"state"` // M | A | D | R
}

type StatusResult struct {
	Branch string       `json:"branch"`
	Dirty  []FileStatus `json:"dirty"`
	Ahead  int          `json:"ahead"`
	Behind int          `json:"behind"`
	// BehindDefault counts commits the local default branch has that this
	// branch lacks (0 for the default branch itself).
	BehindDefault int `json:"behindDefault"`
}

// Status reports uncommitted worktree changes plus ahead/behind vs origin.
func (r *Repo) Status(branch string) (*StatusResult, error) {
	branch, err := r.resolveRef(branch)
	if err != nil {
		return nil, err
	}
	wt, err := r.Worktree(branch)
	if err != nil {
		return nil, err
	}
	mu := r.lockBranch(branch)
	mu.Lock()
	defer mu.Unlock()
	out, err := run(wt, nil, "status", "--porcelain=v2", "-z")
	if err != nil {
		return nil, err
	}
	res := &StatusResult{Branch: branch, Dirty: []FileStatus{}}
	res.Ahead, res.Behind = r.aheadBehind(branch)
	if branch != r.Cfg.DefaultBranch {
		_, res.BehindDefault = r.aheadBehindRefs("refs/heads/"+branch, "refs/heads/"+r.Cfg.DefaultBranch)
	}
	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '1', '2': // ordinary / rename: "1 XY sub mH mI mW hH hI path"
			fields := strings.SplitN(rec, " ", 9)
			if len(fields) < 9 {
				continue
			}
			xy := fields[1]
			state := "M"
			if strings.Contains(xy, "A") {
				state = "A"
			} else if strings.Contains(xy, "D") {
				state = "D"
			} else if rec[0] == '2' {
				state = "R"
			}
			res.Dirty = append(res.Dirty, FileStatus{Path: fields[8], State: state})
		case '?': // untracked: "? path"
			res.Dirty = append(res.Dirty, FileStatus{Path: rec[2:], State: "A"})
		}
	}
	return res, nil
}

// SaveFile writes content into the branch worktree. baseSha guards against
// lost updates: it must match the blob sha of the current on-disk content
// (empty baseSha = create; conflicts return ErrStale).
var ErrStale = fmt.Errorf("file changed since it was loaded")

// ErrProtected marks writes/commits against protected branches — those only
// move through PR merges.
var ErrProtected = fmt.Errorf("branch is protected")

func (r *Repo) protectedErr(branch string) error {
	if r.Cfg.IsProtected(branch) {
		return fmt.Errorf("%w: %s", ErrProtected, branch)
	}
	return nil
}

func (r *Repo) SaveFile(branch, path, content, baseSha string) (sha string, err error) {
	branch, err = r.resolveRef(branch)
	if err != nil {
		return "", err
	}
	if err := r.protectedErr(branch); err != nil {
		return "", err
	}
	path, err = safeRelPath(path)
	if err != nil {
		return "", err
	}
	wt, err := r.Worktree(branch)
	if err != nil {
		return "", err
	}
	mu := r.lockBranch(branch)
	mu.Lock()
	defer mu.Unlock()

	abs := filepath.Join(wt, filepath.FromSlash(path))
	if existing, rerr := os.ReadFile(abs); rerr == nil {
		curSha, herr := runFull2(wt, nil, existing, "hash-object", "-t", "blob", "--stdin")
		if herr != nil {
			return "", herr
		}
		if strings.TrimSpace(curSha) != baseSha {
			return "", ErrStale
		}
	} else if baseSha != "" {
		return "", fmt.Errorf("not found: %s (baseSha given for a file that does not exist)", path)
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".specquill-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), abs); err != nil {
		return "", err
	}
	newSha, err := runFull2(wt, nil, []byte(content), "hash-object", "-t", "blob", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(newSha), nil
}

// SaveFileForce writes room-owned content without an optimistic-concurrency
// check — collaboration rooms are the single writer for their file while
// active. Protection still applies.
func (r *Repo) SaveFileForce(branch, path, content string) (sha string, err error) {
	branch, err = r.resolveRef(branch)
	if err != nil {
		return "", err
	}
	if err := r.protectedErr(branch); err != nil {
		return "", err
	}
	path, err = safeRelPath(path)
	if err != nil {
		return "", err
	}
	wt, err := r.Worktree(branch)
	if err != nil {
		return "", err
	}
	mu := r.lockBranch(branch)
	mu.Lock()
	defer mu.Unlock()
	abs := filepath.Join(wt, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".specquill-*")
	if err != nil {
		return "", err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), abs); err != nil {
		return "", err
	}
	newSha, err := runFull2(wt, nil, []byte(content), "hash-object", "-t", "blob", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(newSha), nil
}

// MoveFile renames a file inside the branch worktree. Tracked files move via
// `git mv` so the rename is staged explicitly; untracked drafts (not yet
// known to git) fall back to a plain filesystem rename.
func (r *Repo) MoveFile(branch, from, to string) error {
	branch, err := r.resolveRef(branch)
	if err != nil {
		return err
	}
	if err := r.protectedErr(branch); err != nil {
		return err
	}
	from, err = safeRelPath(from)
	if err != nil {
		return err
	}
	to, err = safeRelPath(to)
	if err != nil {
		return err
	}
	wt, err := r.Worktree(branch)
	if err != nil {
		return err
	}
	mu := r.lockBranch(branch)
	mu.Lock()
	defer mu.Unlock()
	absFrom := filepath.Join(wt, filepath.FromSlash(from))
	absTo := filepath.Join(wt, filepath.FromSlash(to))
	if _, err := os.Stat(absFrom); err != nil {
		return fmt.Errorf("not found: %s", from)
	}
	if _, err := os.Stat(absTo); err == nil {
		return fmt.Errorf("destination exists: %s", to)
	}
	if err := os.MkdirAll(filepath.Dir(absTo), 0o755); err != nil {
		return err
	}
	if _, mvErr := run(wt, nil, "mv", "--", from, to); mvErr != nil {
		// untracked file — git mv refuses; a plain rename is the same move
		if err := os.Rename(absFrom, absTo); err != nil {
			return fmt.Errorf("move %s -> %s: %v", from, to, mvErr)
		}
	}
	return nil
}

func (r *Repo) DeleteFile(branch, path string) error {
	branch, err := r.resolveRef(branch)
	if err != nil {
		return err
	}
	if err := r.protectedErr(branch); err != nil {
		return err
	}
	path, err = safeRelPath(path)
	if err != nil {
		return err
	}
	wt, err := r.Worktree(branch)
	if err != nil {
		return err
	}
	mu := r.lockBranch(branch)
	mu.Lock()
	defer mu.Unlock()
	abs := filepath.Join(wt, filepath.FromSlash(path))
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("not found: %s", path)
	}
	return os.Remove(abs)
}

// Commit stages and commits worktree changes. The logged-in user is the
// author and committer; the service identity lands as a Co-authored-by trailer.
func (r *Repo) Commit(branch, message, authorName, authorEmail string, paths []string) (string, error) {
	branch, err := r.resolveRef(branch)
	if err != nil {
		return "", err
	}
	if err := r.protectedErr(branch); err != nil {
		return "", err
	}
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("commit message required")
	}
	wt, err := r.Worktree(branch)
	if err != nil {
		return "", err
	}
	mu := r.lockBranch(branch)
	mu.Lock()
	defer mu.Unlock()

	if len(paths) == 0 {
		if _, err := run(wt, nil, "add", "-A"); err != nil {
			return "", err
		}
	} else {
		args := append([]string{"add", "-A", "--"}, paths...)
		for _, p := range paths {
			if _, err := safeRelPath(p); err != nil {
				return "", err
			}
		}
		if _, err := run(wt, nil, args...); err != nil {
			return "", err
		}
	}
	// OKF bundles: regenerate index.md/log.md so the derived reserved files
	// ride in the same commit (no-op unless the workspace opted in)
	r.regenerateOKF(wt, message, authorName)

	// the human is both author AND committer; the service records its
	// involvement as a Co-authored-by trailer instead
	env := []string{
		"GIT_COMMITTER_NAME=" + authorName,
		"GIT_COMMITTER_EMAIL=" + authorEmail,
	}
	message = r.withServiceTrailer(message)
	if _, err := run(wt, env, "commit", "--no-verify",
		"--author", fmt.Sprintf("%s <%s>", authorName, authorEmail), "-m", message); err != nil {
		return "", err
	}
	sha, err := run(wt, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

// withServiceTrailer appends "Co-authored-by: <service>" so the tool's
// involvement stays visible although the user is author and committer.
func (r *Repo) withServiceTrailer(msg string) string {
	t := fmt.Sprintf("Co-authored-by: %s <%s>", r.committer.CommitterName, r.committer.CommitterEmail)
	trimmed := strings.TrimRight(msg, "\n")
	if strings.Contains(trimmed, t) {
		return trimmed + "\n"
	}
	// trailers must form one contiguous final paragraph
	sep := "\n\n"
	if i := strings.LastIndex(trimmed, "\n\n"); i >= 0 && strings.Contains(trimmed[i+2:], "Co-authored-by:") {
		sep = "\n"
	}
	return trimmed + sep + t + "\n"
}
