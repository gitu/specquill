package store

import (
	"database/sql"
	"errors"
	"time"
)

// Projects & sources (config-split plan). A project is a writable workspace:
// a repo (tenant_repos row) plus a content_root subfolder. A source is a
// stage-1 catalog entry projects may reference; grants (stage 2) attach
// sources to tenants. Rows carry managed_by: 'config' rows reconcile to the
// YAML at boot, 'api' rows (added in-app) persist across boots.

type Project struct {
	TenantID    int64
	ProjectID   string
	RepoID      string
	ContentRoot string
	ManagedBy   string
}

type Source struct {
	ID            int64
	TenantID      int64 // 0 = global (app YAML / platform)
	Name          string
	Kind          string // git | url | openapi | confluence
	Remote        string
	TokenEnv      string
	DefaultBranch string
	SyncInterval  int64 // seconds
	ManagedBy     string
}

// ---------------------------------------------------------------- projects

// SyncTenantProjects reconciles the tenant's config-managed projects to
// exactly `projects`; api-managed rows are left alone.
func (s *Store) SyncTenantProjects(tenantID int64, projects []Project) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	keep := []any{tenantID}
	for _, p := range projects {
		if _, err := tx.Exec(rebind(`INSERT INTO tenant_projects (tenant_id, project_id, repo_id, content_root, managed_by, created_at)
			VALUES (?, ?, ?, ?, 'config', ?)
			ON CONFLICT(tenant_id, project_id) DO UPDATE SET
			  repo_id = excluded.repo_id, content_root = excluded.content_root, managed_by = 'config'`),
			tenantID, p.ProjectID, p.RepoID, p.ContentRoot, now); err != nil {
			return err
		}
		keep = append(keep, p.ProjectID)
	}
	q := "DELETE FROM tenant_projects WHERE tenant_id = ? AND managed_by = 'config'"
	if len(projects) > 0 {
		q += " AND project_id NOT IN (?" + repeat(",?", len(projects)-1) + ")"
	}
	if _, err := tx.Exec(rebind(q), keep...); err != nil {
		return err
	}
	return tx.Commit()
}

// AddProject registers an api-managed project.
func (s *Store) AddProject(p Project) error {
	_, err := s.exec(`INSERT INTO tenant_projects (tenant_id, project_id, repo_id, content_root, managed_by, created_at)
		VALUES (?, ?, ?, ?, 'api', ?)`,
		p.TenantID, p.ProjectID, p.RepoID, p.ContentRoot, time.Now().Unix())
	return err
}

func (s *Store) DeleteProject(tenantID int64, projectID string) error {
	_, err := s.exec("DELETE FROM tenant_projects WHERE tenant_id = ? AND project_id = ?", tenantID, projectID)
	return err
}

