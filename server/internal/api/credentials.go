package api

// Tenant credential management (REQ-023) — tenant-admin gated. Tokens are
// write-only past entry: list/read responses carry metadata only (the store
// struct hides sealed fields from JSON), rotation replaces without display,
// and log lines carry ids and labels — never token material.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"specquill/server/internal/auth"
	"specquill/server/internal/secrets"
	"specquill/server/internal/store"
)

// secretsReady gates the credential endpoints on the master key being
// configured; everything else (installation tokens, token_env) still works
// without it.
func (s *Server) secretsReady(w http.ResponseWriter) bool {
	if s.secrets == nil {
		jsonError2(w, http.StatusNotImplemented, "credential storage requires "+secrets.EnvKey, "secrets_unconfigured")
		return false
	}
	return true
}

// GET /api/t/{tenant}/credentials — metadata only, never token material.
func (s *Server) listCredentials(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	creds, err := s.store.Credentials(t.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, creds)
}

// POST /api/t/{tenant}/credentials {name, username?, token}
func (s *Server) createCredential(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok || !s.secretsReady(w) {
		return
	}
	var body struct{ Name, Username, Token string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" || body.Token == "" {
		jsonError(w, http.StatusBadRequest, "name and token are required")
		return
	}
	id, err := s.sealAndStore(t, strings.TrimSpace(body.Name), body.Username, body.Token, auth.UserFrom(r.Context()).ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]int64{"id": id})
}

func (s *Server) sealAndStore(t *store.Tenant, name, username, token string, by int64) (int64, error) {
	nonce, ct, keyID, err := s.secrets.Seal(t.ID, token)
	if err != nil {
		return 0, err
	}
	return s.store.AddCredential(store.Credential{
		TenantID: t.ID, Name: name, Username: username,
		Nonce: nonce, Ciphertext: ct, KeyID: keyID, CreatedBy: by,
	})
}

// PUT /api/t/{tenant}/credentials/{id} {name?, username?, token?} — a
// present token re-seals (rotation); name/username update in place.
func (s *Server) updateCredential(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid credential id")
		return
	}
	cur, err := s.store.Credential(t.ID, id)
	if errors.Is(err, store.ErrNotFound) {
		jsonError(w, http.StatusNotFound, "unknown credential")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var body struct {
		Name     *string `json:"name"`
		Username *string `json:"username"`
		Token    string  `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	name, username := cur.Name, cur.Username
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		name = strings.TrimSpace(*body.Name)
	}
	if body.Username != nil {
		username = *body.Username
	}
	var nonce, ct []byte
	keyID := ""
	if body.Token != "" {
		if !s.secretsReady(w) {
			return
		}
		if nonce, ct, keyID, err = s.secrets.Seal(t.ID, body.Token); err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.store.UpdateCredential(t.ID, id, name, username, nonce, ct, keyID); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// DELETE /api/t/{tenant}/credentials/{id} — refuses while referenced.
func (s *Server) deleteCredential(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "invalid credential id")
		return
	}
	if n, err := s.store.CredentialRefCount(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	} else if n > 0 {
		jsonError2(w, http.StatusConflict, "credential is attached to "+strconv.Itoa(n)+" repo(s) — detach first", "credential_in_use")
		return
	}
	if err := s.store.DeleteCredential(t.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			jsonError(w, http.StatusNotFound, "unknown credential")
			return
		}
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// PUT /api/t/{tenant}/repos/{repo}/settings/credential {credentialId} —
// attach (or detach with null/0). Repo-admin gated in the router.
func (s *Server) putRepoCredential(w http.ResponseWriter, r *http.Request) {
	t, ok := s.tenant(w, r)
	if !ok {
		return
	}
	repoID, err := s.grantRepoID(t, r.PathValue("repo"))
	if err != nil {
		jsonError(w, http.StatusNotFound, "unknown repo")
		return
	}
	var body struct {
		CredentialID int64 `json:"credentialId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if err := s.store.SetRepoCredential(t.ID, repoID, body.CredentialID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			jsonError(w, http.StatusNotFound, "unknown repo or credential")
			return
		}
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
