package store

import (
	"errors"
	"testing"
)

func grantFixture(t *testing.T) (*Store, *User) {
	st := OpenTest(t)
	if err := st.SyncRepos([]RepoRow{
		{RepoID: "specs", Mode: "writable", Remote: "r1", DefaultBranch: "main"},
		{RepoID: "regs", Mode: "readonly", Remote: "r2", DefaultBranch: "main"},
	}); err != nil {
		t.Fatal(err)
	}
	u, err := st.UpsertUser("oidc", "ext-1", "Eve External", "Eve@Partner.Test")
	if err != nil {
		t.Fatal(err)
	}
	return st, u
}

func TestRepoGrants(t *testing.T) {
	st, u := grantFixture(t)

	if _, err := st.RepoGrantRole("specs", u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := st.UpsertRepoGrant("specs", u.ID, "viewer", 0); err != nil {
		t.Fatal(err)
	}
	// upsert re-roles
	if err := st.UpsertRepoGrant("specs", u.ID, "member", 0); err != nil {
		t.Fatal(err)
	}
	if role, err := st.RepoGrantRole("specs", u.ID); err != nil || role != "member" {
		t.Fatalf("grant role: %v %q", err, role)
	}
	if m, err := st.UserRepoGrants(u.ID); err != nil || len(m) != 1 || m["specs"] != "member" {
		t.Fatalf("UserRepoGrants: %v %v", err, m)
	}
	if gs, err := st.RepoGrants("specs"); err != nil || len(gs) != 1 || gs[0].Email != "Eve@Partner.Test" || gs[0].Role != "member" {
		t.Fatalf("RepoGrants: %v %+v", err, gs)
	}

	// grant-only user: not enrolled, but visibly holds a grant
	if u.Role != "" {
		t.Fatalf("fixture user should be unenrolled, got %q", u.Role)
	}
	if has, err := st.HasAnyGrant(u.ID); err != nil || !has {
		t.Fatalf("HasAnyGrant: %v %v", err, has)
	}
	// ... and stays out of the member list until enrolled
	if ms, err := st.MemberList(); err != nil || len(ms) != 0 {
		t.Fatalf("grant-only user must not be a member: %v %+v", err, ms)
	}
	if err := st.EnsureUserRole(u.ID, "viewer"); err != nil {
		t.Fatal(err)
	}
	// enrollment preserves an existing role
	if err := st.EnsureUserRole(u.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.UserByID(u.ID); got.Role != "viewer" {
		t.Fatalf("EnsureUserRole must not overwrite, got %q", got.Role)
	}
	if err := st.SetUserRole(u.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if ms, _ := st.MemberList(); len(ms) != 1 || ms[0].Role != "admin" {
		t.Fatalf("member list after enroll: %+v", ms)
	}

	// deleting the repo cascades the grant
	if err := st.DeleteRepoRow("specs"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RepoGrantRole("specs", u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grant should cascade with repos, got %v", err)
	}

	if err := st.DeleteRepoGrant("regs", u.ID); err != nil { // no-op delete is fine
		t.Fatal(err)
	}
}

func TestGrantInvites(t *testing.T) {
	st, admin := grantFixture(t)

	if err := st.AddGrantInvite("specs", "New.Person@Partner.Test", "member", admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGrantInvite("regs", "octo@cat.test", "viewer", admin.ID); err != nil {
		t.Fatal(err)
	}
	if vs, err := st.RepoGrantInvites("specs"); err != nil || len(vs) != 1 || vs[0].Matcher != "new.person@partner.test" {
		t.Fatalf("invites: %v %+v", err, vs)
	}

	// email match claims the specs invite (case-insensitive), not the other one
	u, err := st.UpsertUser("local", "np", "New Person", "new.person@partner.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimGrantInvites(u.ID, "New.Person@Partner.Test"); err != nil {
		t.Fatal(err)
	}
	if role, err := st.RepoGrantRole("specs", u.ID); err != nil || role != "member" {
		t.Fatalf("claimed grant: %v %q", err, role)
	}
	if _, err := st.RepoGrantRole("regs", u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign invite must not match")
	}
	if vs, _ := st.RepoGrantInvites("specs"); len(vs) != 0 {
		t.Fatalf("claimed invite not deleted: %+v", vs)
	}
	// idempotent: claiming again is a no-op
	if err := st.ClaimGrantInvites(u.ID, "new.person@partner.test"); err != nil {
		t.Fatal(err)
	}

	// an existing grant is not downgraded by a claim
	oc, err := st.UpsertUser("oidc", "oc-1", "Octo Cat", "octo@cat.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertRepoGrant("regs", oc.ID, "member", admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimGrantInvites(oc.ID, "octo@cat.test"); err != nil {
		t.Fatal(err)
	}
	if role, _ := st.RepoGrantRole("regs", oc.ID); role != "member" {
		t.Fatalf("claim downgraded an existing grant to %q", role)
	}
}

func TestUserByEmail(t *testing.T) {
	st, u := grantFixture(t)
	if got, err := st.UserByEmail("eve@partner.test"); err != nil || got.ID != u.ID {
		t.Fatalf("by email: %v %+v", err, got)
	}
	if _, err := st.UserByEmail("nobody@nowhere.test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
