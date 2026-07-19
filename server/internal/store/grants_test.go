package store

import (
	"errors"
	"testing"
)

func grantFixture(t *testing.T) (*Store, *Tenant, *User) {
	st := OpenTest(t)
	ten, err := st.EnsureTenant("acme", "github", 42, "Acme")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SyncTenantRepos(ten.ID, []TenantRepo{
		{RepoID: "specs", Mode: "writable", Remote: "r1", DefaultBranch: "main"},
		{RepoID: "regs", Mode: "readonly", Remote: "r2", DefaultBranch: "main"},
	}); err != nil {
		t.Fatal(err)
	}
	u, err := st.UpsertUser("oidc", "ext-1", "Eve External", "Eve@Partner.Test")
	if err != nil {
		t.Fatal(err)
	}
	return st, ten, u
}

func TestRepoGrants(t *testing.T) {
	st, ten, u := grantFixture(t)

	if _, err := st.RepoGrantRole(ten.ID, "specs", u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := st.UpsertRepoGrant(ten.ID, "specs", u.ID, "viewer", 0); err != nil {
		t.Fatal(err)
	}
	// upsert re-roles
	if err := st.UpsertRepoGrant(ten.ID, "specs", u.ID, "editor", 0); err != nil {
		t.Fatal(err)
	}
	if role, err := st.RepoGrantRole(ten.ID, "specs", u.ID); err != nil || role != "editor" {
		t.Fatalf("grant role: %v %q", err, role)
	}
	if m, err := st.UserRepoGrants(ten.ID, u.ID); err != nil || len(m) != 1 || m["specs"] != "editor" {
		t.Fatalf("UserRepoGrants: %v %v", err, m)
	}
	if gs, err := st.RepoGrants(ten.ID, "specs"); err != nil || len(gs) != 1 || gs[0].Email != "Eve@Partner.Test" || gs[0].Role != "editor" {
		t.Fatalf("RepoGrants: %v %+v", err, gs)
	}

	// grant-only user shows up as a synthetic viewer membership
	ms, err := st.Memberships(u.ID)
	if err != nil || len(ms) != 1 || ms[0].Tenant.Slug != "acme" || ms[0].Role != "viewer" {
		t.Fatalf("grant-only membership: %v %+v", err, ms)
	}
	// ... but has no member row (stays out of tenant management)
	if _, err := st.MemberRole(ten.ID, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grant-only user must have no member row, got %v", err)
	}
	// a real member row wins over the synthetic viewer
	if err := st.EnsureMember(ten.ID, u.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if ms, _ := st.Memberships(u.ID); len(ms) != 1 || ms[0].Role != "admin" {
		t.Fatalf("member row should win: %+v", ms)
	}
	if err := st.DeleteMember(ten.ID, u.ID); err != nil {
		t.Fatal(err)
	}

	// deleting the repo cascades the grant
	if err := st.DeleteTenantRepo(ten.ID, "specs"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RepoGrantRole(ten.ID, "specs", u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grant should cascade with tenant_repos, got %v", err)
	}

	if err := st.DeleteRepoGrant(ten.ID, "regs", u.ID); err != nil { // no-op delete is fine
		t.Fatal(err)
	}
}

func TestGrantInvites(t *testing.T) {
	st, ten, admin := grantFixture(t)

	if err := st.AddGrantInvite(ten.ID, "specs", "email", "New.Person@Partner.Test", "editor", admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.AddGrantInvite(ten.ID, "regs", "github", "OctoCat", "viewer", admin.ID); err != nil {
		t.Fatal(err)
	}
	if vs, err := st.RepoGrantInvites(ten.ID, "specs"); err != nil || len(vs) != 1 || vs[0].Matcher != "new.person@partner.test" {
		t.Fatalf("invites: %v %+v", err, vs)
	}

	// email match claims the specs invite (case-insensitive), not the github one
	u, err := st.UpsertUser("local", "np", "New Person", "new.person@partner.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimGrantInvites(u.ID, "New.Person@Partner.Test", ""); err != nil {
		t.Fatal(err)
	}
	if role, err := st.RepoGrantRole(ten.ID, "specs", u.ID); err != nil || role != "editor" {
		t.Fatalf("claimed grant: %v %q", err, role)
	}
	if vs, _ := st.RepoGrantInvites(ten.ID, "specs"); len(vs) != 0 {
		t.Fatalf("claimed invite not deleted: %+v", vs)
	}
	// idempotent: claiming again is a no-op
	if err := st.ClaimGrantInvites(u.ID, "new.person@partner.test", ""); err != nil {
		t.Fatal(err)
	}

	// login match claims the github invite; an empty login never matches
	gh, err := st.UpsertUser("github", "github:7", "Octo Cat", "octo@cat.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimGrantInvites(gh.ID, "octo@cat.test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RepoGrantRole(ten.ID, "regs", gh.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("github invite must not match on empty login")
	}
	if err := st.ClaimGrantInvites(gh.ID, "octo@cat.test", "octocat"); err != nil {
		t.Fatal(err)
	}
	if role, err := st.RepoGrantRole(ten.ID, "regs", gh.ID); err != nil || role != "viewer" {
		t.Fatalf("login-claimed grant: %v %q", err, role)
	}

	// an existing grant is not downgraded by a claim
	if err := st.AddGrantInvite(ten.ID, "regs", "github", "octocat", "viewer", admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertRepoGrant(ten.ID, "regs", gh.ID, "editor", admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimGrantInvites(gh.ID, "octo@cat.test", "octocat"); err != nil {
		t.Fatal(err)
	}
	if role, _ := st.RepoGrantRole(ten.ID, "regs", gh.ID); role != "editor" {
		t.Fatalf("claim downgraded an existing grant to %q", role)
	}
}

func TestUserByEmailOrLogin(t *testing.T) {
	st, _, u := grantFixture(t)
	if err := st.SetUserLogin(u.ID, "EveDev"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.UserByEmailOrLogin("eve@partner.test"); err != nil || got.ID != u.ID {
		t.Fatalf("by email: %v %+v", err, got)
	}
	if got, err := st.UserByEmailOrLogin("@evedev"); err != nil || got.ID != u.ID {
		t.Fatalf("by @login: %v %+v", err, got)
	}
	if _, err := st.UserByEmailOrLogin("nobody@nowhere.test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