func (s *Store) TenantProjects(tenantID int64) ([]Project, error) {
	rows, err := s.query(`SELECT tenant_id, project_id, repo_id, content_root, managed_by
		FROM tenant_projects WHERE tenant_id = ? ORDER BY created_at, project_id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.TenantID, &p.ProjectID, &p.RepoID, &p.ContentRoot, &p.ManagedBy); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) TenantProject(tenantID int64, projectID string) (*Project, error) {
	p := &Project{}
	err := s.queryRow(`SELECT tenant_id, project_id, repo_id, content_root, managed_by
		FROM tenant_projects WHERE tenant_id = ? AND project_id = ?`, tenantID, projectID).
		Scan(&p.TenantID, &p.ProjectID, &p.RepoID, &p.ContentRoot, &p.ManagedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// ---------------------------------------------------------------- sources

// SyncGlobalSources reconciles the config-managed GLOBAL catalog (tenant_id
// NULL) to exactly `sources`; api-managed and tenant-scoped rows persist.
func (s *Store) SyncGlobalSources(sources []Source) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	keep := []any{}
	for _, src := range sources {
		if _, err := tx.Exec(rebind(`INSERT INTO sources (tenant_id, name, kind, remote, token_env, default_branch, sync_interval, managed_by, created_at)
			VALUES (NULL, ?, ?, ?, ?, ?, ?, 'config', ?)
			ON CONFLICT(tenant_id, name) DO UPDATE SET
			  kind = excluded.kind, remote = excluded.remote, token_env = excluded.token_env,
			  default_branch = excluded.default_branch, sync_interval = excluded.sync_interval,
			  managed_by = 'config'`),
			src.Name, src.Kind, src.Remote, src.TokenEnv, src.DefaultBranch, src.SyncInterval, now); err != nil {
			return err
		}
		keep = append(keep, src.Name)
	}
	q := "DELETE FROM sources WHERE tenant_id IS NULL AND managed_by = 'config'"
	if len(sources) > 0 {
		q += " AND name NOT IN (?" + repeat(",?", len(sources)-1) + ")"
	}
	if _, err := tx.Exec(rebind(q), keep...); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SourceByName(tenantID int64, name string) (*Source, error) {
	src := &Source{}
	var tid sql.NullInt64
	// tenant-scoped first, then the global catalog
	err := s.queryRow(`SELECT id, tenant_id, name, kind, remote, token_env, default_branch, sync_interval, managed_by
		FROM sources WHERE name = ? AND (tenant_id = ? OR tenant_id IS NULL)
		ORDER BY tenant_id NULLS LAST LIMIT 1`, name, tenantID).
		Scan(&src.ID, &tid, &src.Name, &src.Kind, &src.Remote, &src.TokenEnv, &src.DefaultBranch, &src.SyncInterval, &src.ManagedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	src.TenantID = tid.Int64
	return src, err
}

// ---------------------------------------------------------------- grants

func (s *Store) GrantSource(tenantID, sourceID, grantedBy int64) error {
	var by any
	if grantedBy != 0 {
		by = grantedBy
	}
	_, err := s.exec(`INSERT INTO source_grants (tenant_id, source_id, granted_by, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(tenant_id, source_id) DO NOTHING`, tenantID, sourceID, by, time.Now().Unix())
	return err
}

func (s *Store) RevokeGrant(tenantID, sourceID int64) error {
	_, err := s.exec("DELETE FROM source_grants WHERE tenant_id = ? AND source_id = ?", tenantID, sourceID)
	return err
}

// SyncGrants makes the tenant's grants exactly `sourceIDs` (boot sync for the
// default tenant; granted_by NULL marks config-managed grants).
func (s *Store) SyncGrants(tenantID int64, sourceIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	keep := []any{tenantID}
	for _, id := range sourceIDs {
		if _, err := tx.Exec(rebind(`INSERT INTO source_grants (tenant_id, source_id, granted_by, created_at)
			VALUES (?, ?, NULL, ?) ON CONFLICT(tenant_id, source_id) DO NOTHING`), tenantID, id, now); err != nil {
			return err
		}
		keep = append(keep, id)
	}
	q := "DELETE FROM source_grants WHERE tenant_id = ? AND granted_by IS NULL"
	if len(sourceIDs) > 0 {
		q += " AND source_id NOT IN (?" + repeat(",?", len(sourceIDs)-1) + ")"
	}
	if _, err := tx.Exec(rebind(q), keep...); err != nil {
		return err
	}
	return tx.Commit()
}

// TenantGrantedSources lists the sources granted to a tenant.
func (s *Store) TenantGrantedSources(tenantID int64) ([]Source, error) {
	rows, err := s.query(`SELECT s.id, COALESCE(s.tenant_id, 0), s.name, s.kind, s.remote, s.token_env, s.default_branch, s.sync_interval, s.managed_by
		FROM source_grants g JOIN sources s ON s.id = g.source_id
		WHERE g.tenant_id = ? ORDER BY s.name`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var src Source
		if err := rows.Scan(&src.ID, &src.TenantID, &src.Name, &src.Kind, &src.Remote, &src.TokenEnv, &src.DefaultBranch, &src.SyncInterval, &src.ManagedBy); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}
