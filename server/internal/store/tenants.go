package store

import (
	"database/sql"
	"errors"
	"time"
)

// Tenancy model (repo-product/docs/specs/specs/multi-tenancy.md): a tenant is a GitHub App
// installation — or the built-in `default` tenant (provider `config`) that
// mirrors the YAML repos list for self-hosting. The canonical repo key
// everywhere else in this store is "<tenant_slug>/<repo_id>".

type Tenant struct {
	ID           int64  `json:"-"`
	Slug         string `json:"slug"`
	Provider     string `json:"provider"` // 'config' | 'github'
	Installation int64  `json:"-"`        // GitHub App installation id (0 = none)
	DisplayName  string `json:"displayName"`
}

type TenantRepo struct {
	TenantID      int64
	RepoID        string
	Mode          string // writable | readonly
	Remote        string
	DefaultBranch string
	GhFullName    string // 'owner/name' when provider=github
	ManagedBy     string // config (boot-reconciled) | api (persists)
}

type Membership struct {
	Tenant Tenant `json:"tenant"`
	Role   string `json:"role"` // admin | member | viewer
	// GrantOnly marks a synthetic membership: no tenant_members row, the
	// user only holds per-repo grants in this tenant (REQ-020).
	GrantOnly bool `json:"grantOnly,omitempty"`
}

