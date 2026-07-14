package api

// GitHub push webhooks: POST /hooks/github lets pushes to a registered
// repo's remote propagate immediately instead of waiting for the sync
// interval. The endpoint carries no session — the HMAC-SHA256 signature
// (X-Hub-Signature-256, shared secret) is the only authentication, which is
// also why it is exempt from the CSRF guard.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// remoteFullName normalizes a git remote URL to the GitHub "owner/repo"
// form webhooks report — https, ssh scp-like, and ssh:// shapes all
// resolve; non-GitHub-shaped remotes (local paths) return "".
func remoteFullName(remote string) string {
	r := strings.TrimSpace(remote)
	r = strings.TrimSuffix(r, "/")
	r = strings.TrimSuffix(r, ".git")
	switch {
	case strings.HasPrefix(r, "https://"), strings.HasPrefix(r, "http://"), strings.HasPrefix(r, "ssh://"):
		r = r[strings.Index(r, "://")+3:]
		parts := strings.Split(r, "/")
		// need host + owner + repo, all non-empty
		if len(parts) < 3 || parts[len(parts)-2] == "" || parts[len(parts)-1] == "" {
			return ""
		}
		return strings.ToLower(parts[len(parts)-2] + "/" + parts[len(parts)-1])
	case strings.Contains(r, "@") && strings.Contains(r, ":"): // git@github.com:owner/repo
		path := r[strings.LastIndex(r, ":")+1:]
		if strings.Count(path, "/") != 1 {
			return ""
		}
		return strings.ToLower(path)
	default:
		return ""
	}
}

// POST /hooks/github — ping and push events.
func (s *Server) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Webhooks.GitHub.Enabled {
		jsonError(w, http.StatusNotFound, "github webhooks not enabled")
		return
	}
	secret := os.Getenv(s.cfg.Webhooks.GitHub.SecretEnv)
	if secret == "" {
		jsonError(w, http.StatusInternalServerError, "webhook secret not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(r.Header.Get("X-Hub-Signature-256"))) {
		jsonError(w, http.StatusUnauthorized, "signature mismatch")
		return
	}

	switch r.Header.Get("X-GitHub-Event") {
	case "ping":
		jsonOK(w, map[string]any{"ok": true})
	case "push":
		var payload struct {
			Ref        string `json:"ref"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			jsonError(w, http.StatusBadRequest, "parse payload: "+err.Error())
			return
		}
		branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
		matched := s.syncPushedRepo(payload.Repository.FullName, branch)
		jsonOK(w, map[string]any{"ok": true, "matched": matched})
	default:
		jsonOK(w, map[string]any{"ok": true, "ignored": r.Header.Get("X-GitHub-Event")})
	}
}

// syncPushedRepo fetches every registered repo whose remote is the pushed
// GitHub repository and fast-forwards the default branch when that is what
// moved. Errors are logged, never fatal — the sync interval remains the
// backstop. Returns how many repos matched.
func (s *Server) syncPushedRepo(fullName, branch string) int {
	want := strings.ToLower(fullName)
	if want == "" {
		return 0
	}
	matched := 0
	for _, repo := range s.git.Repos() {
		if remoteFullName(repo.Cfg.Remote) != want {
			continue
		}
		matched++
		if err := repo.Fetch(); err != nil {
			log.Printf("webhook: fetch %s: %v", repo.Key(), err)
			continue
		}
		s.publish("fetch", repo.Key(), "")
		// writable repos serve local branches — ff the default branch so the
		// pushed state is what readers see (read-only fetches update heads
		// directly). A diverged branch is left alone, same as manual pull.
		if repo.Writable() && branch == repo.Cfg.DefaultBranch {
			if _, updated, err := repo.Pull(branch); err != nil {
				log.Printf("webhook: pull %s %s: %v", repo.Key(), branch, err)
			} else if updated {
				s.publish("pull", repo.Key(), branch)
			}
		}
	}
	return matched
}
