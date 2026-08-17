package store

import (
	"database/sql"
	"errors"
	"time"
)

// SourceSync is the last-import result for a non-git (importer) source.
type SourceSync struct {
	Name      string
	Status    string // ok | error
	Error     string
	FileCount int
	HeadSHA   string
	SyncedAt  int64 // unix seconds
}

// RecordSourceSync upserts the latest import result for a source.
func (s *Store) RecordSourceSync(rec SourceSync) error {
	_, err := s.exec(`INSERT INTO source_syncs (name, status, error, file_count, head_sha, synced_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
		  status = excluded.status, error = excluded.error, file_count = excluded.file_count,
		  head_sha = excluded.head_sha, synced_at = excluded.synced_at`,
		rec.Name, rec.Status, rec.Error, rec.FileCount, rec.HeadSHA, time.Now().Unix())
	return err
}

// SourceSyncStatus returns the last import result for one source, or ErrNotFound
// if it was never synced.
func (s *Store) SourceSyncStatus(name string) (*SourceSync, error) {
	rec := &SourceSync{}
	err := s.queryRow(`SELECT name, status, error, file_count, head_sha, synced_at
		FROM source_syncs WHERE name = ?`, name).
		Scan(&rec.Name, &rec.Status, &rec.Error, &rec.FileCount, &rec.HeadSHA, &rec.SyncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

// SourceSyncs returns every recorded import result, keyed by source name.
func (s *Store) SourceSyncs() (map[string]SourceSync, error) {
	rows, err := s.query(`SELECT name, status, error, file_count, head_sha, synced_at FROM source_syncs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]SourceSync{}
	for rows.Next() {
		var r SourceSync
		if err := rows.Scan(&r.Name, &r.Status, &r.Error, &r.FileCount, &r.HeadSHA, &r.SyncedAt); err != nil {
			return nil, err
		}
		out[r.Name] = r
	}
	return out, rows.Err()
}
