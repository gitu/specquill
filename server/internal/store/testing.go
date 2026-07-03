package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// OpenTest returns a Store on an isolated, throwaway Postgres schema and
// registers its teardown. Tests are skipped when no Postgres is reachable —
// set TEST_DATABASE_URL, or start the dev one:
//
//	docker compose -f docker-compose.dev.yml up -d postgres
func OpenTest(t *testing.T) *Store {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		base = "postgres://reqbase:reqbase@127.0.0.1:5433/reqbase?sslmode=disable"
	}
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	if err := admin.Ping(); err != nil {
		admin.Close()
		t.Skipf("no test Postgres at %s (%v) — set TEST_DATABASE_URL or `docker compose -f docker-compose.dev.yml up -d postgres`", base, err)
	}
	raw := make([]byte, 6)
	_, _ = rand.Read(raw)
	schemaName := "t_" + hex.EncodeToString(raw)
	if _, err := admin.Exec("CREATE SCHEMA " + schemaName); err != nil {
		admin.Close()
		t.Fatalf("create test schema: %v", err)
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	// pgx passes unknown URL params (search_path) as server runtime params
	st, err := Open(base + sep + "search_path=" + schemaName)
	if err != nil {
		admin.Close()
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() {
		st.Close()
		_, _ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE")
		admin.Close()
	})
	return st
}
