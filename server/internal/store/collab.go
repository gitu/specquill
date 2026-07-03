package store

import (
	"time"
)

// ---------------------------------------------------------------- rooms

type CollabRoom struct {
	Repo, Branch, Path                string
	LastSeq, SeedSeq, FlushedSeq      int64
	FlushedSha                        string
}

func (s *Store) CollabRoom(repo, branch, path string) (*CollabRoom, error) {
	r := &CollabRoom{Repo: repo, Branch: branch, Path: path}
	err := s.db.QueryRow(`SELECT last_seq, seed_seq, flushed_seq, flushed_sha FROM collab_rooms
		WHERE repo = ? AND branch = ? AND path = ?`, repo, branch, path).
		Scan(&r.LastSeq, &r.SeedSeq, &r.FlushedSeq, &r.FlushedSha)
	if err != nil {
		return nil, ErrNotFound
	}
	return r, nil
}

func (s *Store) UpsertCollabRoom(r *CollabRoom) error {
	_, err := s.db.Exec(`INSERT INTO collab_rooms (repo, branch, path, last_seq, seed_seq, flushed_seq, flushed_sha, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo, branch, path) DO UPDATE SET
		  last_seq = excluded.last_seq, seed_seq = excluded.seed_seq,
		  flushed_seq = excluded.flushed_seq, flushed_sha = excluded.flushed_sha,
		  updated_at = excluded.updated_at`,
		r.Repo, r.Branch, r.Path, r.LastSeq, r.SeedSeq, r.FlushedSeq, r.FlushedSha, time.Now().Unix())
	return err
}

func (s *Store) DeleteCollabRoom(repo, branch, path string) error {
	if _, err := s.db.Exec("DELETE FROM collab_updates WHERE repo = ? AND branch = ? AND path = ?", repo, branch, path); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM collab_rooms WHERE repo = ? AND branch = ? AND path = ?", repo, branch, path)
	return err
}

// OrphanedCollabRooms lists rooms whose log holds edits never flushed to git.
func (s *Store) OrphanedCollabRooms(repo string) ([]CollabRoom, error) {
	rows, err := s.db.Query(`SELECT branch, path, last_seq, seed_seq, flushed_seq, flushed_sha
		FROM collab_rooms WHERE repo = ? AND last_seq > flushed_seq AND last_seq > seed_seq`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CollabRoom{}
	for rows.Next() {
		r := CollabRoom{Repo: repo}
		if err := rows.Scan(&r.Branch, &r.Path, &r.LastSeq, &r.SeedSeq, &r.FlushedSeq, &r.FlushedSha); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- updates

func (s *Store) AppendCollabUpdates(repo, branch, path string, firstSeq int64, payloads [][]byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, p := range payloads {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO collab_updates (repo, branch, path, seq, payload) VALUES (?, ?, ?, ?, ?)`,
			repo, branch, path, firstSeq+int64(i), p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CollabUpdates(repo, branch, path string) ([][]byte, error) {
	rows, err := s.db.Query(`SELECT payload FROM collab_updates WHERE repo = ? AND branch = ? AND path = ? ORDER BY seq`,
		repo, branch, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var p []byte
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CompactCollabLog replaces every update with seq <= covered by one snapshot
// row (stored at seq = covered, lowest surviving seq).
func (s *Store) CompactCollabLog(repo, branch, path string, covered int64, snapshot []byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM collab_updates WHERE repo = ? AND branch = ? AND path = ? AND seq <= ?`,
		repo, branch, path, covered); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO collab_updates (repo, branch, path, seq, payload) VALUES (?, ?, ?, ?, ?)`,
		repo, branch, path, covered, snapshot); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------- contributors

func (s *Store) RecordContributor(repo, branch, path string, userID int64) error {
	_, err := s.db.Exec(`INSERT INTO collab_contributors (repo, branch, path, user_id, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(repo, branch, path, user_id) DO UPDATE SET updated_at = excluded.updated_at`,
		repo, branch, path, userID, time.Now().Unix())
	return err
}

// Contributors returns distinct users who edited any of the paths on a branch
// (all paths when paths is empty).
func (s *Store) Contributors(repo, branch string, paths []string) ([]User, error) {
	q := `SELECT DISTINCT u.id, u.provider, u.subject, u.name, u.email
		FROM collab_contributors c JOIN users u ON u.id = c.user_id
		WHERE c.repo = ? AND c.branch = ?`
	args := []any{repo, branch}
	if len(paths) > 0 {
		q += " AND c.path IN (?" + repeat(",?", len(paths)-1) + ")"
		for _, p := range paths {
			args = append(args, p)
		}
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Provider, &u.Subject, &u.Name, &u.Email); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) ClearContributors(repo, branch string, paths []string) error {
	q := "DELETE FROM collab_contributors WHERE repo = ? AND branch = ?"
	args := []any{repo, branch}
	if len(paths) > 0 {
		q += " AND path IN (?" + repeat(",?", len(paths)-1) + ")"
		for _, p := range paths {
			args = append(args, p)
		}
	}
	_, err := s.db.Exec(q, args...)
	return err
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
