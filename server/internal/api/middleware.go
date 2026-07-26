package api

import (
	"net/http"
	"strings"

	"specquill/server/internal/auth"
)

// requireAuth resolves the session (or the -dev auto-user) and attaches the
// user to the request context; /api requests without a session get 401.
//
// Forge-PAT mode additionally requires the session's token to be present in
// the RAM vault: after a server restart the session row survives in SQLite
// but the token does not, so the request 401s and the SPA silently re-logs-in
// with the token from localStorage.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.devUser != nil {
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), s.devUser)))
			return
		}
		u := s.sessions.Resolve(r)
		if u == nil {
			jsonError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		ctx := auth.WithUser(r.Context(), u)
		if s.patMode() {
			c, err := r.Cookie(auth.SessionCookie)
			if err != nil {
				jsonError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			token, ok := s.vault.Get(c.Value)
			if !ok {
				jsonError2(w, http.StatusUnauthorized, "session has no forge token — sign in again", "token_gone")
				return
			}
			ctx = auth.WithToken(ctx, token)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// csrfGuard rejects state-changing requests that lack the X-SpecQuill header.
// Together with SameSite=Lax cookies this blocks cross-site request forgery
// without token machinery — browsers won't let cross-origin JS set the header.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !strings.HasPrefix(r.URL.Path, "/hooks/") && r.Header.Get("X-SpecQuill") != "1" {
				jsonError(w, http.StatusForbidden, "missing X-SpecQuill header")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
