package store

// Tenant-owned sealed credentials (REQ-023). The store only ever sees
// nonce+ciphertext — sealing/unsealing is internal/secrets' job. Ciphertext
// and nonce are json-invisible so a Credential can never leak through an
// API response by accident.

import (
	"database/sql"
	"errors"
	"time"
)

type Credential struct {
	ID         int64  `json:"id"`
	TenantID   int64  `json:"-"`
	Name       string `json:"name"`
	Username   string `json:"username,omitempty"`
	Nonce      []byte `json:"-"`
	Ciphertext []byte `json:"-"`
	KeyID      string `json:"-"`
	CreatedBy  int64  `json:"createdBy,omitempty"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	RepoCount  int    `json:"repoCount"` // attached tenant_repos + sources
}

func (s *Store) AddCredential(c Credential) (int64, error) {
	now := time.Now().Unix()
	var id int64
	err := s.queryRow(`INSERT INTO credentials (tenant_id, name, username, nonce, ciphertext, key_id, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, 0), ?, ?) RETURNING id`,
		c.TenantID, c.Name, c.Username, c.Nonce, c.Ciphertext, c.KeyID, c.CreatedBy, now, now).Scan(&id)
	return id, err
}

// Credentials lists a tenant's credentials with their reference counts —
// metadata only; handlers marshal the redacted struct directly.
func (s *Store) Credentials(tenantID int64) ([]Credential, error) {
	rows, err := s.query(`SELECT c.id, c.tenant_id, c.name, c.username, c.key_id,
			COALESCE(c.created_by, 0), c.created_at, c.updated_at,
			(SELECT COUNT(*) FROM tenant_repos tr WHERE tr.credential_id = c.id)
			+ (SELECT COUNT(*) FROM sources src WHERE src.credential_id = c.id)
		FROM credentials c WHERE c.tenant_id = ? ORDER BY c.name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Credential{}
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.Username, &c.KeyID,
			&c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.RepoCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Credential loads one row including the sealed material (TokenFor,
// re-seal on rotation).
func (s *Store) Credential(tenantID, id int64) (*Credential, error) {
	c := &Credential{}
	err := s.queryRow(`SELECT id, tenant_id, name, username, nonce, ciphertext, key_id, COALESCE(created_by, 0), created_at, updated_at
		FROM credentials WHERE tenant_id = ? AND id = ?`, tenantID, id).
		Scan(&c.ID, &c.TenantID, &c.Name, &c.Username, &c.Nonce, &c.Ciphertext, &c.KeyID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// UpdateCredential renames and/or re-seals. Sealed fields update only when
// nonce is non-nil (token rotation).
func (s *Store) UpdateCredential(tenantID, id int64, name, username string, nonce, ciphertext []byte, keyID string) error {
	now := time.Now().Unix()
	var err error
	if nonce != nil {
		_, err = s.exec(`UPDATE credentials SET name = ?, username = ?, nonce = ?, ciphertext = ?, key_id = ?, updated_at = ?
			WHERE tenant_id = ? AND id = ?`, name, username, nonce, ciphertext, keyID, now, tenantID, id)
	} else {
		_, err = s.exec(`UPDATE credentials SET name = ?, username = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`,
			name, username, now, tenantID, id)
	}
	return err
}

// CredentialRefCount — attached repos + sources; deletion refuses while > 0.
func (s *Store) CredentialRefCount(id int64) (int, error) {
	var n int
	err := s.queryRow(`SELECT (SELECT COUNT(*) FROM tenant_repos WHERE credential_id = ?)
		+ (SELECT COUNT(*) FROM sources WHERE credential_id = ?)`, id, id).Scan(&n)
	return n, err
}

func (s *Store) DeleteCredential(tenantID, id int64) error {
	res, err := s.exec(`DELETE FROM credentials WHERE tenant_id = ? AND id = ?`, tenantID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetRepoCredential attaches (or, with credID 0, detaches) a credential to
// a tenant repo. The credential must belong to the same tenant.
func (s *Store) SetRepoCredential(tenantID int64, repoID string, credID int64) error {
	if credID != 0 {
		if _, err := s.Credential(tenantID, credID); err != nil {
			return err
		}
	}
	res, err := s.exec(`UPDATE tenant_repos SET credential_id = NULLIF(?, 0) WHERE tenant_id = ? AND repo_id = ?`,
		credID, tenantID, repoID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RepoCredential resolves the credential attached to <tenantSlug>/<repoID>
// — the TokenFor hot path (one join per uncached fetch/push).
func (s *Store) RepoCredential(tenantSlug, repoID string) (*Credential, error) {
	c := &Credential{}
	err := s.queryRow(`SELECT c.id, c.tenant_id, c.name, c.username, c.nonce, c.ciphertext, c.key_id, COALESCE(c.created_by, 0), c.created_at, c.updated_at
		FROM credentials c
		JOIN tenants t ON t.id = c.tenant_id
		JOIN tenant_repos tr ON tr.tenant_id = c.tenant_id AND tr.credential_id = c.id
		WHERE t.slug = ? AND tr.repo_id = ?`, tenantSlug, repoID).
		Scan(&c.ID, &c.TenantID, &c.Name, &c.Username, &c.Nonce, &c.Ciphertext, &c.KeyID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}
