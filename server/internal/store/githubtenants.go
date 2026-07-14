package store

import (
	"database/sql"
	"errors"
	"time"
)

// Store methods backing GitHub-App tenant management: tenants keyed by
// installation, membership revocation on uninstall, per-tenant sources for
// reference repos, and the user's GitHub login for permission lookups.

// TenantsByProvider lists tenants of one provider ('github' | 'config').
func (s *Store) TenantsByProvider(provider string) ([]Tenant, error) {
	rows, err := s.query(`SELECT id, slug, provider, COALESCE(installation_id, 0), display_name
		FROM tenants WHERE provider = ? ORDER BY id`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tenant{}
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Slug, &t.Provider, &t.Installation, &t.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TenantByInstallation resolves a tenant by its GitHub App installation id.
func (s *Store) TenantByInstallation(installationID int64) (*Tenant, error) {
	t := &Tenant{}
	err := s.queryRow(`SELECT id, slug, provider, COALESCE(installation_id, 0), display_name
		FROM tenants WHERE installation_id = ?`, installationID).
		Scan(&t.ID, &t.Slug, &t.Provider, &t.Installation, &t.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// DeleteTenantMembers revokes every membership of a tenant (uninstall:
// access is gone immediately; the tenant row and repo registry survive a
// re-install).
func (s *Store) DeleteTenantMembers(tenantID int64) error {
	_, err := s.exec("DELETE FROM tenant_members WHERE tenant_id = ?", tenantID)
	return err
}

// DeleteMember removes one membership (role sync: permission dropped to none).
func (s *Store) DeleteMember(tenantID, userID int64) error {
	_, err := s.exec("DELETE FROM tenant_members WHERE tenant_id = ? AND user_id = ?", tenantID, userID)
	return err
}

// DeleteTenantRepo drops one repo from the tenant's registry.
func (s *Store) DeleteTenantRepo(tenantID int64, repoID string) error {
	_, err := s.exec("DELETE FROM tenant_repos WHERE tenant_id = ? AND repo_id = ?", tenantID, repoID)
	return err
}

// AddTenantSource registers a tenant-scoped git source (reference repo) and
// grants it to the tenant in one step.
func (s *Store) AddTenantSource(tenantID int64, name, remote, defaultBranch string, grantedBy int64) error {
	now := time.Now().Unix()
	_, err := s.exec(`INSERT INTO sources (tenant_id, name, kind, remote, default_branch, sync_interval, managed_by, created_at)
		VALUES (?, ?, 'git', ?, ?, 300, 'api', ?)
		ON CONFLICT(tenant_id, name) DO UPDATE SET remote = excluded.remote, default_branch = excluded.default_branch`,
		tenantID, name, remote, defaultBranch, now)
	if err != nil {
		return err
	}
	src, err := s.SourceByName(tenantID, name)
	if err != nil {
		return err
	}
	return s.GrantSource(tenantID, src.ID, grantedBy)
}

// DeleteTenantSource revokes and removes a tenant-scoped source.
func (s *Store) DeleteTenantSource(tenantID int64, name string) error {
	src, err := s.SourceByName(tenantID, name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.RevokeGrant(tenantID, src.ID); err != nil {
		return err
	}
	_, err = s.exec("DELETE FROM sources WHERE id = ? AND tenant_id = ?", src.ID, tenantID)
	return err
}

// SetUserLogin records the user's GitHub handle (permission lookups need it;
// the subject stays the immutable numeric id).
func (s *Store) SetUserLogin(userID int64, login string) error {
	_, err := s.exec("UPDATE users SET login = ? WHERE id = ?", login, userID)
	return err
}

// UserLogin reads the stored GitHub handle ('' when unknown).
func (s *Store) UserLogin(userID int64) (string, error) {
	var login string
	err := s.queryRow("SELECT login FROM users WHERE id = ?", userID).Scan(&login)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return login, err
}
