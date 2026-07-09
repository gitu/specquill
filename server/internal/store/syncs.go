package store

import (
	"database/sql"
	"errors"
	"time"
)

// SourceSync is the last-import result for a non-git (importer) source.
type SourceSync struct {
	TenantID  int64
	Name      string
	Status    string // ok | error
	Error     string
	FileCount int
	HeadSHA   string
	SyncedAt  int64 // unix seconds
}

// RecordSourceSync upserts the latest import result for a source.
func (s *Store) RecordSourceSync(rec SourceSync) error {
	_, err := s.exec(`INSERT INTO source_syncs (tenant_id, name, status, error, file_count, head_sha, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, name) DO UPDATE SET
		  status = excluded.status, error = excluded.error, file_count = excluded.file_count,
		  head_sha = excluded.head_sha, synced_at = excluded.synced_at`,
		rec.TenantID, rec.Name, rec.Status, rec.Error, rec.FileCount, rec.HeadSHA, time.Now().Unix())
	return err
}

// SourceSyncStatus returns the last import result for one source, or ErrNotFound
// if it was never synced.
func (s *Store) SourceSyncStatus(tenantID int64, name string) (*SourceSync, error) {
	rec := &SourceSync{}
	err := s.queryRow(`SELECT tenant_id, name, status, error, file_count, head_sha, synced_at
		FROM source_syncs WHERE tenant_id = ? AND name = ?`, tenantID, name).
		Scan(&rec.TenantID, &rec.Name, &rec.Status, &rec.Error, &rec.FileCount, &rec.HeadSHA, &rec.SyncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rec, err
}

// TenantSourceSyncs returns every recorded import result for a tenant, keyed by
// source name.
func (s *Store) TenantSourceSyncs(tenantID int64) (map[string]SourceSync, error) {
	rows, err := s.query(`SELECT tenant_id, name, status, error, file_count, head_sha, synced_at
		FROM source_syncs WHERE tenant_id = ?`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]SourceSync{}
	for rows.Next() {
		var r SourceSync
		if err := rows.Scan(&r.TenantID, &r.Name, &r.Status, &r.Error, &r.FileCount, &r.HeadSHA, &r.SyncedAt); err != nil {
			return nil, err
		}
		out[r.Name] = r
	}
	return out, rows.Err()
}
