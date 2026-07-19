// Package auth implements OIDC (code+PKCE), local argon2id users, and opaque
// cookie sessions backed by the store.
package auth

import (
	"context"
	"net/http"
	"time"

	"specquill/server/internal/config"
	"specquill/server/internal/store"
)

const SessionCookie = "specquill_session"

type ctxKey struct{}

// Sessions issues and resolves session cookies.
type Sessions struct {
	Store  *store.Store
	TTL    time.Duration
	Secure bool
}

func NewSessions(st *store.Store, cfg *config.Config) *Sessions {
	return &Sessions{Store: st, TTL: cfg.Session.TTL, Secure: cfg.Session.CookieSecure}
}

// Issue mints a session cookie for userID, bound to tenantID (0 = unbound,
// honored on any host — the self-host default).
func (s *Sessions) Issue(w http.ResponseWriter, userID, tenantID int64) error {
	id, err := s.Store.CreateSession(userID, tenantID, s.TTL)
	if err != nil {
		return err
	}
	// browser-session cookie (no MaxAge): the server enforces the idle
	// timeout by sliding expires_at on each request — a fixed MaxAge would
	// log active users out TTL after login
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: id, Path: "/",
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *Sessions) Clear(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		_ = s.Store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

// Resolve returns the logged-in user for a request and the tenant its session
// is bound to (0 = unbound), or (nil, 0) when there is no valid session.
func (s *Sessions) Resolve(r *http.Request) (*store.User, int64) {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return nil, 0
	}
	u, tenantID, err := s.Store.SessionUser(c.Value, s.TTL)
	if err != nil {
		return nil, 0
	}
	return u, tenantID
}

// WithUser / UserFrom pass the authenticated user through the request context.
func WithUser(ctx context.Context, u *store.User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

func UserFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(ctxKey{}).(*store.User)
	return u
}
