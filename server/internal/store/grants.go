package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Per-repo user grants (REQ-020): explicit access layered on top of derived
// roles. A grant never comes from a sync, so revoking someone on the git
// host cannot drop it — the effective role is max(derived, granted),
// resolved in the api layer.

type RepoGrant struct {
	RepoID    string `json:"repo"`
	UserID    int64  `json:"userId"`
	Role      string `json:"role"` // viewer | editor | maintainer | admin
	Name      string `json:"name"`
	Email     string `json:"email"`
	Login     string `json:"login,omitempty"`
	Provider  string `json:"provider"`
	CreatedAt int64  `json:"createdAt"`
}

type GrantInvite struct {
	ID        int64  `json:"id"`
	RepoID    string `json:"repo"`
	Kind      string `json:"kind"`    // email | github
	Matcher   string `json:"matcher"` // lowercased email or login
	Role      string `json:"role"`
	CreatedAt int64  `json:"createdAt"`
}

type MemberInfo struct {
	UserID   int64  `json:"userId"`
	Role     string `json:"role"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Login    string `json:"login,omitempty"`
	Provider string `json:"provider"`
}

// UpsertRepoGrant creates or re-roles an explicit grant.
func (s *Store) UpsertRepoGrant(tenantID int64, repoID string, userID int64, role string, grantedBy int64) error {
	_, err := s.exec(`INSERT INTO repo_grants (tenant_id, repo_id, user_id, role, granted_by, created_at)
		VALUES (?, ?, ?, ?, NULLIF(?, 0), ?)
		ON CONFLICT(tenant_id, repo_id, user_id) DO UPDATE SET role = excluded.role`,
		tenantID, repoID, userID, role, grantedBy, time.Now().Unix())
	return err
}

func (s *Store) DeleteRepoGrant(tenantID int64, repoID string, userID int64) error {
	_, err := s.exec("DELETE FROM repo_grants WHERE tenant_id = ? AND repo_id = ? AND user_id = ?",
		tenantID, repoID, userID)
	return err
}

// RepoGrantRole reads one user's explicit grant on a repo (ErrNotFound when
// none exists).
func (s *Store) RepoGrantRole(tenantID int64, repoID string, userID int64) (string, error) {
	var role string
	err := s.queryRow("SELECT role FROM repo_grants WHERE tenant_id = ? AND repo_id = ? AND user_id = ?",
		tenantID, repoID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

// RepoGrants lists a repo's explicit grants with grantee identity (admin view).
func (s *Store) RepoGrants(tenantID int64, repoID string) ([]RepoGrant, error) {
	rows, err := s.query(`SELECT g.repo_id, g.user_id, g.role, u.name, u.email, u.login, u.provider, g.created_at
		FROM repo_grants g JOIN users u ON u.id = g.user_id
		WHERE g.tenant_id = ? AND g.repo_id = ? ORDER BY g.created_at, u.name`, tenantID, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepoGrant{}
	for rows.Next() {
		var g RepoGrant
		if err := rows.Scan(&g.RepoID, &g.UserID, &g.Role, &g.Name, &g.Email, &g.Login, &g.Provider, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UserRepoGrants maps repo_id → granted role for one user in one tenant
// (repo-list filtering: one query instead of one per repo).
func (s *Store) UserRepoGrants(tenantID, userID int64) (map[string]string, error) {
	rows, err := s.query("SELECT repo_id, role FROM repo_grants WHERE tenant_id = ? AND user_id = ?",
		tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var repo, role string
		if err := rows.Scan(&repo, &role); err != nil {
			return nil, err
		}
		out[repo] = role
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- invites

// AddGrantInvite records a pending grant for a not-yet-seen user; kind is
// 'email' or 'github', the matcher is stored lowercased.
func (s *Store) AddGrantInvite(tenantID int64, repoID, kind, matcher, role string, grantedBy int64) error {
	_, err := s.exec(`INSERT INTO repo_grant_invites (tenant_id, repo_id, kind, matcher, role, granted_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, repo_id, kind, matcher) DO UPDATE SET role = excluded.role`,
		tenantID, repoID, kind, strings.ToLower(matcher), role, grantedBy, time.Now().Unix())
	return err
}

func (s *Store) DeleteGrantInvite(tenantID, id int64) error {
	_, err := s.exec("DELETE FROM repo_grant_invites WHERE tenant_id = ? AND id = ?", tenantID, id)
	return err
}

// RepoGrantInvites lists a repo's pending invites (admin view).
func (s *Store) RepoGrantInvites(tenantID int64, repoID string) ([]GrantInvite, error) {
	rows, err := s.query(`SELECT id, repo_id, kind, matcher, role, created_at
		FROM repo_grant_invites WHERE tenant_id = ? AND repo_id = ? ORDER BY created_at, id`, tenantID, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GrantInvite{}
	for rows.Next() {
		var v GrantInvite
		if err := rows.Scan(&v.ID, &v.RepoID, &v.Kind, &v.Matcher, &v.Role, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ClaimGrantInvites converts every invite matching the user's email or
// GitHub login into a grant and deletes the invites — called on each
// successful login, idempotent. An existing grant is kept (not downgraded).
func (s *Store) ClaimGrantInvites(userID int64, email, login string) error {
	email = strings.ToLower(email)
	login = strings.ToLower(login)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(rebind(`SELECT id, tenant_id, repo_id, role, granted_by FROM repo_grant_invites
		WHERE (kind = 'email' AND matcher = ?) OR (kind = 'github' AND matcher = ? AND ? <> '')`),
		email, login, login)
	if err != nil {
		return err
	}
	type claim struct {
		id, tenantID, grantedBy int64
		repoID, role            string
	}
	var claims []claim
	for rows.Next() {
		var c claim
		var by sql.NullInt64
		if err := rows.Scan(&c.id, &c.tenantID, &c.repoID, &c.role, &by); err != nil {
			rows.Close()
			return err
		}
		c.grantedBy = by.Int64
		claims = append(claims, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(claims) == 0 {
		return nil
	}
	now := time.Now().Unix()
	for _, c := range claims {
		if _, err := tx.Exec(rebind(`INSERT INTO repo_grants (tenant_id, repo_id, user_id, role, granted_by, created_at)
			VALUES (?, ?, ?, ?, NULLIF(?, 0), ?) ON CONFLICT(tenant_id, repo_id, user_id) DO NOTHING`),
			c.tenantID, c.repoID, userID, c.role, c.grantedBy, now); err != nil {
			return err
		}
		if _, err := tx.Exec(rebind("DELETE FROM repo_grant_invites WHERE id = ?"), c.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------------------------------------------------------------- members

// TenantMemberList lists a tenant's members with identity (admin view).
func (s *Store) TenantMemberList(tenantID int64) ([]MemberInfo, error) {
	rows, err := s.query(`SELECT m.user_id, m.role, u.name, u.email, u.login, u.provider
		FROM tenant_members m JOIN users u ON u.id = m.user_id
		WHERE m.tenant_id = ? ORDER BY u.name, u.id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MemberInfo{}
	for rows.Next() {
		var m MemberInfo
		if err := rows.Scan(&m.UserID, &m.Role, &m.Name, &m.Email, &m.Login, &m.Provider); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UserByEmailOrLogin resolves a grant target: an email address (any
// provider, case-insensitive) or a GitHub login. Ambiguous emails resolve to
// the oldest account.
func (s *Store) UserByEmailOrLogin(identifier string) (*User, error) {
	id := strings.ToLower(strings.TrimPrefix(identifier, "@"))
	if strings.Contains(id, "@") {
		return s.userBy("LOWER(email) = ? ORDER BY id LIMIT 1", id)
	}
	return s.userBy("LOWER(login) = ? ORDER BY id LIMIT 1", id)
}
