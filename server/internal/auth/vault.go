package auth

import (
	"context"
	"sync"
)

// TokenVault holds forge personal access tokens in RAM only, keyed by session
// id. By design nothing here ever touches disk: the token's persistent home
// is the user's browser (localStorage), which silently re-authenticates when
// a restart empties the vault or the session expires.
type TokenVault struct {
	mu sync.RWMutex
	m  map[string]string
}

func NewTokenVault() *TokenVault { return &TokenVault{m: map[string]string{}} }

func (v *TokenVault) Put(sessionID, token string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.m[sessionID] = token
}

func (v *TokenVault) Get(sessionID string) (string, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	tok, ok := v.m[sessionID]
	return tok, ok
}

func (v *TokenVault) Delete(sessionID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.m, sessionID)
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
