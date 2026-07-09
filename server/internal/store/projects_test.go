package store

import "testing"

func TestProjectAndSourceReconciliation(t *testing.T) {
	st := OpenTest(t)
	ten, err := st.EnsureTenant("default", "config", 0, "Workspace")
	if err != nil {
		t.Fatal(err)
	}

	// config-managed projects reconcile; api-managed persist
	if err := st.SyncTenantProjects(ten.ID, []Project{{ProjectID: "a", RepoID: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddProject(Project{TenantID: ten.ID, ProjectID: "manual", RepoID: "manual", ContentRoot: "docs"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SyncTenantProjects(ten.ID, []Project{{ProjectID: "b", RepoID: "b"}}); err != nil {
		t.Fatal(err)
	}
	ps, err := st.TenantProjects(ten.ID)
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

	// global sources reconcile the same way; grants sync + lookup
	if err := st.SyncGlobalSources([]Source{{Name: "reg", Kind: "git", Remote: "r1", DefaultBranch: "main", SyncInterval: 300}}); err != nil {
		t.Fatal(err)
	}
	src, err := st.SourceByName(ten.ID, "reg")
	if err != nil || src.Kind != "git" {
		t.Fatalf("SourceByName: %v %+v", err, src)
	}
	if err := st.SyncGrants(ten.ID, []int64{src.ID}); err != nil {
		t.Fatal(err)
	}
	granted, err := st.TenantGrantedSources(ten.ID)
	if err != nil || len(granted) != 1 || granted[0].Name != "reg" {
		t.Fatalf("granted: %v %+v", err, granted)
	}
	// re-sync with empty set revokes config-managed grants
	if err := st.SyncGrants(ten.ID, nil); err != nil {
		t.Fatal(err)
	}
	if granted, _ = st.TenantGrantedSources(ten.ID); len(granted) != 0 {
		t.Fatalf("grants not revoked: %+v", granted)
	}
}
