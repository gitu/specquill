package store

import "testing"

func TestDriftRunLifecycle(t *testing.T) {
	s := OpenTest(t)
	id, err := s.CreateDriftRun(DriftRun{RepoKey: "r", Branch: "main", ScopeJSON: `["a.md"]`, DocsTotal: 1, HeadSHA: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateDriftRunProgress(id, 1, 2); err != nil {
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
	if _, err := s.LatestDriftRun("r", "other"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMarkInterruptedDriftRuns(t *testing.T) {
	s := OpenTest(t)
	if _, err := s.CreateDriftRun(DriftRun{RepoKey: "r", Branch: "main"}); err != nil {
		t.Fatal(err)
	}
	n, err := s.MarkInterruptedDriftRuns()
	if err != nil || n != 1 {
		t.Fatalf("want 1 interrupted, got %d (%v)", n, err)
	}
	run, _ := s.LatestDriftRun("r", "main")
	if run.Status != "error" || run.Error != "interrupted" {
		t.Fatalf("unexpected run: %+v", run)
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
	if err := s.ResolveDriftFindingsExcept("r", "main", "a.md", []string{"a1"}); err != nil {
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
	if err := s.ResolveGapFindingsExcept("r", "main", "api", []string{"g1"}); err != nil {
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
