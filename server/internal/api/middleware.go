package api

import (
	"net/http"
	"strings"

	"reqbase/server/internal/auth"
)

// requireAuth resolves the session (or the -dev auto-user) and attaches the
// user to the request context; /api requests without a session get 401.
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
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), u)))
	})
}

// csrfGuard rejects state-changing requests that lack the X-Reqbase header.
// Together with SameSite=Lax cookies this blocks cross-site request forgery
// without token machinery — browsers won't let cross-origin JS set the header.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !strings.HasPrefix(r.URL.Path, "/auth/callback") && r.Header.Get("X-Reqbase") != "1" {
				jsonError(w, http.StatusForbidden, "missing X-Reqbase header")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
