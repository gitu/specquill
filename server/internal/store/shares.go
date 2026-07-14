package store

import (
	"database/sql"
	"errors"
	"time"
)

// Share links: unauthenticated OKF-bundle downloads where the URL token is
// the only credential. One link per (tenant, project); re-minting rotates.

type ShareLink struct {
	TenantID  int64
	ProjectID string
	Token     string
	CreatedBy int64
	CreatedAt int64
}

// SetShareLink creates or rotates the project's share link.
func (s *Store) SetShareLink(tenantID int64, projectID, token string, createdBy int64) error {
	_, err := s.exec(`INSERT INTO share_links (tenant_id, project_id, token, created_by, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, project_id) DO UPDATE SET
		  token = excluded.token, created_by = excluded.created_by, created_at = excluded.created_at`,
		tenantID, projectID, token, createdBy, time.Now().Unix())
	return err
}

func (s *Store) DeleteShareLink(tenantID int64, projectID string) error {
	_, err := s.exec("DELETE FROM share_links WHERE tenant_id = ? AND project_id = ?", tenantID, projectID)
	return err
}

func (s *Store) ShareLink(tenantID int64, projectID string) (*ShareLink, error) {
	return s.shareLink("tenant_id = ? AND project_id = ?", tenantID, projectID)
}

// ShareLinkByToken resolves a share token — the public download path.
func (s *Store) ShareLinkByToken(token string) (*ShareLink, error) {
	return s.shareLink("token = ?", token)
}

func (s *Store) shareLink(where string, args ...any) (*ShareLink, error) {
	l := &ShareLink{}
	err := s.queryRow("SELECT tenant_id, project_id, token, created_by, created_at FROM share_links WHERE "+where, args...).
		Scan(&l.TenantID, &l.ProjectID, &l.Token, &l.CreatedBy, &l.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return l, err
}
