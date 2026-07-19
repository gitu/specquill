// Package store wraps the Postgres database holding users, sessions and PR
// review metadata. Workspace content never lands here — it stays in git.
package store

import (
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"specquill/server/internal/secrets"
)

//go:embed schema.sql
var schema string

var ErrNotFound = errors.New("not found")

type Store struct {
	db     *sql.DB
	sealer *secrets.Sealer // encrypts tenant_credentials; nil = credential store off
}

type User struct {
	ID       int64  `json:"id"`
	Provider string `json:"provider"`
	Subject  string `json:"-"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

// Open connects to Postgres (any pgx-parseable DSN/URL, e.g. a Neon URL)
// and applies the idempotent schema.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	// serverless Postgres (Neon) closes idle conns; don't hold them forever
	db.SetConnMaxIdleTime(5 * time.Minute)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// rebind rewrites `?` placeholders to Postgres $N so query text stays terse.
// None of our SQL carries `?` inside literals.
func rebind(q string) string {
	if !strings.ContainsRune(q, '?') {
		return q
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		} else {
			b.WriteByte(q[i])
		}
	}
	return b.String()
}

func (s *Store) exec(q string, args ...any) (sql.Result, error) { return s.db.Exec(rebind(q), args...) }
func (s *Store) query(q string, args ...any) (*sql.Rows, error) {
	return s.db.Query(rebind(q), args...)
}
func (s *Store) queryRow(q string, args ...any) *sql.Row { return s.db.QueryRow(rebind(q), args...) }

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
	err := s.queryRow("SELECT id, provider, subject, name, email FROM users WHERE "+where, args...).
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
