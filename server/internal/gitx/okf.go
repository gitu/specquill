package gitx

import (
	"log"
	"strings"
	"time"

	"reqbase/server/internal/okf"
)

// maxOKFLogEntries caps log.md — full history stays in git itself.
const maxOKFLogEntries = 200

// regenerateOKF keeps an opted-in bundle (root index.md declares
// okf_version) conformant at commit time: per-directory index.md files and
// log.md are rewritten in the worktree and staged, so they land in the SAME
// commit as the change they describe. The pending commit's own log entry is
// synthesized from its message/author; earlier entries come from git log.
// Generation is best-effort: a failure never blocks the user's commit.
func (r *Repo) regenerateOKF(wt, message, authorName string) {
	if !okf.Enabled(wt) {
		return
	}
	changed, err := okf.GenerateIndexes(wt)
	if err != nil {
		log.Printf("okf indexes %s: %v", r.key, err)
		return
	}

	entries := []okf.LogEntry{{
		Date:    time.Now().UTC().Format("2006-01-02"),
		Author:  authorName,
		Subject: strings.SplitN(strings.TrimSpace(message), "\n", 2)[0],
	}}
	// %as = author date short, %an = author name, %s = subject
	if out, err := run(wt, nil, "log", "--pretty=format:%as\x1f%an\x1f%s", "-n", "199"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			f := strings.SplitN(line, "\x1f", 3)
			if len(f) == 3 && len(entries) < maxOKFLogEntries {
				entries = append(entries, okf.LogEntry{Date: f[0], Author: f[1], Subject: f[2]})
			}
		}
	}
	if wrote, err := okf.WriteLog(wt, entries); err != nil {
		log.Printf("okf log %s: %v", r.key, err)
	} else if wrote {
		changed = append(changed, "log.md")
	}

	if len(changed) > 0 {
		if _, err := run(wt, nil, append([]string{"add", "--"}, changed...)...); err != nil {
			log.Printf("okf stage %s: %v", r.key, err)
		}
	}
}
