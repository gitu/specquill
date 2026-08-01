package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// DriftRun is one source-drift check over a frozen doc scope.
type DriftRun struct {
	ID                int64
	RepoKey           string
	Branch            string
	Mode              string // drift (per-doc verify) | gaps (per-source coverage)
	Status            string // running | ok | error | cancelled
	Error             string
	ScopeJSON         string // resolved doc list, frozen at start
	DocsTotal         int
	DocsDone          int
	DroppedUnverified int
	HeadSHA           string
	ActivityJSON      string // per-unit progress lines, live feedback + report material
	ReportPath        string // in-repo report doc this run maintains ('' = none)
	ReportBranch      string
	ExtractionsJSON   string // persisted application inventories [{source,path}]
	StartedAt         int64
	FinishedAt        int64
}

// DriftFinding is one verified divergence between a document and a source.
type DriftFinding struct {
	RepoKey        string
	Branch         string
	Fingerprint    string
	RunID          int64
	DocPath        string // '' for coverage gaps (nothing covers them yet)
	SuggestedPath  string // gaps: where the missing document should live
	DraftPath      string // gaps: the reverse-engineered draft, once created
	RemedyPath     string // the change/work-item doc created to remedy the finding
	RemedyKind     string // change | work_item
	DocumentsJSON  string // every document created for this finding [{kind,path}]
	Anchor         string
	Source         string
	Kind           string
	Severity       string
	Title          string
	Detail         string
	EvidenceJSON   string
	Status         string // open | dismissed | filed
	WorkItemURL    string
	WorkItemTarget string
	CreatedAt      int64
	UpdatedAt      int64
	ResolvedAt     int64
}

