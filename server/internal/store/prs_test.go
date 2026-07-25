package store

import "testing"

// PR + comment writes exercise the SQL that is most dialect-sensitive:
// RETURNING id, NULLIF on optional columns, the per-repo MAX(number)+1
// assignment, and scanning a stored boolean back into a Go bool.
func TestPRLifecycleAndComments(t *testing.T) {
	st := OpenTest(t)
	author, err := st.UpsertUser("local", "flo", "Flo Test", "flo@test.local")
	if err != nil {
		t.Fatal(err)
	}

	// numbers are assigned per repo, starting at 1
	pr, err := st.CreatePR("w", "First", "body", "ws/flo", "main", author.ID)
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if pr.ID == 0 || pr.Number != 1 || pr.State != "open" {
		t.Fatalf("first PR: %+v", pr)
	}
	second, err := st.CreatePR("w", "Second", "", "ws/other", "main", author.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Number != 2 || second.ID == pr.ID {
		t.Fatalf("second PR: %+v", second)
	}
	// a different repo restarts numbering
	other, err := st.CreatePR("docs", "Elsewhere", "", "ws/flo", "main", author.ID)
	if err != nil {
		t.Fatal(err)
	}
	if other.Number != 1 {
		t.Fatalf("per-repo numbering: %+v", other)
	}

	if got, err := st.PRByNumber("w", 1); err != nil || got.ID != pr.ID || got.Title != "First" {
		t.Fatalf("PRByNumber: %v %+v", err, got)
	}
	if open, err := st.ListPRs("w", "open"); err != nil || len(open) != 2 {
		t.Fatalf("ListPRs open: %v %+v", err, open)
	}

	// anchored comment keeps its file/line; a general comment stores NULLs
	// that read back as zero values
	if _, err := st.AddComment(pr.ID, author.ID, "specs/a.md", 12, "deadbeef", "anchored"); err != nil {
		t.Fatalf("AddComment anchored: %v", err)
	}
	if _, err := st.AddComment(pr.ID, author.ID, "", 0, "", "general"); err != nil {
		t.Fatalf("AddComment general: %v", err)
	}
	cs, err := st.Comments(pr.ID)
	if err != nil || len(cs) != 2 {
		t.Fatalf("Comments: %v %+v", err, cs)
	}
	byBody := map[string]PRComment{}
	for _, c := range cs {
		byBody[c.Body] = c
	}
	if a := byBody["anchored"]; a.FilePath != "specs/a.md" || a.Line != 12 || a.AnchoredCommit != "deadbeef" || a.Resolved {
		t.Fatalf("anchored comment: %+v", a)
	}
	if g := byBody["general"]; g.FilePath != "" || g.Line != 0 || g.AnchoredCommit != "" {
		t.Fatalf("general comment should read back empty: %+v", g)
	}
	if byBody["anchored"].Author.Email != "flo@test.local" {
		t.Fatalf("comment author not joined: %+v", byBody["anchored"].Author)
	}

	// merging records the commit; an empty commit stays NULL
	if err := st.SetPRState(pr.ID, "merged", "cafebabe"); err != nil {
		t.Fatal(err)
	}
	merged, err := st.PRByNumber("w", 1)
	if err != nil || merged.State != "merged" || merged.MergedCommit != "cafebabe" || merged.MergedAt == 0 {
		t.Fatalf("merged PR: %v %+v", err, merged)
	}
	if open, _ := st.ListPRs("w", "open"); len(open) != 1 {
		t.Fatalf("merged PR still listed as open: %+v", open)
	}

	// approvals are keyed per (pr, user) — re-approving re-pins the commit
	if err := st.Approve(second.ID, author.ID, "sha1"); err != nil {
		t.Fatal(err)
	}
	if err := st.Approve(second.ID, author.ID, "sha2"); err != nil {
		t.Fatal(err)
	}
	aps, err := st.Approvals(second.ID)
	if err != nil || len(aps) != 1 || aps[0].CommitSha != "sha2" {
		t.Fatalf("Approvals: %v %+v", err, aps)
	}
}
