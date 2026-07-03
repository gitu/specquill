// Package store wraps the SQLite database holding users, sessions and PR
// review metadata. Workspace content never lands here — it stays in git.
package store

import (
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
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
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc sqlite: single writer keeps it simple and safe
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// ---------------------------------------------------------------- users

func (s *Store) UpsertUser(provider, subject, name, email string) (*User, error) {
	_, err := s.db.Exec(`
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
	err := s.db.QueryRow("SELECT id, provider, subject, name, email FROM users WHERE "+where, args...).
		Scan(&u.ID, &u.Provider, &u.Subject, &u.Name, &u.Email)
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
	_, err = s.db.Exec(`
		INSERT INTO local_users (user_id, username, argon2_hash) VALUES (?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET argon2_hash = excluded.argon2_hash`,
		u.ID, username, argonHash)
	return err
}

func (s *Store) LocalUserHash(username string) (userID int64, hash string, err error) {
	err = s.db.QueryRow("SELECT user_id, argon2_hash FROM local_users WHERE username = ?", username).
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
	_, _ = s.db.Exec("DELETE FROM sessions WHERE expires_at < ?", now)
	_, err := s.db.Exec("INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		id, userID, now, now+int64(ttl.Seconds()))
	return id, err
}

// SessionUser resolves a session to its user and slides the expiry.
func (s *Store) SessionUser(sessionID string, ttl time.Duration) (*User, error) {
	var userID int64
	var expiresAt int64
	err := s.db.QueryRow("SELECT user_id, expires_at FROM sessions WHERE id = ?", sessionID).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if expiresAt < now {
		_, _ = s.db.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
		return nil, ErrNotFound
	}
	_, _ = s.db.Exec("UPDATE sessions SET expires_at = ? WHERE id = ?", now+int64(ttl.Seconds()), sessionID)
	return s.UserByID(userID)
}

func (s *Store) DeleteSession(sessionID string) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	return err
}

func (s *Store) DB() *sql.DB { return s.db }
