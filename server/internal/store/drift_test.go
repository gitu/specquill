package store

import "testing"

func TestDriftRunLifecycle(t *testing.T) {
	s := OpenTest(t)
	id, err := s.CreateDriftRun(DriftRun{RepoKey: "r", Branch: "main", ScopeJSON: `["a.md"]`, DocsTotal: 1, HeadSHA: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDriftRunProgress(id, 1, 2, `["12:00:00 ✓ a.md — clean"]`); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishDriftRun(id, "ok", ""); err != nil {
		t.Fatal(err)
	}
	run, err := s.LatestDriftRun("r", "main")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "ok" || run.DocsDone != 1 || run.DroppedUnverified != 2 || run.FinishedAt == 0 {
		t.Fatalf("unexpected run: %+v", run)
	}
	if run.ActivityJSON != `["12:00:00 ✓ a.md — clean"]` {
		t.Fatalf("activity not persisted: %q", run.ActivityJSON)
	}
	if _, err := s.LatestDriftRun("r", "other"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMarkInterruptedDriftRuns(t *testing.T) {
	s := OpenTest(t)
	// a run that got 2 of 5 units done before the process died
	id, err := s.CreateDriftRun(DriftRun{RepoKey: "r", Branch: "main", DocsTotal: 5,
		ScopeJSON: `["a.md","b.md","c.md","d.md","e.md"]`})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDriftRunProgress(id, 2, 0, "[]"); err != nil {
		t.Fatal(err)
	}
	n, err := s.MarkInterruptedDriftRuns()
	if err != nil || n != 1 {
		t.Fatalf("want 1 interrupted, got %d (%v)", n, err)
	}
	run, _ := s.LatestDriftRun("r", "main")
	if run.Status != "interrupted" || run.Error == "" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if !run.Resumable() {
		t.Error("a run with units left must be resumable")
	}
	byID, err := s.DriftRunByID(id)
	if err != nil || byID.Status != "interrupted" {
		t.Fatalf("DriftRunByID = %+v (%v)", byID, err)
	}
}

func TestDriftRunResumable(t *testing.T) {
	for name, tc := range map[string]struct {
		run  DriftRun
		want bool
	}{
		"interrupted midway":  {DriftRun{Status: "interrupted", DocsTotal: 5, DocsDone: 2}, true},
		"cancelled midway":    {DriftRun{Status: "cancelled", DocsTotal: 5, DocsDone: 2}, true},
		"failed midway":       {DriftRun{Status: "error", DocsTotal: 5, DocsDone: 2}, true},
		"still running":       {DriftRun{Status: "running", DocsTotal: 5, DocsDone: 2}, false},
		"finished":            {DriftRun{Status: "ok", DocsTotal: 5, DocsDone: 5}, false},
		"cancelled at theend": {DriftRun{Status: "cancelled", DocsTotal: 5, DocsDone: 5}, false},
	} {
		if got := tc.run.Resumable(); got != tc.want {
			t.Errorf("%s: Resumable() = %v", name, got)
		}
	}
}

func TestDriftFindingLifecycleSurvivesRuns(t *testing.T) {
	s := OpenTest(t)
	f := DriftFinding{RepoKey: "r", Branch: "main", Fingerprint: "fp1", RunID: 1,
		DocPath: "specs/a.md", Anchor: "REQ-1", Source: "src", Kind: "contradiction",
		Severity: "high", Title: "first title", EvidenceJSON: "[]"}
	if err := s.UpsertDriftFinding(f); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDriftFindingStatus("r", "main", "fp1", "dismissed"); err != nil {
		t.Fatal(err)
	}
	// a later run re-reports the same fingerprint with a reworded title:
	// display refreshes, the dismissal sticks
	f.RunID, f.Title = 2, "reworded title"
	if err := s.UpsertDriftFinding(f); err != nil {
		t.Fatal(err)
	}
	got, err := s.DriftFinding("r", "main", "fp1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "dismissed" || got.Title != "reworded title" || got.RunID != 2 {
		t.Fatalf("unexpected finding: %+v", got)
	}
}

func TestResolveDriftFindingsScopeAware(t *testing.T) {
	s := OpenTest(t)
	for _, f := range []DriftFinding{
		{RepoKey: "r", Branch: "main", Fingerprint: "a1", RunID: 1, DocPath: "a.md"},
		{RepoKey: "r", Branch: "main", Fingerprint: "a2", RunID: 1, DocPath: "a.md"},
		{RepoKey: "r", Branch: "main", Fingerprint: "b1", RunID: 1, DocPath: "b.md"},
	} {
		if err := s.UpsertDriftFinding(f); err != nil {
			t.Fatal(err)
		}
	}
	// re-check of a.md keeps only a1 — b.md was out of scope and must survive
	if err := s.ResolveDriftFindingsExcept("r", "main", "a.md", []string{"a1"}, nil); err != nil {
		t.Fatal(err)
	}
	live, err := s.DriftFindings("r", "main")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range live {
		got[f.Fingerprint] = true
	}
	if !got["a1"] || got["a2"] || !got["b1"] {
		t.Fatalf("unexpected live findings: %v", got)
	}
	// the drift returns → the resolved fingerprint reopens
	if err := s.UpsertDriftFinding(DriftFinding{RepoKey: "r", Branch: "main", Fingerprint: "a2", RunID: 2, DocPath: "a.md"}); err != nil {
		t.Fatal(err)
	}
	live, _ = s.DriftFindings("r", "main")
	if len(live) != 3 {
		t.Fatalf("want a2 reopened (3 live), got %d", len(live))
	}
}

func TestGapFindingsReconcilePerSource(t *testing.T) {
	s := OpenTest(t)
	for _, f := range []DriftFinding{
		{RepoKey: "r", Branch: "main", Fingerprint: "g1", RunID: 1, Source: "api", Kind: "coverage-gap", SuggestedPath: "requirements/REQ-x.md"},
		{RepoKey: "r", Branch: "main", Fingerprint: "g2", RunID: 1, Source: "api", Kind: "coverage-gap"},
		{RepoKey: "r", Branch: "main", Fingerprint: "g3", RunID: 1, Source: "docs", Kind: "coverage-gap"},
		{RepoKey: "r", Branch: "main", Fingerprint: "d1", RunID: 1, Source: "api", DocPath: "a.md"},
	} {
		if err := s.UpsertDriftFinding(f); err != nil {
			t.Fatal(err)
		}
	}
	// a fresh sweep of `api` keeps only g1 — other sources' gaps and
	// doc-backed drift findings must survive
	if err := s.ResolveGapFindingsExcept("r", "main", "api", []string{"g1"}, []string{"coverage-gap"}); err != nil {
		t.Fatal(err)
	}
	live, err := s.DriftFindings("r", "main")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range live {
		got[f.Fingerprint] = true
	}
	if !got["g1"] || got["g2"] || !got["g3"] || !got["d1"] {
		t.Fatalf("unexpected live findings: %v", got)
	}
}

func TestSetDriftFindingDraft(t *testing.T) {
	s := OpenTest(t)
	if err := s.UpsertDriftFinding(DriftFinding{RepoKey: "r", Branch: "main", Fingerprint: "g1", RunID: 1, Kind: "coverage-gap"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDriftFindingDraft("r", "main", "g1", "requirements/REQ-x.md"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.DriftFinding("r", "main", "g1")
	if got.DraftPath != "requirements/REQ-x.md" {
		t.Fatalf("draft not recorded: %+v", got)
	}
	if err := s.SetDriftFindingDraft("r", "main", "nope", "x.md"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDriftRunMode(t *testing.T) {
	s := OpenTest(t)
	if _, err := s.CreateDriftRun(DriftRun{RepoKey: "r", Branch: "main", Mode: "gaps"}); err != nil {
		t.Fatal(err)
	}
	run, err := s.LatestDriftRun("r", "main")
	if err != nil || run.Mode != "gaps" {
		t.Fatalf("mode round-trip failed: %+v (%v)", run, err)
	}
}

func TestFileDriftFinding(t *testing.T) {
	s := OpenTest(t)
	if err := s.UpsertDriftFinding(DriftFinding{RepoKey: "r", Branch: "main", Fingerprint: "fp", RunID: 1, DocPath: "a.md"}); err != nil {
		t.Fatal(err)
	}
	if err := s.FileDriftFinding("r", "main", "fp", "https://x/issues/1", "this-repo"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.DriftFinding("r", "main", "fp")
	if got.Status != "filed" || got.WorkItemURL != "https://x/issues/1" || got.WorkItemTarget != "this-repo" {
		t.Fatalf("unexpected finding: %+v", got)
	}
	if err := s.FileDriftFinding("r", "main", "nope", "u", "t"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestListDriftRunsAndFindingCounts(t *testing.T) {
	s := OpenTest(t)
	older, err := s.CreateDriftRun(DriftRun{RepoKey: "r", Branch: "main", Mode: "drift",
		ScopeJSON: `["a.md"]`, DocsTotal: 1, SourcesJSON: `["reg"]`})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.CreateDriftRun(DriftRun{RepoKey: "r", Branch: "main", Mode: "gaps",
		DocsTotal: 1, Focus: "retention", SourcesJSON: `["reg"]`})
	if err != nil {
		t.Fatal(err)
	}
	// another branch's run must not leak into this branch's history
	if _, err := s.CreateDriftRun(DriftRun{RepoKey: "r", Branch: "ws/flo", Mode: "drift"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []DriftFinding{
		{RepoKey: "r", Branch: "main", Fingerprint: "a", RunID: older, DocPath: "a.md"},
		{RepoKey: "r", Branch: "main", Fingerprint: "b", RunID: newer},
		{RepoKey: "r", Branch: "main", Fingerprint: "c", RunID: newer},
	} {
		if err := s.UpsertDriftFinding(f); err != nil {
			t.Fatal(err)
		}
	}
	// a resolved finding is not what a past run is still worth
	if err := s.ResolveDriftFindingsExcept("r", "main", "a.md", nil, nil); err != nil {
		t.Fatal(err)
	}

	runs, err := s.ListDriftRuns("r", "main", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("history = %+v, want this branch's 2 runs", runs)
	}
	if runs[0].ID != newer || runs[1].ID != older {
		t.Errorf("history must be newest first: %d then %d", runs[0].ID, runs[1].ID)
	}
	if runs[0].Mode != "gaps" || runs[0].Focus != "retention" || runs[0].SourcesJSON != `["reg"]` {
		t.Errorf("row lost the run's shape: %+v", runs[0])
	}

	counts, err := s.DriftFindingCountsByRun("r", "main")
	if err != nil {
		t.Fatal(err)
	}
	if counts[newer] != 2 || counts[older] != 0 {
		t.Errorf("counts = %v, want 2 live for run %d and none for the resolved one", counts, newer)
	}
}

// Two recipes may audit the same document or source. Reconciliation is
// limited to the KINDS the running recipe declares, so a custom pipeline's
// findings survive a built-in run over the same unit — and vice versa.
func TestReconciliationLeavesOtherRecipesAlone(t *testing.T) {
	s := OpenTest(t)
	for _, f := range []DriftFinding{
		{RepoKey: "r", Branch: "main", Fingerprint: "d1", RunID: 1, DocPath: "a.md", Kind: "contradiction"},
		{RepoKey: "r", Branch: "main", Fingerprint: "m1", RunID: 2, DocPath: "a.md", Kind: "model-gap"},
		{RepoKey: "r", Branch: "main", Fingerprint: "g1", RunID: 1, Source: "api", Kind: "coverage-gap"},
		{RepoKey: "r", Branch: "main", Fingerprint: "x1", RunID: 2, Source: "api", Kind: "unstated-deadline"},
	} {
		if err := s.UpsertDriftFinding(f); err != nil {
			t.Fatal(err)
		}
	}
	// a drift run over a.md reports nothing: it resolves ITS kind only
	if err := s.ResolveDriftFindingsExcept("r", "main", "a.md", nil,
		[]string{"contradiction", "new-requirement"}); err != nil {
		t.Fatal(err)
	}
	// a gaps sweep of ~api likewise
	if err := s.ResolveGapFindingsExcept("r", "main", "api", nil, []string{"coverage-gap"}); err != nil {
		t.Fatal(err)
	}
	live, err := s.DriftFindings("r", "main")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range live {
		got[f.Fingerprint] = true
	}
	if got["d1"] || got["g1"] {
		t.Errorf("the running recipe's own stale findings should resolve: %v", got)
	}
	if !got["m1"] || !got["x1"] {
		t.Errorf("another recipe's findings must survive: %v", got)
	}
}

// A finding about a document that has been DELETED can never be reconciled the
// normal way — that only happens when a run re-checks the document, and a
// deleted one is never in scope again. It would otherwise sit on the page
// forever, pointing at nothing, with actions that quietly do nothing.
func TestOrphanedFindingsRetire(t *testing.T) {
	s := OpenTest(t)
	for _, f := range []DriftFinding{
		{RepoKey: "r", Branch: "main", Fingerprint: "live", RunID: 1, DocPath: "specs/a.md"},
		{RepoKey: "r", Branch: "main", Fingerprint: "gone", RunID: 1, DocPath: "specs/deleted.md"},
		{RepoKey: "r", Branch: "main", Fingerprint: "gap", RunID: 1, Source: "api", Kind: "coverage-gap"},
		{RepoKey: "r", Branch: "other", Fingerprint: "elsewhere", RunID: 1, DocPath: "specs/deleted.md"},
	} {
		if err := s.UpsertDriftFinding(f); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.ResolveOrphanedDriftFindings("r", "main", []string{"specs/a.md"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("retired %d, want 1", n)
	}
	live, _ := s.DriftFindings("r", "main")
	got := map[string]bool{}
	for _, f := range live {
		got[f.Fingerprint] = true
	}
	if got["gone"] {
		t.Error("a finding about a deleted document should retire")
	}
	if !got["live"] {
		t.Error("a finding about a live document must survive")
	}
	// source-anchored findings have no document to lose
	if !got["gap"] {
		t.Error("coverage gaps carry no doc_path and must never be retired this way")
	}
	// another branch's findings are its own business
	other, _ := s.DriftFindings("r", "other")
	if len(other) != 1 {
		t.Errorf("another branch was touched: %v", other)
	}
}

// An empty document set means the branch has no documents at all — every
// doc-backed finding is orphaned, and the query must not degenerate into a
// no-op WHERE clause.
func TestOrphanedFindingsWithNoDocumentsLeft(t *testing.T) {
	s := OpenTest(t)
	if err := s.UpsertDriftFinding(DriftFinding{
		RepoKey: "r", Branch: "main", Fingerprint: "x", RunID: 1, DocPath: "specs/a.md"}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ResolveOrphanedDriftFindings("r", "main", nil); err != nil || n != 1 {
		t.Fatalf("retired %d (err %v), want 1", n, err)
	}
}
