package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Repo registry: the deployment's repos, mirroring the YAML list at boot.
// The canonical repo key everywhere else in this store is the plain repo id.

type RepoRow struct {
	RepoID        string
	Mode          string // writable | readonly
	Remote        string
	DefaultBranch string
	ManagedBy     string // config (boot-reconciled) | api (persists)
}

// SyncRepos makes the repo registry exactly match `repos` (upsert present,
// delete missing) — used at boot to mirror the YAML list.
func (s *Store) SyncRepos(repos []RepoRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	keep := make([]any, 0, len(repos))
	for _, r := range repos {
		if _, err := tx.Exec(`INSERT INTO repos (repo_id, mode, remote, default_branch, created_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(repo_id) DO UPDATE SET
			  mode = excluded.mode, remote = excluded.remote,
			  default_branch = excluded.default_branch`,
			r.RepoID, r.Mode, r.Remote, r.DefaultBranch, now); err != nil {
			return err
		}
		keep = append(keep, r.RepoID)
	}
	q := "DELETE FROM repos WHERE managed_by = 'config'"
	if len(repos) > 0 {
		q += " AND repo_id NOT IN (?" + strings.Repeat(",?", len(repos)-1) + ")"
	}
	if _, err := tx.Exec(q, keep...); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RepoRows() ([]RepoRow, error) {
	rows, err := s.query(`SELECT repo_id, mode, remote, default_branch, managed_by
		FROM repos ORDER BY created_at, repo_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepoRow{}
	for rows.Next() {
		var r RepoRow
		if err := rows.Scan(&r.RepoID, &r.Mode, &r.Remote, &r.DefaultBranch, &r.ManagedBy); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RepoRow reads one repo row.
func (s *Store) RepoRow(repoID string) (*RepoRow, error) {
	r := &RepoRow{}
	err := s.queryRow(`SELECT repo_id, mode, remote, default_branch, managed_by
		FROM repos WHERE repo_id = ?`, repoID).
		Scan(&r.RepoID, &r.Mode, &r.Remote, &r.DefaultBranch, &r.ManagedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// UpsertRepoRow registers/updates a single repo row (runtime AddRepo path;
// boot reconciliation uses SyncRepos).
func (s *Store) UpsertRepoRow(r RepoRow) error {
	_, err := s.exec(`INSERT INTO repos (repo_id, mode, remote, default_branch, managed_by, created_at)
		VALUES (?, ?, ?, ?, 'api', ?)
		ON CONFLICT(repo_id) DO UPDATE SET
		  mode = excluded.mode, remote = excluded.remote, default_branch = excluded.default_branch`,
		r.RepoID, r.Mode, r.Remote, r.DefaultBranch, time.Now().Unix())
	return err
}

// DeleteRepoRow removes a repo row; grants and invites cascade with it.
func (s *Store) DeleteRepoRow(repoID string) error {
	_, err := s.exec(`DELETE FROM repos WHERE repo_id = ?`, repoID)
	return err
}

// ---------------------------------------------------------------- roles

// EnsureUserRole enrolls a user with an initial deployment role; an existing
// role is preserved (changes go through SetUserRole so enrollment can't
// silently downgrade).
func (s *Store) EnsureUserRole(userID int64, role string) error {
	_, err := s.exec(`UPDATE users SET role = ? WHERE id = ? AND role = ''`, role, userID)
	return err
}

func (s *Store) SetUserRole(userID int64, role string) error {
	_, err := s.exec(`UPDATE users SET role = ? WHERE id = ?`, role, userID)
	return err
}
