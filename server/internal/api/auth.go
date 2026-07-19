package api

import (
	"encoding/json"
	"log"
	"net/http"

	"specquill/server/internal/auth"
)

// GET /auth/login — OIDC redirect when enabled; GitHub-only setups go
// straight to GitHub; otherwise the SPA's login page (which offers whatever
// /auth/providers reports).
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc != nil {
		s.oidc.Begin(w, r, s.cfg.Session.CookieSecure)
		return
	}
	if s.github != nil && !s.cfg.Auth.Local.Enabled {
		s.github.Begin(w, r, s.cfg.Session.CookieSecure)
		return
	}
	http.Redirect(w, r, "/#/login", http.StatusFound)
}

// GET /auth/providers — which login methods the SPA should offer (public).
func (s *Server) authProviders(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]bool{
		"oidc":   s.oidc != nil,
		"github": s.github != nil,
		"local":  s.cfg.Auth.Local.Enabled,
	})
}

// GET /auth/github/login — start the GitHub OAuth flow.
func (s *Server) authGitHubLogin(w http.ResponseWriter, r *http.Request) {
	if s.github == nil {
		jsonError(w, http.StatusNotFound, "github login not enabled")
		return
	}
	s.github.Begin(w, r, s.cfg.Session.CookieSecure)
}

// GET /auth/github/callback — code exchange → allowlist → session.
func (s *Server) authGitHubCallback(w http.ResponseWriter, r *http.Request) {
	if s.github == nil {
		jsonError(w, http.StatusNotFound, "github login not enabled")
		return
	}
	id, err := s.github.Finish(w, r, s.cfg.Session.CookieSecure)
	if err != nil {
		log.Printf("github callback: %v", err)
		http.Redirect(w, r, "/#/login?error=github", http.StatusFound)
		return
	}
	if !s.github.Allowed(id.Login) {
		log.Printf("github login denied: @%s is not in auth.github.allowed_users", id.Login)
		http.Redirect(w, r, "/#/login?error=forbidden", http.StatusFound)
		return
	}
	u, err := s.store.UpsertUser("github", id.Subject, id.Name, id.Email)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// the @handle backs permission lookups (GitHub-App tenants) — keep it
	// current on every login, handles are renameable
	if err := s.store.SetUserLogin(u.ID, id.Login); err != nil {
		log.Printf("github callback: set login: %v", err)
	}
	if err := s.sessions.Issue(w, u.ID); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// GET /auth/callback — OIDC code exchange → session.
func (s *Server) authCallback(w http.ResponseWriter, r *http.Request) {
	if s.oidc == nil {
		jsonError(w, http.StatusNotFound, "oidc not enabled")
		return
	}
	claims, err := s.oidc.Finish(w, r, s.cfg.Session.CookieSecure)
	if err != nil {
		log.Printf("oidc callback: %v", err)
		http.Redirect(w, r, "/#/login?error=oidc", http.StatusFound)
		return
	}
	u, err := s.store.UpsertUser("oidc", claims.Subject, claims.Name, claims.Email)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.sessions.Issue(w, u.ID); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// POST /auth/local/login {username, password}
func (s *Server) authLocalLogin(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Auth.Local.Enabled {
		jsonError(w, http.StatusForbidden, "local login disabled")
		return
	}
	var body struct{ Username, Password string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
		jsonError(w, http.StatusBadRequest, "username and password required")
		return
	}
	userID, hash, err := s.store.LocalUserHash(body.Username)
	if err != nil || !auth.VerifyPassword(hash, body.Password) {
		jsonError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := s.sessions.Issue(w, userID); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, _ := s.store.UserByID(userID)
	jsonOK(w, u)
}

// POST /auth/logout
func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.Clear(w, r)
	jsonOK(w, map[string]bool{"ok": true})
}

// GET /api/me
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	ms, err := s.memberships(u) // auto-enrolls into the default tenant
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"id": u.ID, "name": u.Name, "email": u.Email, "provider": u.Provider,
		"initials": initialsOf(u.Name), "tenants": ms,
	})
}

func initialsOf(name string) string {
	out := []rune{}
	for i, part := range splitWords(name) {
		if i > 1 {
			break
		}
		r := []rune(part)
		if len(r) > 0 {
			out = append(out, r[0])
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func splitWords(s string) []string {
	var words []string
	cur := ""
	for _, r := range s {
		if r == ' ' || r == '.' || r == '-' || r == '_' {
			if cur != "" {
				words = append(words, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		words = append(words, cur)
	}
	return words
}
