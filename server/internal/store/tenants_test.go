package store

import "testing"

func TestTenantRegistry(t *testing.T) {
	st := OpenTest(t)

	// upsert by slug is idempotent and updates metadata
	a, err := st.EnsureTenant("acme", "github", 42, "Acme")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.EnsureTenant("acme", "github", 42, "Acme Corp")
	if err != nil || b.ID != a.ID || b.DisplayName != "Acme Corp" {
		t.Fatalf("EnsureTenant not idempotent: %v %+v vs %+v", err, a, b)
	}

	// repo sync converges to exactly the given set
	if err := st.SyncTenantRepos(a.ID, []TenantRepo{
		{RepoID: "specs", Mode: "writable", Remote: "r1", DefaultBranch: "main"},
		{RepoID: "regs", Mode: "readonly", Remote: "r2", DefaultBranch: "main"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SyncTenantRepos(a.ID, []TenantRepo{
		{RepoID: "specs", Mode: "writable", Remote: "r1-moved", DefaultBranch: "main"},
	}); err != nil {
		t.Fatal(err)
	}
	repos, err := st.TenantRepos(a.ID)
	if err != nil || len(repos) != 1 || repos[0].RepoID != "specs" || repos[0].Remote != "r1-moved" {
		t.Fatalf("sync did not converge: %v %+v", err, repos)
	}

	// membership: EnsureMember keeps an existing role; SetMemberRole changes it
	u, err := st.UpsertUser("github", "99", "Jane", "jane@acme.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureMember(a.ID, u.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureMember(a.ID, u.ID, "viewer"); err != nil { // re-sync must not downgrade
		t.Fatal(err)
	}
	if role, _ := st.MemberRole(a.ID, u.ID); role != "admin" {
		t.Fatalf("EnsureMember downgraded role to %q", role)
	}
	if err := st.SetMemberRole(a.ID, u.ID, "viewer"); err != nil {
		t.Fatal(err)
	}
	ms, err := st.Memberships(u.ID)
	if err != nil || len(ms) != 1 || ms[0].Role != "viewer" || ms[0].Tenant.Slug != "acme" {
		t.Fatalf("memberships: %v %+v", err, ms)
	}
}
