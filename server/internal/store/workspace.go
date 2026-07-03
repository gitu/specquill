package store

import (
	"database/sql"
	"errors"
)

// WorkspaceBranch returns the branch a user has claimed for a repo ("" when none).
func (s *Store) WorkspaceBranch(repo string, userID int64) (string, error) {
	var branch string
	err := s.db.QueryRow("SELECT branch FROM workspace_branches WHERE repo = ? AND user_id = ?", repo, userID).Scan(&branch)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return branch, err
}

// ClaimWorkspaceBranch records ownership; fails on UNIQUE(repo,branch) when
// another user already claimed that name.
func (s *Store) ClaimWorkspaceBranch(repo, branch string, userID int64) error {
	_, err := s.db.Exec("INSERT INTO workspace_branches (repo, user_id, branch) VALUES (?, ?, ?)", repo, userID, branch)
	return err
}

// WorkspaceOwner returns the owning user id of a claimed branch (ErrNotFound otherwise).
func (s *Store) WorkspaceOwner(repo, branch string) (int64, error) {
	var id int64
	err := s.db.QueryRow("SELECT user_id FROM workspace_branches WHERE repo = ? AND branch = ?", repo, branch).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
