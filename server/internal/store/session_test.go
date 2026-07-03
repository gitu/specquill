package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionIdleExpiry(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	u, err := st.UpsertUser("local", "jane", "Jane", "jane@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// expired session (idle past the TTL) resolves to not-found and is deleted
	id, err := st.CreateSession(u.ID, -1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionUser(id, 10*time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session resolved: %v", err)
	}
	var n int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", id).Scan(&n); err != nil || n != 0 {
		t.Fatalf("expired session row not deleted (n=%d, err=%v)", n, err)
	}

	// live session slides: each resolve pushes expires_at to now+ttl
	id, err = st.CreateSession(u.ID, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var before int64
	_ = st.db.QueryRow("SELECT expires_at FROM sessions WHERE id = ?", id).Scan(&before)
	if _, err := st.SessionUser(id, time.Hour); err != nil {
		t.Fatal(err)
	}
	var after int64
	_ = st.db.QueryRow("SELECT expires_at FROM sessions WHERE id = ?", id).Scan(&after)
	if after <= before {
		t.Fatalf("expiry did not slide: before=%d after=%d", before, after)
	}

	// creating a session prunes other idle-expired rows
	stale, _ := st.CreateSession(u.ID, -1*time.Second)
	if _, err := st.CreateSession(u.ID, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", stale).Scan(&n); err != nil || n != 0 {
		t.Fatalf("stale session not pruned (n=%d, err=%v)", n, err)
	}
}
