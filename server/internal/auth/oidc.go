package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"specquill/server/internal/config"
)

const oauthCookie = "specquill_oauth"

// OIDC drives the authorization-code + PKCE flow against the configured IdP.
type OIDC struct {
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
}

func NewOIDC(ctx context.Context, cfg *config.Config) (*OIDC, error) {
	o := cfg.Auth.OIDC
	provider, err := oidc.NewProvider(ctx, o.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery %s: %w", o.Issuer, err)
	}
	scopes := o.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	return &OIDC{
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: o.ClientID}),
		oauth: oauth2.Config{
			ClientID:     o.ClientID,
			ClientSecret: os.Getenv(o.ClientSecretEnv),
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.BaseURL + "/auth/callback",
			Scopes:       scopes,
		},
	}, nil
}

func randB64(n int) string {
	raw := make([]byte, n)
	_, _ = rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// Begin redirects to the IdP, stashing state + PKCE verifier in a short-lived
// HttpOnly cookie.
func (o *OIDC) Begin(w http.ResponseWriter, r *http.Request, secure bool) {
	state := randB64(24)
	verifier := randB64(48)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: state + "." + verifier, Path: "/auth",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(10 * time.Minute),
	})
	url := o.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"))
	http.Redirect(w, r, url, http.StatusFound)
}

type Claims struct {
	Subject string
	Name    string
	Email   string
}

// Finish validates the callback and returns the identity claims.
func (o *OIDC) Finish(w http.ResponseWriter, r *http.Request, secure bool) (*Claims, error) {
	cookie, err := r.Cookie(oauthCookie)
	if err != nil {
		return nil, fmt.Errorf("missing oauth state cookie")
	}
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: "", Path: "/auth", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
	var state, verifier string
	if i := len(cookie.Value); i > 0 {
		parts := [2]string{}
		if n := copyParts(cookie.Value, &parts); n != 2 {
			return nil, fmt.Errorf("malformed oauth state cookie")
		}
		state, verifier = parts[0], parts[1]
	}
	if r.URL.Query().Get("state") != state {
		return nil, fmt.Errorf("state mismatch")
	}
	tok, err := o.oauth.Exchange(r.Context(), r.URL.Query().Get("code"),
		oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, fmt.Errorf("code exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}
	idToken, err := o.verifier.Verify(r.Context(), rawID)
	if err != nil {
		return nil, fmt.Errorf("id token verify: %w", err)
	}
	var claims struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, err
	}
	name := claims.Name
	if name == "" {
		name = claims.PreferredUsername
	}
	if claims.Email == "" {
		return nil, fmt.Errorf("IdP returned no email claim; email is required for git authorship")
	}
	if name == "" {
		name = claims.Email
	}
	return &Claims{Subject: idToken.Subject, Name: name, Email: claims.Email}, nil
}

func copyParts(v string, out *[2]string) int {
	for i := 0; i < len(v); i++ {
		if v[i] == '.' {
			out[0], out[1] = v[:i], v[i+1:]
			return 2
		}
	}
	return 1
}