// EnsureTenant upserts a tenant by slug and returns it.
func (s *Store) EnsureTenant(slug, provider string, installation int64, displayName string) (*Tenant, error) {
	_, err := s.exec(`INSERT INTO tenants (slug, provider, installation_id, display_name, created_at)
		VALUES (?, ?, NULLIF(?, 0), ?, ?)
		ON CONFLICT(slug) DO UPDATE SET
		  provider = excluded.provider, installation_id = excluded.installation_id,
		  display_name = excluded.display_name`,
		slug, provider, installation, displayName, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	return s.TenantBySlug(slug)
}

func (s *Store) TenantByID(id int64) (*Tenant, error) {
	t := &Tenant{}
	var inst sql.NullInt64
	err := s.queryRow(`SELECT id, slug, provider, installation_id, display_name FROM tenants WHERE id = ?`, id).
		Scan(&t.ID, &t.Slug, &t.Provider, &inst, &t.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	t.Installation = inst.Int64
	return t, err
}

func (s *Store) TenantBySlug(slug string) (*Tenant, error) {
	t := &Tenant{}
	var inst sql.NullInt64
	err := s.queryRow(`SELECT id, slug, provider, installation_id, display_name FROM tenants WHERE slug = ?`, slug).
		Scan(&t.ID, &t.Slug, &t.Provider, &inst, &t.DisplayName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	t.Installation = inst.Int64
	return t, err
}

// ---------------------------------------------------------------- members

// EnsureMember upserts a membership; an existing role is preserved (role
// changes go through SetMemberRole so a sync can't silently downgrade).
func (s *Store) EnsureMember(tenantID, userID int64, role string) error {
	_, err := s.exec(`INSERT INTO tenant_members (tenant_id, user_id, role, synced_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(tenant_id, user_id) DO UPDATE SET synced_at = excluded.synced_at`,
		tenantID, userID, role, time.Now().Unix())
	return err
}

func (s *Store) SetMemberRole(tenantID, userID int64, role string) error {
	_, err := s.exec(`UPDATE tenant_members SET role = ?, synced_at = ? WHERE tenant_id = ? AND user_id = ?`,
		role, time.Now().Unix(), tenantID, userID)
	return err
}

func (s *Store) MemberRole(tenantID, userID int64) (string, error) {
	var role string
	err := s.queryRow(`SELECT role FROM tenant_members WHERE tenant_id = ? AND user_id = ?`, tenantID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

// Memberships lists a user's tenants (stable order: oldest tenant first).
// Tenants where the user only holds per-repo grants (REQ-020) appear with a
// synthetic 'viewer' tenant role: no member row is materialized, so a role
// sync can never revoke grant-only visibility.
func (s *Store) Memberships(userID int64) ([]Membership, error) {
	rows, err := s.query(`SELECT t.id, t.slug, t.provider, COALESCE(t.installation_id, 0), t.display_name,
			COALESCE(m.role, 'viewer'), m.role IS NULL
		FROM tenants t
		LEFT JOIN tenant_members m ON m.tenant_id = t.id AND m.user_id = ?
		WHERE m.user_id IS NOT NULL
		   OR EXISTS (SELECT 1 FROM repo_grants g WHERE g.tenant_id = t.id AND g.user_id = ?)
		ORDER BY t.id`, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Membership{}
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.Tenant.ID, &m.Tenant.Slug, &m.Tenant.Provider, &m.Tenant.Installation,
			&m.Tenant.DisplayName, &m.Role, &m.GrantOnly); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- repos

// SyncTenantRepos makes the tenant's repo registry exactly match `repos`
// (upsert present, delete missing) — used at boot to mirror the YAML list
// into the default tenant, and by installation syncs later.
func (s *Store) SyncTenantRepos(tenantID int64, repos []TenantRepo) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	keep := make([]any, 0, len(repos)+1)
	keep = append(keep, tenantID)
	for _, r := range repos {
		if _, err := tx.Exec(rebind(`INSERT INTO tenant_repos (tenant_id, repo_id, mode, remote, default_branch, gh_full_name, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(tenant_id, repo_id) DO UPDATE SET
			  mode = excluded.mode, remote = excluded.remote,
			  default_branch = excluded.default_branch, gh_full_name = excluded.gh_full_name`),
			tenantID, r.RepoID, r.Mode, r.Remote, r.DefaultBranch, r.GhFullName, now); err != nil {
			return err
		}
		keep = append(keep, r.RepoID)
	}
	q := "DELETE FROM tenant_repos WHERE tenant_id = ? AND managed_by = 'config'"
	if len(repos) > 0 {
		q += " AND repo_id NOT IN (?" + repeat(",?", len(repos)-1) + ")"
	}
	if _, err := tx.Exec(rebind(q), keep...); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) TenantRepos(tenantID int64) ([]TenantRepo, error) {
	rows, err := s.query(`SELECT tenant_id, repo_id, mode, remote, default_branch, gh_full_name, managed_by
		FROM tenant_repos WHERE tenant_id = ? ORDER BY created_at, repo_id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TenantRepo{}
	for rows.Next() {
		var r TenantRepo
		if err := rows.Scan(&r.TenantID, &r.RepoID, &r.Mode, &r.Remote, &r.DefaultBranch, &r.GhFullName, &r.ManagedBy); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TenantRepo reads one repo row (per-repo role derivation needs gh_full_name).
func (s *Store) TenantRepo(tenantID int64, repoID string) (*TenantRepo, error) {
	r := &TenantRepo{}
	err := s.queryRow(`SELECT tenant_id, repo_id, mode, remote, default_branch, gh_full_name, managed_by
		FROM tenant_repos WHERE tenant_id = ? AND repo_id = ?`, tenantID, repoID).
		Scan(&r.TenantID, &r.RepoID, &r.Mode, &r.Remote, &r.DefaultBranch, &r.GhFullName, &r.ManagedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// UpsertTenantRepo registers/updates a single repo row (runtime AddRepo path;
// boot reconciliation uses SyncTenantRepos).
func (s *Store) UpsertTenantRepo(tenantID int64, r TenantRepo) error {
	_, err := s.exec(`INSERT INTO tenant_repos (tenant_id, repo_id, mode, remote, default_branch, gh_full_name, managed_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 'api', ?)
		ON CONFLICT(tenant_id, repo_id) DO UPDATE SET
		  mode = excluded.mode, remote = excluded.remote, default_branch = excluded.default_branch`,
		tenantID, r.RepoID, r.Mode, r.Remote, r.DefaultBranch, r.GhFullName, time.Now().Unix())
	return err
}
