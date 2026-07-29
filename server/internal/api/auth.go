package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"specquill/server/internal/auth"
	"specquill/server/internal/forge"
)

// GET /auth/login — the SPA's login page (which offers whatever
// /auth/providers reports: forge-PAT and/or local).
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusFound)
}

// GET /auth/providers — which login methods the SPA should offer (public).
// In forge-PAT mode the payload carries what the login page needs to guide
// token creation: kind, the deep link, and the scopes to grant.
func (s *Server) authProviders(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"local": s.cfg.Auth.Local.Enabled,
	}
	if s.patMode() {
		out["forge"] = map[string]any{
			"kind":           s.cfg.Auth.Forge.Kind,
			"baseUrl":        s.cfg.Auth.Forge.BaseURL,
			"tokenCreateUrl": s.cfg.TokenCreateLink(),
			"scopes":         s.cfg.ForgeScopes(),
		}
	}
	jsonOK(w, out)
}

// POST /auth/pat/login {token} — forge-PAT login: verify the token against
// the forge's /user endpoint, derive the deployment role from the token
// owner's permission on the main project, issue a session and keep the token
// in the RAM vault for that session's git/forge operations.
func (s *Server) authPatLogin(w http.ResponseWriter, r *http.Request) {
	if !s.patMode() {
		jsonError(w, http.StatusNotFound, "forge login not enabled")
		return
	}
	var body struct{ Token string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Token) == "" {
		jsonError(w, http.StatusBadRequest, "token required")
		return
	}
	token := strings.TrimSpace(body.Token)
	if len(s.cfg.Projects) == 0 {
		jsonError(w, http.StatusInternalServerError, "no project configured")
		return
	}
	// the deployment's main project anchors identity and role (v1: one
	// writable repository per deployment)
	pc := s.cfg.Projects[0]
	client, err := forge.New(pc.Forge, pc.Remote, token)
	if err != nil || client == nil {
		jsonError(w, http.StatusInternalServerError, "forge auth misconfigured: "+errStr(err))
		return
	}
	fu, err := client.CurrentUser(r.Context())
	if err != nil {
		// only a forge VERDICT on the token (401/403) is a rejection; a rate
		// limit, forge 5xx or network failure never judged the token, and
		// answering 401 here would make clients treat a working token as bad
		var se *forge.StatusError
		if errors.As(err, &se) && (se.StatusCode == http.StatusUnauthorized || se.StatusCode == http.StatusForbidden) {
			jsonError2(w, http.StatusUnauthorized, "token rejected by "+s.cfg.Auth.Forge.Kind+": "+err.Error(), "invalid_token")
			return
		}
		jsonError2(w, http.StatusBadGateway,
			s.cfg.Auth.Forge.Kind+" could not verify the token — try again later: "+err.Error(), "forge_unavailable")
		return
	}
	role, err := client.ProjectRole(r.Context())
	if err != nil {
		// 401/403/404 = the forge hid or refused the project for this token;
		// anything else is the forge being unavailable, not a permission answer
		var se *forge.StatusError
		if errors.As(err, &se) && (se.StatusCode == http.StatusUnauthorized ||
			se.StatusCode == http.StatusForbidden || se.StatusCode == http.StatusNotFound) {
			jsonError2(w, http.StatusForbidden,
				"this token has no access to "+pc.ID+" — grant access on the forge or use a different token", "no_project_access")
			return
		}
		jsonError2(w, http.StatusBadGateway,
			s.cfg.Auth.Forge.Kind+" could not answer the project-access check — try again later: "+err.Error(), "forge_unavailable")
		return
	}
	if role == "none" {
		jsonError2(w, http.StatusForbidden,
			"this token has no access to "+pc.ID+" — grant access on the forge or use a different token", "no_project_access")
		return
	}
	u, err := s.store.UpsertUser(s.cfg.Auth.Forge.Kind, fu.Subject, fu.Name, fu.Email)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// the forge is the source of truth for the deployment role — refresh it
	// every login so permission changes propagate (admin_emails still floor
	// to admin via deployRole)
	if err := s.store.SetUserRole(u.ID, role); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	u.Role = role
	s.claimInvites(u.ID, u.Email)
	sid, err := s.sessions.Issue(w, u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.vault.Put(sid, token)

	resp := map[string]any{
		"id": u.ID, "name": u.Name, "email": u.Email, "provider": u.Provider,
		"initials": initialsOf(u.Name), "role": role,
	}
	if warn := scopeWarning(s.cfg.ForgeScopes(), fu.Scopes); warn != "" {
		resp["warning"] = warn
	}
	jsonOK(w, resp)
}

// scopeWarning reports missing token scopes when the forge disclosed them
// (GitHub classic PATs only) — a warning, never a hard failure, because
// fine-grained tokens don't expose scopes at all.
func scopeWarning(wanted, got []string) string {
	if len(got) == 0 {
		return ""
	}
	have := map[string]bool{}
	for _, s := range got {
		have[s] = true
	}
	var missing []string
	for _, s := range wanted {
		if !have[s] {
			missing = append(missing, s)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return "token is missing the " + strings.Join(missing, ", ") + " scope — pushing or opening merge requests may fail"
}

func errStr(err error) string {
	if err == nil {
		return "client disabled"
	}
	return err.Error()
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
	if _, err := s.sessions.Issue(w, userID); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, _ := s.store.UserByID(userID)
	if u != nil {
		s.claimInvites(u.ID, u.Email)
	}
	jsonOK(w, u)
}

// claimInvites converts pending repo-grant invites matching this identity
// into grants (REQ-020) — best-effort, a failure must not block the login.
func (s *Server) claimInvites(userID int64, email string) {
	if err := s.store.ClaimGrantInvites(userID, email); err != nil {
		log.Printf("claim grant invites: %v", err)
	}
}

// POST /auth/logout
func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		s.vault.Delete(c.Value)
	}
	s.sessions.Clear(w, r)
	jsonOK(w, map[string]bool{"ok": true})
}

// GET /api/me
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	role, err := s.deployRole(u) // auto-enrolls
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"id": u.ID, "name": u.Name, "email": u.Email, "provider": u.Provider,
		"initials": initialsOf(u.Name), "role": role.String(),
		"mergeMode": s.mergeMode(),
	})
}

// mergeMode tells the SPA how work reaches the default branch: "local"
// (in-app merge) or "forge" (push + MR/PR, forge-PAT mode).
func (s *Server) mergeMode() string {
	if s.patMode() {
		return "forge"
	}
	return "local"
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
