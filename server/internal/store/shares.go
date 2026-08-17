package store

import (
	"database/sql"
	"errors"
	"time"
)

// Share links: unauthenticated OKF-bundle downloads where the URL token is
// the only credential. One link per project; re-minting rotates.

type ShareLink struct {
	ProjectID string
	Token     string
	CreatedBy int64
	CreatedAt int64
}

// SetShareLink creates or rotates the project's share link.
func (s *Store) SetShareLink(projectID, token string, createdBy int64) error {
	_, err := s.exec(`INSERT INTO share_links (project_id, token, created_by, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
		  token = excluded.token, created_by = excluded.created_by, created_at = excluded.created_at`,
		projectID, token, createdBy, time.Now().Unix())
	return err
}

func (s *Store) DeleteShareLink(projectID string) error {
	_, err := s.exec("DELETE FROM share_links WHERE project_id = ?", projectID)
	return err
}

func (s *Store) ShareLink(projectID string) (*ShareLink, error) {
	return s.shareLink("project_id = ?", projectID)
}

// ShareLinkByToken resolves a share token — the public download path.
func (s *Store) ShareLinkByToken(token string) (*ShareLink, error) {
	return s.shareLink("token = ?", token)
}

func (s *Store) shareLink(where string, args ...any) (*ShareLink, error) {
	l := &ShareLink{}
	err := s.queryRow("SELECT project_id, token, created_by, created_at FROM share_links WHERE "+where, args...).
		Scan(&l.ProjectID, &l.Token, &l.CreatedBy, &l.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return l, err
}
