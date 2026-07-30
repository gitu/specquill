package gitx

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"specquill/server/internal/okf"
)

// maxOKFLogEntries caps the bundle log.md — full history stays in git itself.
const maxOKFLogEntries = 200

// contentRoots lists the project content roots registered on this repo
// ("" = repo root — the default). A monorepo project regenerates OKF files
// under its subtree only.
func (r *Repo) contentRoots() []string {
	if r.Cfg.ContentRoot != "" {
		return []string{r.Cfg.ContentRoot}
	}
	return []string{""}
}

// regenerateOKF keeps opted-in bundles (a content root whose index.md
// declares okf_version) conformant at commit time: per-directory index.md
// files are rewritten in the worktree and staged, so they land in the SAME
// commit as the change they describe. log.md is NOT materialized here — it
// is generated on the fly when the OKF bundle is exported (OKFLogEntries);
// a log.md an older producer version committed is retired so it cannot go
// stale in the tree. Generation is best-effort: a failure never blocks the
// commit.
func (r *Repo) regenerateOKF(wt string) {
	for _, root := range r.contentRoots() {
		dir := wt
		if root != "" {
			dir = filepath.Join(wt, filepath.FromSlash(root))
		}
		if !okf.Enabled(dir) {
			continue
		}
		changed, err := okf.GenerateIndexes(dir)
		if err != nil {
			log.Printf("okf indexes %s: %v", r.key, err)
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "log.md")); err == nil {
			if err := os.Remove(filepath.Join(dir, "log.md")); err != nil {
				log.Printf("okf retire log %s: %v", r.key, err)
			} else {
				changed = append(changed, "log.md")
			}
		}

		if len(changed) > 0 {
			staged := make([]string, 0, len(changed))
			for _, c := range changed {
				if root != "" {
					c = root + "/" + c
				}
				staged = append(staged, c)
			}
			if _, err := run(wt, nil, append([]string{"add", "--"}, staged...)...); err != nil {
				log.Printf("okf stage %s: %v", r.key, err)
			}
		}
	}
}

// OKFLogEntries builds the bundle change log from git history at ref:
// newest-first commits touching subdir ("" = the whole tree), capped at
// maxOKFLogEntries. This is the on-the-fly source for the exported bundle's
// log.md.
func (r *Repo) OKFLogEntries(ref, subdir string) ([]okf.LogEntry, error) {
	// %as = author date short, %an = author name, %s = subject
	args := []string{"log", "--pretty=format:%as\x1f%an\x1f%s", "-n", strconv.Itoa(maxOKFLogEntries), ref}
	if subdir != "" {
		args = append(args, "--", subdir)
	}
	out, err := run(r.gitDir, nil, args...)
	if err != nil {
		return nil, err
	}
	var entries []okf.LogEntry
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\x1f", 3)
		if len(f) == 3 {
			entries = append(entries, okf.LogEntry{Date: f[0], Author: f[1], Subject: f[2]})
		}
	}
	return entries, nil
}
