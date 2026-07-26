// Package store wraps the SQLite database holding users, sessions and
// workspace claims. Workspace content never lands here — it stays in git.
package store

import (
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // cgo-free SQLite driver, registered as "sqlite"
)

//go:embed schema.sql
var schema string

var ErrNotFound = errors.New("not found")

type Store struct {
	db *sql.DB
}

type User struct {
	ID       int64  `json:"id"`
	Provider string `json:"provider"`
	Subject  string `json:"-"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Role     string `json:"role"` // deployment role on the authz ladder; '' = not enrolled
}

// Open opens (creating it and its parent directory if needed) the SQLite
// database at path and applies the idempotent schema.
//
// Pragmas: WAL so a reader never blocks the writer, foreign_keys because the
// grant cascades rely on them (SQLite defaults them OFF), busy_timeout as a
// belt-and-braces wait, and synchronous=NORMAL — the safe pairing with WAL
// (a crash can cost the last commits, never the file; everything durable is
// in git anyway).
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	// _txlock=immediate: transactions take the write lock up front, so a
	// read-then-write transaction can never be overtaken between its read
	// and its write.
	db, err := sql.Open("sqlite", path+
		"?_pragma=journal_mode(WAL)"+
		"&_pragma=foreign_keys(1)"+
		"&_pragma=busy_timeout(5000)"+
		"&_pragma=synchronous(NORMAL)"+
		"&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	// One connection: SQLite takes a single writer anyway, and serializing
	// here removes SQLITE_BUSY as a failure mode entirely. Safe because no
	// query path holds open rows (or a transaction) across another query —
	// every store method drains and closes before returning.
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) exec(q string, args ...any) (sql.Result, error) { return s.db.Exec(q, args...) }
func (s *Store) query(q string, args ...any) (*sql.Rows, error) { return s.db.Query(q, args...) }
func (s *Store) queryRow(q string, args ...any) *sql.Row        { return s.db.QueryRow(q, args...) }

// ---------------------------------------------------------------- users

func (s *Store) UpsertUser(provider, subject, name, email string) (*User, error) {
	_, err := s.exec(`
		INSERT INTO users (provider, subject, name, email) VALUES (?, ?, ?, ?)
		ON CONFLICT(provider, subject) DO UPDATE SET name = excluded.name, email = excluded.email`,
		provider, subject, name, email)
	if err != nil {
		return nil, err
	}
	return s.userBy("provider = ? AND subject = ?", provider, subject)
}

func (s *Store) UserByID(id int64) (*User, error) {
	return s.userBy("id = ?", id)
}

func (s *Store) userBy(where string, args ...any) (*User, error) {
	u := &User{}
	err := s.queryRow("SELECT id, provider, subject, name, email, role FROM users WHERE "+where, args...).
		Scan(&u.ID, &u.Provider, &u.Subject, &u.Name, &u.Email, &u.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// ---------------------------------------------------------------- local users

func (s *Store) AddLocalUser(username, name, email, argonHash string) error {
	u, err := s.UpsertUser("local", username, name, email)
	if err != nil {
		return err
	}
	_, err = s.exec(`
		INSERT INTO local_users (user_id, username, argon2_hash) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET argon2_hash = excluded.argon2_hash`,
		u.ID, username, argonHash)
	return err
}

func (s *Store) LocalUserHash(username string) (userID int64, hash string, err error) {
	err = s.queryRow("SELECT user_id, argon2_hash FROM local_users WHERE username = ?", username).
		Scan(&userID, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", ErrNotFound
	}
	return userID, hash, err
}

// ---------------------------------------------------------------- sessions

func (s *Store) CreateSession(userID int64, ttl time.Duration) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw)
	now := time.Now().Unix()
	// opportunistic prune — idle-expired sessions are otherwise only
	// deleted when their cookie comes back
	_, _ = s.exec("DELETE FROM sessions WHERE expires_at < ?", now)
	_, err := s.exec("INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		id, userID, now, now+int64(ttl.Seconds()))
	return id, err
}

// SessionUser resolves a session to its user and slides the expiry.
func (s *Store) SessionUser(sessionID string, ttl time.Duration) (*User, error) {
	var userID int64
	var expiresAt int64
	err := s.queryRow("SELECT user_id, expires_at FROM sessions WHERE id = ?", sessionID).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if expiresAt < now {
		_, _ = s.exec("DELETE FROM sessions WHERE id = ?", sessionID)
		return nil, ErrNotFound
	}
	_, _ = s.exec("UPDATE sessions SET expires_at = ? WHERE id = ?", now+int64(ttl.Seconds()), sessionID)
	return s.UserByID(userID)
}

func (s *Store) DeleteSession(sessionID string) error {
	_, err := s.exec("DELETE FROM sessions WHERE id = ?", sessionID)
	return err
}

func (s *Store) DB() *sql.DB { return s.db }
