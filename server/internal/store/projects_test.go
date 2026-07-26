package store

import "testing"

func TestProjectAndSourceReconciliation(t *testing.T) {
	st := OpenTest(t)

	// config-managed projects reconcile; api-managed persist
	if err := st.SyncProjects([]Project{{ProjectID: "a", RepoID: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddProject(Project{ProjectID: "manual", RepoID: "manual", ContentRoot: "docs"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SyncProjects([]Project{{ProjectID: "b", RepoID: "b"}}); err != nil {
		t.Fatal(err)
	}
	ps, err := st.Projects()
	if err != nil || len(ps) != 2 {
		t.Fatalf("projects after resync: %v %+v", err, ps)
	}
	byID := map[string]Project{}
	for _, p := range ps {
		byID[p.ProjectID] = p
	}
	if _, gone := byID["a"]; gone {
		t.Fatal("config-managed project 'a' should have been reconciled away")
	}
	if m, ok := byID["manual"]; !ok || m.ManagedBy != "api" || m.ContentRoot != "docs" {
		t.Fatalf("api-managed project lost or mangled: %+v", byID)
	}

	// the source catalog reconciles the same way
	if err := st.SyncSources([]Source{{Name: "reg", Kind: "git", Remote: "r1", DefaultBranch: "main", SyncInterval: 300}}); err != nil {
		t.Fatal(err)
	}
	src, err := st.SourceByName("reg")
	if err != nil || src.Kind != "git" {
		t.Fatalf("SourceByName: %v %+v", err, src)
	}
	if all, err := st.Sources(); err != nil || len(all) != 1 || all[0].Name != "reg" {
		t.Fatalf("Sources: %v %+v", err, all)
	}
	// re-sync with an empty set removes config-managed catalog entries
	if err := st.SyncSources(nil); err != nil {
		t.Fatal(err)
	}
	if all, _ := st.Sources(); len(all) != 0 {
		t.Fatalf("catalog not reconciled: %+v", all)
	}
}
