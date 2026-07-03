package store

import (
	"database/sql"
	"errors"
	"time"
)

type PR struct {
	ID           int64  `json:"-"`
	Repo         string `json:"repo"`
	Number       int    `json:"number"`
	Title        string `json:"title"`
	Body         string `json:"body"`
	SourceBranch string `json:"source"`
	TargetBranch string `json:"target"`
	Author       User   `json:"author"`
	State        string `json:"state"`
	MergedCommit string `json:"mergedCommit,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
	MergedAt     int64  `json:"mergedAt,omitempty"`
}

type PRComment struct {
	ID             int64  `json:"id"`
	Author         User   `json:"author"`
	FilePath       string `json:"filePath,omitempty"`
	Line           int    `json:"line,omitempty"`
	AnchoredCommit string `json:"anchoredCommit,omitempty"`
	Body           string `json:"body"`
	Resolved       bool   `json:"resolved"`
	CreatedAt      int64  `json:"createdAt"`
}

type PRApproval struct {
	User      User   `json:"user"`
	CommitSha string `json:"commitSha"`
	CreatedAt int64  `json:"createdAt"`
}

func (s *Store) CreatePR(repo, title, body, source, target string, authorID int64) (*PR, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// serialize number assignment per repo — MAX()+1 under concurrency would
	// otherwise race into the UNIQUE(repo, number) constraint
	if _, err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext($1))", repo); err != nil {
		return nil, err
	}
	var next int
	if err := tx.QueryRow(rebind("SELECT COALESCE(MAX(number), 0) + 1 FROM prs WHERE repo = ?"), repo).Scan(&next); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	var id int64
	err = tx.QueryRow(rebind(`INSERT INTO prs (repo, number, title, body, source_branch, target_branch, author_id, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'open', ?) RETURNING id`), repo, next, title, body, source, target, authorID, now).Scan(&id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.prBy("p.id = ?", id)
}

func (s *Store) PRByNumber(repo string, number int) (*PR, error) {
	return s.prBy("p.repo = ? AND p.number = ?", repo, number)
}

// OpenPRForBranch finds the open PR whose source is the given branch.
func (s *Store) OpenPRForBranch(repo, source string) (*PR, error) {
	return s.prBy("p.repo = ? AND p.source_branch = ? AND p.state = 'open'", repo, source)
}

const prSelect = `SELECT p.id, p.repo, p.number, p.title, COALESCE(p.body,''), p.source_branch, p.target_branch,
	p.state, COALESCE(p.merged_commit,''), p.created_at, COALESCE(p.merged_at,0),
	u.id, u.provider, u.name, u.email
	FROM prs p JOIN users u ON u.id = p.author_id `

func scanPR(row interface{ Scan(...any) error }) (*PR, error) {
	pr := &PR{}
	err := row.Scan(&pr.ID, &pr.Repo, &pr.Number, &pr.Title, &pr.Body, &pr.SourceBranch, &pr.TargetBranch,
		&pr.State, &pr.MergedCommit, &pr.CreatedAt, &pr.MergedAt,
		&pr.Author.ID, &pr.Author.Provider, &pr.Author.Name, &pr.Author.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return pr, err
}

func (s *Store) prBy(where string, args ...any) (*PR, error) {
	return scanPR(s.queryRow(prSelect+"WHERE "+where, args...))
}

func (s *Store) ListPRs(repo, state string) ([]*PR, error) {
	q := prSelect + "WHERE p.repo = ?"
	args := []any{repo}
	if state != "" && state != "all" {
		q += " AND p.state = ?"
		args = append(args, state)
	}
	q += " ORDER BY p.number DESC"
	rows, err := s.query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*PR{}
	for rows.Next() {
		pr, err := scanPR(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

func (s *Store) SetPRState(id int64, state, mergedCommit string) error {
	var mergedAt any
	if state == "merged" {
		mergedAt = time.Now().Unix()
	}
	_, err := s.exec("UPDATE prs SET state = ?, merged_commit = NULLIF(?, ''), merged_at = ? WHERE id = ?",
		state, mergedCommit, mergedAt, id)
	return err
}

// ---------------------------------------------------------------- comments

func (s *Store) AddComment(prID, authorID int64, filePath string, line int, anchoredCommit, body string) (int64, error) {
	var id int64
	err := s.queryRow(`INSERT INTO pr_comments (pr_id, author_id, file_path, line, anchored_commit, body, created_at)
		VALUES (?, ?, NULLIF(?, ''), NULLIF(?, 0), NULLIF(?, ''), ?, ?) RETURNING id`,
		prID, authorID, filePath, line, anchoredCommit, body, time.Now().Unix()).Scan(&id)
	return id, err
}

func (s *Store) Comments(prID int64) ([]PRComment, error) {
	rows, err := s.query(`SELECT c.id, COALESCE(c.file_path,''), COALESCE(c.line,0), COALESCE(c.anchored_commit,''),
		c.body, c.resolved, c.created_at, u.id, u.provider, u.name, u.email
		FROM pr_comments c JOIN users u ON u.id = c.author_id WHERE c.pr_id = ? ORDER BY c.created_at`, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PRComment{}
	for rows.Next() {
		var c PRComment
		if err := rows.Scan(&c.ID, &c.FilePath, &c.Line, &c.AnchoredCommit, &c.Body, &c.Resolved, &c.CreatedAt,
			&c.Author.ID, &c.Author.Provider, &c.Author.Name, &c.Author.Email); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- approvals

func (s *Store) Approve(prID, userID int64, commitSha string) error {
	_, err := s.exec(`INSERT INTO pr_approvals (pr_id, user_id, commit_sha, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(pr_id, user_id) DO UPDATE SET commit_sha = excluded.commit_sha, created_at = excluded.created_at`,
		prID, userID, commitSha, time.Now().Unix())
	return err
}

func (s *Store) Approvals(prID int64) ([]PRApproval, error) {
	rows, err := s.query(`SELECT a.commit_sha, a.created_at, u.id, u.provider, u.name, u.email
		FROM pr_approvals a JOIN users u ON u.id = a.user_id WHERE a.pr_id = ? ORDER BY a.created_at`, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PRApproval{}
	for rows.Next() {
		var a PRApproval
		if err := rows.Scan(&a.CommitSha, &a.CreatedAt, &a.User.ID, &a.User.Provider, &a.User.Name, &a.User.Email); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
