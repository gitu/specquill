package store

import (
	"database/sql"
	"errors"
	"time"
)

// Projects & sources (config-split plan). A project is a writable workspace:
// a repo (repos row) plus a content_root subfolder. A source is a catalog
// entry projects may reference — in a single-tenant deployment the catalog
// IS the availability; in-repo config selects from it. Rows carry
// managed_by: 'config' rows reconcile to the YAML at boot, 'api' rows
// (added in-app) persist across boots.

type Project struct {
	ProjectID   string
	RepoID      string
	ContentRoot string
	ManagedBy   string
}

type Source struct {
	ID            int64
	Name          string
	Kind          string // git | url | openapi | confluence
	Remote        string
	TokenEnv      string
	DefaultBranch string
	SyncInterval  int64 // seconds
	ManagedBy     string
}

// ---------------------------------------------------------------- projects

// SyncProjects reconciles the config-managed projects to exactly `projects`;
// api-managed rows are left alone.
func (s *Store) SyncProjects(projects []Project) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	keep := []any{}
	for _, p := range projects {
		if _, err := tx.Exec(rebind(`INSERT INTO projects (project_id, repo_id, content_root, managed_by, created_at)
			VALUES (?, ?, ?, 'config', ?)
			ON CONFLICT(project_id) DO UPDATE SET
			  repo_id = excluded.repo_id, content_root = excluded.content_root, managed_by = 'config'`),
			p.ProjectID, p.RepoID, p.ContentRoot, now); err != nil {
			return err
		}
		keep = append(keep, p.ProjectID)
	}
	q := "DELETE FROM projects WHERE managed_by = 'config'"
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
	_, err := s.exec(`INSERT INTO projects (project_id, repo_id, content_root, managed_by, created_at)
		VALUES (?, ?, ?, 'api', ?)`,
		p.ProjectID, p.RepoID, p.ContentRoot, time.Now().Unix())
	return err
}

func (s *Store) DeleteProject(projectID string) error {
	_, err := s.exec("DELETE FROM projects WHERE project_id = ?", projectID)
	return err
}

func (s *Store) Projects() ([]Project, error) {
	rows, err := s.query(`SELECT project_id, repo_id, content_root, managed_by
		FROM projects ORDER BY created_at, project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ProjectID, &p.RepoID, &p.ContentRoot, &p.ManagedBy); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Project(projectID string) (*Project, error) {
	p := &Project{}
	err := s.queryRow(`SELECT project_id, repo_id, content_root, managed_by
		FROM projects WHERE project_id = ?`, projectID).
		Scan(&p.ProjectID, &p.RepoID, &p.ContentRoot, &p.ManagedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// ---------------------------------------------------------------- sources

// SyncSources reconciles the config-managed catalog to exactly `sources`;
// api-managed rows persist.
func (s *Store) SyncSources(sources []Source) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	keep := []any{}
	for _, src := range sources {
		if _, err := tx.Exec(rebind(`INSERT INTO sources (name, kind, remote, token_env, default_branch, sync_interval, managed_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, 'config', ?)
			ON CONFLICT(name) DO UPDATE SET
			  kind = excluded.kind, remote = excluded.remote, token_env = excluded.token_env,
			  default_branch = excluded.default_branch, sync_interval = excluded.sync_interval,
			  managed_by = 'config'`),
			src.Name, src.Kind, src.Remote, src.TokenEnv, src.DefaultBranch, src.SyncInterval, now); err != nil {
			return err
		}
		keep = append(keep, src.Name)
	}
	q := "DELETE FROM sources WHERE managed_by = 'config'"
	if len(sources) > 0 {
		q += " AND name NOT IN (?" + repeat(",?", len(sources)-1) + ")"
	}
	if _, err := tx.Exec(rebind(q), keep...); err != nil {
		return err
	}
	return tx.Commit()
}

// Sources lists the catalog.
func (s *Store) Sources() ([]Source, error) {
	rows, err := s.query(`SELECT id, name, kind, remote, token_env, default_branch, sync_interval, managed_by
		FROM sources ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Source{}
	for rows.Next() {
		var src Source
		if err := rows.Scan(&src.ID, &src.Name, &src.Kind, &src.Remote, &src.TokenEnv, &src.DefaultBranch, &src.SyncInterval, &src.ManagedBy); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// SourceByName resolves a catalog entry (ErrNotFound when absent — the
// availability gate for browsing and syncing sources).
func (s *Store) SourceByName(name string) (*Source, error) {
	src := &Source{}
	err := s.queryRow(`SELECT id, name, kind, remote, token_env, default_branch, sync_interval, managed_by
		FROM sources WHERE name = ?`, name).
		Scan(&src.ID, &src.Name, &src.Kind, &src.Remote, &src.TokenEnv, &src.DefaultBranch, &src.SyncInterval, &src.ManagedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return src, err
}
