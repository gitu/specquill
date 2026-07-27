package auth

import (
	"context"
	"sync"
	"time"
)

// TokenVault holds forge personal access tokens in RAM only, keyed by session
// id. By design nothing here ever touches disk: the token's persistent home
// is the user's browser (localStorage), which silently re-authenticates when
// a restart empties the vault or the session expires.
//
// Entries are reaped two ways, since a browser that never comes back would
// otherwise pin one forever: explicitly (logout, or a request whose session
// no longer resolves) and by PruneIdle, which mirrors the store's sliding
// session expiry.
type TokenVault struct {
	mu sync.RWMutex
	m  map[string]*vaultEntry
}

type vaultEntry struct {
	token    string
	lastSeen time.Time
}

func NewTokenVault() *TokenVault { return &TokenVault{m: map[string]*vaultEntry{}} }

func (v *TokenVault) Put(sessionID, token string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.m[sessionID] = &vaultEntry{token: token, lastSeen: time.Now()}
}

func (v *TokenVault) Get(sessionID string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.m[sessionID]
	if !ok {
		return "", false
	}
	e.lastSeen = time.Now() // active sessions never age out
	return e.token, true
}

func (v *TokenVault) Delete(sessionID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.m, sessionID)
}

// PruneIdle drops entries untouched for longer than maxIdle and reports how
// many went. The matching session has already idled out server-side, so the
// token could not have been used again anyway.
func (v *TokenVault) PruneIdle(maxIdle time.Duration) int {
	cutoff := time.Now().Add(-maxIdle)
	v.mu.Lock()
	defer v.mu.Unlock()
	n := 0
	for id, e := range v.m {
		if e.lastSeen.Before(cutoff) {
			delete(v.m, id)
			n++
		}
	}
	return n
}

// Len is the number of live entries (tests, diagnostics).
func (v *TokenVault) Len() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.m)
}

// backdate rewinds an entry's lastSeen — test seam for PruneIdle.
func (v *TokenVault) backdate(sessionID string, d time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if e, ok := v.m[sessionID]; ok {
		e.lastSeen = e.lastSeen.Add(-d)
	}
}

type tokenCtxKey struct{}

// WithToken / TokenFrom pass the session's forge token through the request
// context (forge-PAT mode only; empty otherwise).
func WithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenCtxKey{}, token)
}

func TokenFrom(ctx context.Context) string {
	tok, _ := ctx.Value(tokenCtxKey{}).(string)
	return tok
}