// CreateDriftRun inserts a run in `running` state and returns its id.
func (s *Store) CreateDriftRun(run DriftRun) (int64, error) {
	if run.Mode == "" {
		run.Mode = "drift"
	}
	res, err := s.exec(`INSERT INTO drift_runs
		(repo_key, branch, mode, status, scope_json, docs_total, head_sha,
		 report_path, report_branch, started_at)
		VALUES (?, ?, ?, 'running', ?, ?, ?, ?, ?, ?)`,
		run.RepoKey, run.Branch, run.Mode, run.ScopeJSON, run.DocsTotal, run.HeadSHA,
		run.ReportPath, run.ReportBranch, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateDriftRunProgress bumps the per-unit counters and the live activity
// feed of a running drift run.
func (s *Store) UpdateDriftRunProgress(id int64, docsDone, dropped int, activityJSON string) error {
	_, err := s.exec(`UPDATE drift_runs SET docs_done = ?, dropped_unverified = ?, activity_json = ? WHERE id = ?`,
		docsDone, dropped, activityJSON, id)
	return err
}

// SetDriftRunExtractions records the application inventories a run persisted.
func (s *Store) SetDriftRunExtractions(id int64, extractionsJSON string) error {
	_, err := s.exec(`UPDATE drift_runs SET extractions_json = ? WHERE id = ?`, extractionsJSON, id)
	return err
}

// FinishDriftRun records the terminal state of a run.
func (s *Store) FinishDriftRun(id int64, status, errMsg string) error {
	_, err := s.exec(`UPDATE drift_runs SET status = ?, error = ?, finished_at = ? WHERE id = ?`,
		status, errMsg, time.Now().Unix(), id)
	return err
}

// LatestDriftRun returns the most recent run for a repo+branch, or ErrNotFound.
func (s *Store) LatestDriftRun(repoKey, branch string) (*DriftRun, error) {
	r := &DriftRun{}
	err := s.queryRow(`SELECT id, repo_key, branch, mode, status, error, scope_json, docs_total,
			docs_done, dropped_unverified, head_sha, activity_json, report_path, report_branch,
			extractions_json, started_at, finished_at
		FROM drift_runs WHERE repo_key = ? AND branch = ? ORDER BY id DESC LIMIT 1`,
		repoKey, branch).
		Scan(&r.ID, &r.RepoKey, &r.Branch, &r.Mode, &r.Status, &r.Error, &r.ScopeJSON, &r.DocsTotal,
			&r.DocsDone, &r.DroppedUnverified, &r.HeadSHA, &r.ActivityJSON, &r.ReportPath,
			&r.ReportBranch, &r.ExtractionsJSON, &r.StartedAt, &r.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// MarkInterruptedDriftRuns fails every `running` run at boot: their worker
// goroutines died with the previous process, re-running the scope is the
// resume mechanism.
func (s *Store) MarkInterruptedDriftRuns() (int64, error) {
	res, err := s.exec(`UPDATE drift_runs SET status = 'error', error = 'interrupted',
		finished_at = ? WHERE status = 'running'`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// UpsertDriftFinding inserts a finding or refreshes an existing fingerprint's
// display fields. Lifecycle state survives re-runs on purpose: a dismissed
// finding stays dismissed, a filed one keeps its work item — and a previously
// resolved fingerprint that drifts again reopens (resolved_at cleared).
func (s *Store) UpsertDriftFinding(f DriftFinding) error {
	now := time.Now().Unix()
	_, err := s.exec(`INSERT INTO drift_findings
		(repo_key, branch, fingerprint, run_id, doc_path, suggested_path, anchor, source,
		 kind, severity, title, detail, evidence_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'open', ?, ?)
		ON CONFLICT(repo_key, branch, fingerprint) DO UPDATE SET
		  run_id = excluded.run_id, severity = excluded.severity, title = excluded.title,
		  detail = excluded.detail, evidence_json = excluded.evidence_json,
		  suggested_path = excluded.suggested_path,
		  updated_at = excluded.updated_at, resolved_at = 0`,
		f.RepoKey, f.Branch, f.Fingerprint, f.RunID, f.DocPath, f.SuggestedPath, f.Anchor,
		f.Source, f.Kind, f.Severity, f.Title, f.Detail, f.EvidenceJSON, now, now)
	return err
}

// ResolveDriftFindingsExcept marks a doc's findings resolved when the fresh
// check no longer reports their fingerprint. Scope-aware reconciliation: it
// touches ONE doc, so scoped runs never clear findings they did not re-check.
func (s *Store) ResolveDriftFindingsExcept(repoKey, branch, docPath string, keep []string) error {
	args := []any{time.Now().Unix(), repoKey, branch, docPath}
	q := `UPDATE drift_findings SET resolved_at = ?
		WHERE repo_key = ? AND branch = ? AND doc_path = ? AND resolved_at = 0`
	if len(keep) > 0 {
		q += ` AND fingerprint NOT IN (?` + strings.Repeat(",?", len(keep)-1) + `)`
		for _, fp := range keep {
			args = append(args, fp)
		}
	}
	_, err := s.exec(q, args...)
	return err
}

// ResolveGapFindingsExcept is the gaps-mode counterpart of
// ResolveDriftFindingsExcept: coverage gaps have no doc_path, so a fresh
// sweep of ONE source resolves that source's stale gaps only.
func (s *Store) ResolveGapFindingsExcept(repoKey, branch, source string, keep []string) error {
	args := []any{time.Now().Unix(), repoKey, branch, source}
	q := `UPDATE drift_findings SET resolved_at = ?
		WHERE repo_key = ? AND branch = ? AND doc_path = '' AND source = ? AND resolved_at = 0`
	if len(keep) > 0 {
		q += ` AND fingerprint NOT IN (?` + strings.Repeat(",?", len(keep)-1) + `)`
		for _, fp := range keep {
			args = append(args, fp)
		}
	}
	_, err := s.exec(q, args...)
	return err
}

// SetDriftFindingDraft records the reverse-engineered draft document created
// for a coverage gap.
func (s *Store) SetDriftFindingDraft(repoKey, branch, fingerprint, draftPath string) error {
	res, err := s.exec(`UPDATE drift_findings SET draft_path = ?, updated_at = ?
		WHERE repo_key = ? AND branch = ? AND fingerprint = ?`,
		draftPath, time.Now().Unix(), repoKey, branch, fingerprint)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDriftFindingRemedy records the in-repo change/work-item document
// created to remedy a finding.
func (s *Store) SetDriftFindingRemedy(repoKey, branch, fingerprint, path, kind string) error {
	res, err := s.exec(`UPDATE drift_findings SET remedy_path = ?, remedy_kind = ?, updated_at = ?
		WHERE repo_key = ? AND branch = ? AND fingerprint = ?`,
		path, kind, time.Now().Unix(), repoKey, branch, fingerprint)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDriftFindingDocuments records every document created for a finding.
func (s *Store) SetDriftFindingDocuments(repoKey, branch, fingerprint, documentsJSON string) error {
	res, err := s.exec(`UPDATE drift_findings SET documents_json = ?, updated_at = ?
		WHERE repo_key = ? AND branch = ? AND fingerprint = ?`,
		documentsJSON, time.Now().Unix(), repoKey, branch, fingerprint)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DriftFindings returns the live (unresolved) findings for a repo+branch.
func (s *Store) DriftFindings(repoKey, branch string) ([]DriftFinding, error) {
	rows, err := s.query(`SELECT repo_key, branch, fingerprint, run_id, doc_path, suggested_path,
			draft_path, remedy_path, remedy_kind, documents_json, anchor, source, kind, severity,
			title, detail, evidence_json, status, work_item_url, work_item_target,
			created_at, updated_at, resolved_at
		FROM drift_findings
		WHERE repo_key = ? AND branch = ? AND resolved_at = 0
		ORDER BY doc_path, anchor, source, kind`, repoKey, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DriftFinding
	for rows.Next() {
		var f DriftFinding
		if err := rows.Scan(&f.RepoKey, &f.Branch, &f.Fingerprint, &f.RunID, &f.DocPath,
			&f.SuggestedPath, &f.DraftPath, &f.RemedyPath, &f.RemedyKind, &f.DocumentsJSON,
			&f.Anchor, &f.Source, &f.Kind, &f.Severity, &f.Title, &f.Detail, &f.EvidenceJSON,
			&f.Status, &f.WorkItemURL, &f.WorkItemTarget, &f.CreatedAt, &f.UpdatedAt,
			&f.ResolvedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DriftFinding returns one finding by fingerprint, or ErrNotFound.
func (s *Store) DriftFinding(repoKey, branch, fingerprint string) (*DriftFinding, error) {
	f := &DriftFinding{}
	err := s.queryRow(`SELECT repo_key, branch, fingerprint, run_id, doc_path, suggested_path,
			draft_path, remedy_path, remedy_kind, documents_json, anchor, source, kind, severity,
			title, detail, evidence_json, status, work_item_url, work_item_target,
			created_at, updated_at, resolved_at
		FROM drift_findings WHERE repo_key = ? AND branch = ? AND fingerprint = ?`,
		repoKey, branch, fingerprint).
		Scan(&f.RepoKey, &f.Branch, &f.Fingerprint, &f.RunID, &f.DocPath, &f.SuggestedPath,
			&f.DraftPath, &f.RemedyPath, &f.RemedyKind, &f.DocumentsJSON, &f.Anchor, &f.Source,
			&f.Kind, &f.Severity, &f.Title, &f.Detail, &f.EvidenceJSON, &f.Status, &f.WorkItemURL,
			&f.WorkItemTarget, &f.CreatedAt, &f.UpdatedAt, &f.ResolvedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return f, err
}

// SetDriftFindingStatus flips a finding's lifecycle status (dismiss/reopen).
func (s *Store) SetDriftFindingStatus(repoKey, branch, fingerprint, status string) error {
	res, err := s.exec(`UPDATE drift_findings SET status = ?, updated_at = ?
		WHERE repo_key = ? AND branch = ? AND fingerprint = ?`,
		status, time.Now().Unix(), repoKey, branch, fingerprint)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// FileDriftFinding records the work item a finding was filed as.
func (s *Store) FileDriftFinding(repoKey, branch, fingerprint, url, target string) error {
	res, err := s.exec(`UPDATE drift_findings SET status = 'filed', work_item_url = ?,
		work_item_target = ?, updated_at = ?
		WHERE repo_key = ? AND branch = ? AND fingerprint = ?`,
		url, target, time.Now().Unix(), repoKey, branch, fingerprint)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
