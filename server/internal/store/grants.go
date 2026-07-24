package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Per-repo user grants (REQ-020): explicit access layered on top of the
// deployment role. A grant never comes from a sync, so a role change cannot
// drop it — the effective role is max(deployment role, granted), resolved in
// the api layer.

type RepoGrant struct {
	RepoID    string `json:"repo"`
	UserID    int64  `json:"userId"`
	Role      string `json:"role"` // viewer | member
	Name      string `json:"name"`
	Email     string `json:"email"`
	Provider  string `json:"provider"`
	CreatedAt int64  `json:"createdAt"`
}

type GrantInvite struct {
	ID        int64  `json:"id"`
	RepoID    string `json:"repo"`
	Matcher   string `json:"matcher"` // lowercased email
	Role      string `json:"role"`
	CreatedAt int64  `json:"createdAt"`
}

type MemberInfo struct {
	UserID   int64  `json:"userId"`
	Role     string `json:"role"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Provider string `json:"provider"`
}

// UpsertRepoGrant creates or re-roles an explicit grant.
func (s *Store) UpsertRepoGrant(repoID string, userID int64, role string, grantedBy int64) error {
	_, err := s.exec(`INSERT INTO repo_grants (repo_id, user_id, role, granted_by, created_at)
		VALUES (?, ?, ?, NULLIF(?, 0), ?)
		ON CONFLICT(repo_id, user_id) DO UPDATE SET role = excluded.role`,
		repoID, userID, role, grantedBy, time.Now().Unix())
	return err
}

func (s *Store) DeleteRepoGrant(repoID string, userID int64) error {
	_, err := s.exec("DELETE FROM repo_grants WHERE repo_id = ? AND user_id = ?", repoID, userID)
	return err
}

// RepoGrantRole reads one user's explicit grant on a repo (ErrNotFound when
// none exists).
func (s *Store) RepoGrantRole(repoID string, userID int64) (string, error) {
	var role string
	err := s.queryRow("SELECT role FROM repo_grants WHERE repo_id = ? AND user_id = ?",
		repoID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

// RepoGrants lists a repo's explicit grants with grantee identity (admin view).
func (s *Store) RepoGrants(repoID string) ([]RepoGrant, error) {
	rows, err := s.query(`SELECT g.repo_id, g.user_id, g.role, u.name, u.email, u.provider, g.created_at
		FROM repo_grants g JOIN users u ON u.id = g.user_id
		WHERE g.repo_id = ? ORDER BY g.created_at, u.name`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RepoGrant{}
	for rows.Next() {
		var g RepoGrant
		if err := rows.Scan(&g.RepoID, &g.UserID, &g.Role, &g.Name, &g.Email, &g.Provider, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UserRepoGrants maps repo_id → granted role for one user (repo-list
// filtering: one query instead of one per repo).
func (s *Store) UserRepoGrants(userID int64) (map[string]string, error) {
	rows, err := s.query("SELECT repo_id, role FROM repo_grants WHERE user_id = ?", userID)
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

// AddGrantInvite records a pending grant for a not-yet-seen user; the
// matcher is an email address, stored lowercased.
func (s *Store) AddGrantInvite(repoID, matcher, role string, grantedBy int64) error {
	_, err := s.exec(`INSERT INTO repo_grant_invites (repo_id, matcher, role, granted_by, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(repo_id, matcher) DO UPDATE SET role = excluded.role`,
		repoID, strings.ToLower(matcher), role, grantedBy, time.Now().Unix())
	return err
}

func (s *Store) DeleteGrantInvite(id int64) error {
	_, err := s.exec("DELETE FROM repo_grant_invites WHERE id = ?", id)
	return err
}

// RepoGrantInvites lists a repo's pending invites (admin view).
func (s *Store) RepoGrantInvites(repoID string) ([]GrantInvite, error) {
	rows, err := s.query(`SELECT id, repo_id, matcher, role, created_at
		FROM repo_grant_invites WHERE repo_id = ? ORDER BY created_at, id`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GrantInvite{}
	for rows.Next() {
		var v GrantInvite
		if err := rows.Scan(&v.ID, &v.RepoID, &v.Matcher, &v.Role, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ClaimGrantInvites converts every invite matching the user's email into a
// grant and deletes the invites — called on each successful login,
// idempotent. An existing grant is kept (not downgraded).
func (s *Store) ClaimGrantInvites(userID int64, email string) error {
	email = strings.ToLower(email)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(rebind(`SELECT id, repo_id, role, granted_by FROM repo_grant_invites
		WHERE matcher = ?`),
		email)
	if err != nil {
		return err
	}
	type claim struct {
		id, grantedBy int64
		repoID, role  string
	}
	var claims []claim
	for rows.Next() {
		var c claim
		var by sql.NullInt64
		if err := rows.Scan(&c.id, &c.repoID, &c.role, &by); err != nil {
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
		if _, err := tx.Exec(rebind(`INSERT INTO repo_grants (repo_id, user_id, role, granted_by, created_at)
			VALUES (?, ?, ?, NULLIF(?, 0), ?) ON CONFLICT(repo_id, user_id) DO NOTHING`),
			c.repoID, userID, c.role, c.grantedBy, now); err != nil {
			return err
		}
		if _, err := tx.Exec(rebind("DELETE FROM repo_grant_invites WHERE id = ?"), c.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------------------------------------------------------------- members

// MemberList lists the deployment's enrolled users with identity (admin view).
func (s *Store) MemberList() ([]MemberInfo, error) {
	rows, err := s.query(`SELECT id, role, name, email, provider
		FROM users WHERE role <> '' ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MemberInfo{}
	for rows.Next() {
		var m MemberInfo
		if err := rows.Scan(&m.UserID, &m.Role, &m.Name, &m.Email, &m.Provider); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UserByEmail resolves a grant target: an email address (any provider,
// case-insensitive). Ambiguous emails resolve to the oldest account.
func (s *Store) UserByEmail(identifier string) (*User, error) {
	return s.userBy("LOWER(email) = ? ORDER BY id LIMIT 1", strings.ToLower(identifier))
}

// HasAnyGrant reports whether the user holds at least one per-repo grant —
// grant-only users (deployment role '') see exactly their granted repos.
func (s *Store) HasAnyGrant(userID int64) (bool, error) {
	var one int
	err := s.queryRow("SELECT 1 FROM repo_grants WHERE user_id = ? LIMIT 1", userID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
