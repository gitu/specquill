package store

import (
	"errors"
	"testing"
)

func TestCredentialLifecycle(t *testing.T) {
	st := OpenTest(t)
	ten, err := st.EnsureTenant("acme", "config", 0, "Acme")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.EnsureTenant("other", "config", 0, "Other")
	if err != nil {
		t.Fatal(err)
	}

	id, err := st.AddCredential(Credential{
		TenantID: ten.ID, Name: "deploy PAT", Username: "bot",
		Nonce: []byte{1, 2, 3}, Ciphertext: []byte{4, 5, 6}, KeyID: "v1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// listed with zero refs; sealed material present on the single read
	list, err := st.Credentials(ten.ID)
	if err != nil || len(list) != 1 || list[0].Name != "deploy PAT" || list[0].RepoCount != 0 {
		t.Fatalf("list: %v %+v", err, list)
	}
	c, err := st.Credential(ten.ID, id)
	if err != nil || string(c.Ciphertext) != string([]byte{4, 5, 6}) || c.KeyID != "v1" {
		t.Fatalf("read: %v %+v", err, c)
	}
	// tenant-scoped: another tenant cannot read it
	if _, err := st.Credential(other.ID, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read must be ErrNotFound, got %v", err)
	}

	// attach to a repo → refcount blocks deletion, RepoCredential resolves
	if err := st.UpsertTenantRepo(ten.ID, TenantRepo{RepoID: "specs", Mode: "writable", Remote: "r", DefaultBranch: "main", CredentialID: id}); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.CredentialRefCount(id); n != 1 {
		t.Fatalf("refcount: %d", n)
	}
	rc, err := st.RepoCredential("acme", "specs")
	if err != nil || rc.ID != id || rc.Username != "bot" {
		t.Fatalf("repo credential: %v %+v", err, rc)
	}

	// rotation re-seals; rename-only keeps sealed fields
	if err := st.UpdateCredential(ten.ID, id, "deploy PAT", "bot", []byte{9}, []byte{8}, "v1"); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Credential(ten.ID, id); string(c.Ciphertext) != string([]byte{8}) {
		t.Fatalf("rotate did not re-seal: %+v", c)
	}
	if err := st.UpdateCredential(ten.ID, id, "renamed", "bot", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.Credential(ten.ID, id); c.Name != "renamed" || string(c.Ciphertext) != string([]byte{8}) {
		t.Fatalf("rename touched sealed fields: %+v", c)
	}

	// detach → delete succeeds; attach validates tenant ownership
	if err := st.SetRepoCredential(other.ID, "specs", id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant attach must fail, got %v", err)
	}
	if err := st.SetRepoCredential(ten.ID, "specs", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RepoCredential("acme", "specs"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("detached repo still resolves a credential: %v", err)
	}
	if err := st.DeleteCredential(ten.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Credential(ten.ID, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("credential survived deletion")
	}
}
