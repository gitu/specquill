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

// webhookSecrets returns the accepted HMAC secrets: the plain repo-webhook
// secret and/or the GitHub App's webhook secret — both operator-controlled.
func (s *Server) webhookSecrets() []string {
	var out []string
	if s.cfg.Webhooks.GitHub.Enabled {
		if v := os.Getenv(s.cfg.Webhooks.GitHub.SecretEnv); v != "" {
			out = append(out, v)
		}
	}
	if s.cfg.GitHubApp.Enabled() {
		if v := os.Getenv(s.cfg.GitHubApp.WebhookSecretEnv); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// POST /hooks/github — repo webhooks (ping, push) and GitHub App webhooks
// (installation lifecycle) share the endpoint; the signature decides entry.
func (s *Server) githubWebhook(w http.ResponseWriter, r *http.Request) {
	secrets := s.webhookSecrets()
	if len(secrets) == 0 {
		jsonError(w, http.StatusNotFound, "github webhooks not enabled")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		jsonError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	got := []byte(r.Header.Get("X-Hub-Signature-256"))
	valid := false
	for _, secret := range secrets {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if hmac.Equal([]byte(want), got) {
			valid = true
			break
		}
	}
	if !valid {
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
	case "installation":
		s.handleInstallationEvent(w, body)
	case "installation_repositories":
		s.handleInstallationReposEvent(w, body)
	default:
		jsonOK(w, map[string]any{"ok": true, "ignored": r.Header.Get("X-GitHub-Event")})
	}
}

// handleInstallationEvent syncs the tenant lifecycle: created/unsuspended
// installations become (or reactivate) a tenant; deleted/suspended ones are
// locked out immediately — memberships revoked, repos deregistered from the
// git manager. Store rows survive so a re-install restores the tenant.
func (s *Server) handleInstallationEvent(w http.ResponseWriter, body []byte) {
	var payload struct {
		Action       string `json:"action"`
		Installation struct {
			ID      int64 `json:"id"`
			Account struct {
				Login string `json:"login"`
			} `json:"account"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		jsonError(w, http.StatusBadRequest, "parse payload: "+err.Error())
		return
	}
	inst := payload.Installation
	switch payload.Action {
	case "created", "unsuspend", "new_permissions_accepted":
		slug := strings.ToLower(inst.Account.Login)
		if slug == "" || inst.ID == 0 {
			jsonError(w, http.StatusBadRequest, "installation without account")
			return
		}
		if _, err := s.store.EnsureTenant(slug, "github", inst.ID, inst.Account.Login); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Printf("github app: installation %s (%d) %s", slug, inst.ID, payload.Action)
		jsonOK(w, map[string]any{"ok": true, "tenant": slug})
	case "deleted", "suspend":
		ten, err := s.store.TenantByInstallation(inst.ID)
		if err != nil {
			jsonOK(w, map[string]any{"ok": true, "ignored": "unknown installation"})
			return
		}
		if err := s.store.DeleteTenantMembers(ten.ID); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if repos, err := s.store.TenantRepos(ten.ID); err == nil {
			for _, tr := range repos {
				s.git.RemoveRepo(ten.Slug + "/" + tr.RepoID)
			}
		}
		log.Printf("github app: installation %s (%d) %s — memberships revoked", ten.Slug, inst.ID, payload.Action)
		jsonOK(w, map[string]any{"ok": true, "tenant": ten.Slug})
	default:
		jsonOK(w, map[string]any{"ok": true, "ignored": payload.Action})
	}
}

// handleInstallationReposEvent drops adopted repos the installation no
// longer grants (additions surface as picker candidates, nothing to store).
func (s *Server) handleInstallationReposEvent(w http.ResponseWriter, body []byte) {
	var payload struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Removed []struct {
			FullName string `json:"full_name"`
		} `json:"repositories_removed"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		jsonError(w, http.StatusBadRequest, "parse payload: "+err.Error())
		return
	}
	ten, err := s.store.TenantByInstallation(payload.Installation.ID)
	if err != nil {
		jsonOK(w, map[string]any{"ok": true, "ignored": "unknown installation"})
		return
	}
	removed := 0
	if len(payload.Removed) > 0 {
		gone := map[string]bool{}
		for _, r := range payload.Removed {
			gone[strings.ToLower(r.FullName)] = true
		}
		if repos, err := s.store.TenantRepos(ten.ID); err == nil {
			for _, tr := range repos {
				if !gone[strings.ToLower(tr.GhFullName)] {
					continue
				}
				_ = s.store.DeleteProject(ten.ID, tr.RepoID)
				_ = s.store.DeleteTenantSource(ten.ID, tr.RepoID)
				_ = s.store.DeleteTenantRepo(ten.ID, tr.RepoID)
				s.git.RemoveRepo(ten.Slug + "/" + tr.RepoID)
				s.publish("repos-changed", ten.Slug+"/"+tr.RepoID, "")
				removed++
			}
		}
	}
	jsonOK(w, map[string]any{"ok": true, "removed": removed})
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
