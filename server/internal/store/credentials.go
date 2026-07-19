package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"specquill/server/internal/secrets"
)

// SetSealer installs the encryptor used by the tenant-credential methods.
// Called once at startup; when nil, credential writes/reads return
// ErrSecretsDisabled so callers fall through to the legacy resolution chain.
func (s *Store) SetSealer(sl *secrets.Sealer) { s.sealer = sl }

// ErrSecretsDisabled is returned when a credential operation is attempted but
// no master key is configured (secrets.Sealer is nil).
var ErrSecretsDisabled = errors.New("credential store disabled: no secrets master key configured")

// Credential is credential metadata — it NEVER carries the secret value.
type Credential struct {
	Kind       string `json:"kind"`
	Ref        string `json:"ref"`
	Username   string `json:"username,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
	RotatedAt  int64  `json:"rotatedAt,omitempty"`
	LastUsedAt int64  `json:"lastUsedAt,omitempty"`
}

// PutCredential encrypts and upserts a credential for (tenant, kind, ref).
// Replacing an existing slot stamps rotated_at. The plaintext secret is used
// only to seal and is not retained.
func (s *Store) PutCredential(tenantID int64, kind, ref, username string, secret []byte, createdBy int64) error {
	if s.sealer == nil {
		return ErrSecretsDisabled
	}
	blob, err := s.sealer.Seal(secrets.AAD(tenantID, kind, ref), secret)
	if err != nil {
		return err
	}
	var by any
	if createdBy != 0 {
		by = createdBy
	}
	now := time.Now().Unix()
	_, err = s.exec(`INSERT INTO tenant_credentials
		  (tenant_id, kind, ref, username, secret_blob, key_version, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, kind, ref) DO UPDATE SET
		  username = excluded.username, secret_blob = excluded.secret_blob,
		  key_version = excluded.key_version, rotated_at = ?`,
		tenantID, kind, ref, username, blob, secrets.KeyVersion, by, now, now)
	return err
}

// GetCredentialSecret decrypts and returns the credential for (tenant, kind,
// ref), touching last_used_at. Returns ErrNotFound when absent so callers can
// fall through to the next resolution source. The returned secret is caller-
// owned; overwrite it when done.
func (s *Store) GetCredentialSecret(tenantID int64, kind, ref string) (username string, secret []byte, err error) {
	if s.sealer == nil {
		return "", nil, ErrSecretsDisabled
	}
	var blob []byte
	err = s.queryRow(`SELECT username, secret_blob FROM tenant_credentials
		WHERE tenant_id = ? AND kind = ? AND ref = ?`, tenantID, kind, ref).Scan(&username, &blob)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	secret, err = s.sealer.Open(secrets.AAD(tenantID, kind, ref), blob)
	if err != nil {
		return "", nil, fmt.Errorf("decrypt credential %s/%s: %w", kind, ref, err)
	}
	// best-effort usage stamp; a failure here must not fail the resolution
	_, _ = s.exec(`UPDATE tenant_credentials SET last_used_at = ? WHERE tenant_id = ? AND kind = ? AND ref = ?`,
		time.Now().Unix(), tenantID, kind, ref)
	return username, secret, nil
}

// RevokeCredential deletes a credential slot.
func (s *Store) RevokeCredential(tenantID int64, kind, ref string) error {
	_, err := s.exec(`DELETE FROM tenant_credentials WHERE tenant_id = ? AND kind = ? AND ref = ?`,
		tenantID, kind, ref)
	return err
}

// ListCredentials returns metadata for a tenant's credentials — never secrets.
func (s *Store) ListCredentials(tenantID int64) ([]Credential, error) {
	rows, err := s.query(`SELECT kind, ref, username, created_at, rotated_at, last_used_at
		FROM tenant_credentials WHERE tenant_id = ? ORDER BY kind, ref`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.Kind, &c.Ref, &c.Username, &c.CreatedAt, &c.RotatedAt, &c.LastUsedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
