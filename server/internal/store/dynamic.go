package store

// Token-scoped dynamic projects (REQ-025): per-user project rows and the
// last-use stamps the reclamation janitor reads. Rows survive clone
// reclamation — the clone is a cache, the row is the user's open list.

import (
	"database/sql"
	"errors"
	"time"
)

type UserProject struct {
	UserID        int64  `json:"-"`
	ProjectID     string `json:"id"`
	ForgeRepoID   string `json:"-"`
	Name          string `json:"name"`
	Spelling      string `json:"spelling"`
	Remote        string `json:"remote"`
	ContentRoot   string `json:"contentRoot"`
	DefaultBranch string `json:"defaultBranch"`
	Role          string `json:"role"`
	LastUsed      int64  `json:"lastUsed"`
}

// UpsertUserProject records (or refreshes — the role and remote follow the
// forge) one dynamically opened project for one user.
func (s *Store) UpsertUserProject(p UserProject) error {
	now := time.Now().Unix()
	_, err := s.exec(`INSERT INTO user_projects
		(user_id, project_id, forge_repo_id, name, spelling, remote, content_root, default_branch, role, created_at, last_used)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, project_id) DO UPDATE SET
		  spelling = excluded.spelling, remote = excluded.remote,
		  content_root = excluded.content_root, default_branch = excluded.default_branch,
		  role = excluded.role, last_used = excluded.last_used`,
		p.UserID, p.ProjectID, p.ForgeRepoID, p.Name, p.Spelling, p.Remote,
		p.ContentRoot, p.DefaultBranch, p.Role, now, now)
	return err
}

func (s *Store) UserProject(userID int64, projectID string) (*UserProject, error) {
	p := &UserProject{}
	err := s.queryRow(`SELECT user_id, project_id, forge_repo_id, name, spelling, remote,
		content_root, default_branch, role, last_used
		FROM user_projects WHERE user_id = ? AND project_id = ?`, userID, projectID).
		Scan(&p.UserID, &p.ProjectID, &p.ForgeRepoID, &p.Name, &p.Spelling, &p.Remote,
			&p.ContentRoot, &p.DefaultBranch, &p.Role, &p.LastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) UserProjects(userID int64) ([]UserProject, error) {
	rows, err := s.query(`SELECT user_id, project_id, forge_repo_id, name, spelling, remote,
		content_root, default_branch, role, last_used
		FROM user_projects WHERE user_id = ? ORDER BY created_at, project_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserProject{}
	for rows.Next() {
		var p UserProject
		if err := rows.Scan(&p.UserID, &p.ProjectID, &p.ForgeRepoID, &p.Name, &p.Spelling,
			&p.Remote, &p.ContentRoot, &p.DefaultBranch, &p.Role, &p.LastUsed); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUserProject(userID int64, projectID string) error {
	_, err := s.exec("DELETE FROM user_projects WHERE user_id = ? AND project_id = ?", userID, projectID)
	return err
}

// TouchClone stamps last-use for one clone in one user scope. Called on
// request resolution; the janitor treats a missing stamp as "never used".
func (s *Store) TouchClone(scope, repoID string) {
	now := time.Now().Unix()
	_, _ = s.exec(`INSERT INTO clone_uses (scope, repo_id, last_used) VALUES (?, ?, ?)
		ON CONFLICT(scope, repo_id) DO UPDATE SET last_used = excluded.last_used`,
		scope, repoID, now)
	_, _ = s.exec("UPDATE user_projects SET last_used = ? WHERE project_id = ?", now, repoID)
}

// CloneUse returns the last-use stamp (unix seconds; 0 = no stamp).
func (s *Store) CloneUse(scope, repoID string) int64 {
	var t int64
	_ = s.queryRow("SELECT last_used FROM clone_uses WHERE scope = ? AND repo_id = ?", scope, repoID).Scan(&t)
	return t
}

// DropCloneUse forgets the stamp of a reclaimed clone.
func (s *Store) DropCloneUse(scope, repoID string) {
	_, _ = s.exec("DELETE FROM clone_uses WHERE scope = ? AND repo_id = ?", scope, repoID)
}
