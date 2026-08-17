package store

import (
	"path/filepath"
	"testing"
)

// OpenTest returns a Store on a throwaway SQLite database under the test's
// temp dir and registers its teardown. No external service, so — unlike the
// Postgres era — store tests never skip.
func OpenTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "specquill.db"))
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
