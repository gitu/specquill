package api

// Per-tenant credential management (encrypted at rest). Admin-gated. Secret
// values are write-only: they are accepted on PUT and never returned by any
// read. Resolution for git fetch/push happens in the gitx TokenFor hook; these
// handlers only administer the store.

import (
	"encoding/json"
	"errors"
	"net/http"

	"specquill/server/internal/auth"
	"specquill/server/internal/store"
)

// credentialKinds is the allowlist of credential slots the API accepts.
var credentialKinds = map[string]bool{
	"git_pat":        true, // personal access token / installation token (bearer-style)
	"git_basic":      true, // username + password/token (HTTP Basic)
	"importer_token": true, // url/openapi/confluence importer credential
}

// GET /api/credentials — the tenant's credentials, metadata only (no secrets).
func (s *Server) listCredentials(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	creds, err := s.store.ListCredentials(t.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, creds)
}

// PUT /api/credentials/{kind}[/{ref}] — create or replace a credential. Body:
// {username?, secret}. The secret is write-only; the response echoes metadata.
func (s *Server) putCredential(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	if !credentialKinds[kind] {
		jsonError(w, http.StatusBadRequest, "unknown credential kind")
		return
	}
	ref := r.PathValue("ref") // "" for the tenant-default slot
	var body struct {
		Username string `json:"username"`
		Secret   string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Secret == "" {
		jsonError(w, http.StatusBadRequest, "secret is required")
		return
	}
	u := auth.UserFrom(r.Context())
	err := s.store.PutCredential(t.ID, kind, ref, body.Username, []byte(body.Secret), u.ID)
	if errors.Is(err, store.ErrSecretsDisabled) {
		jsonError2(w, http.StatusServiceUnavailable, "credential store not configured (set secrets.master_key_*)", "secrets_disabled")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]string{"kind": kind, "ref": ref})
}

// DELETE /api/credentials/{kind}[/{ref}] — revoke a credential.
func (s *Server) deleteCredential(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	if err := s.store.RevokeCredential(t.ID, r.PathValue("kind"), r.PathValue("ref")); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
