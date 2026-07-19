package store

import (
	"encoding/base64"
	"errors"
	"testing"

	"specquill/server/internal/config"
	"specquill/server/internal/secrets"
)

func credStore(t *testing.T) (*Store, *Tenant) {
	t.Helper()
	st := OpenTest(t)
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	t.Setenv("TEST_MK", base64.StdEncoding.EncodeToString(key))
	sl, err := secrets.NewSealer(config.SecretsConfig{MasterKeyEnv: "TEST_MK"})
	if err != nil {
		t.Fatal(err)
	}
	st.SetSealer(sl)
	ten, err := st.EnsureTenant("acme", "github", 7, "Acme")
	if err != nil {
		t.Fatal(err)
	}
	return st, ten
}

func TestCredentialRoundTripAndRevoke(t *testing.T) {
	st, ten := credStore(t)

	if err := st.PutCredential(ten.ID, "git_pat", "specs", "", []byte("ghp_abc"), 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	user, secret, err := st.GetCredentialSecret(ten.ID, "git_pat", "specs")
	if err != nil || string(secret) != "ghp_abc" {
		t.Fatalf("get: %q %q %v", user, secret, err)
	}

	// list returns metadata but never the secret value
	creds, err := st.ListCredentials(ten.ID)
	if err != nil || len(creds) != 1 || creds[0].Kind != "git_pat" || creds[0].Ref != "specs" {
		t.Fatalf("list: %+v %v", creds, err)
	}

	// replace bumps rotated_at and changes the value
	if err := st.PutCredential(ten.ID, "git_pat", "specs", "", []byte("ghp_new"), 0); err != nil {
		t.Fatal(err)
	}
	if _, secret, _ := st.GetCredentialSecret(ten.ID, "git_pat", "specs"); string(secret) != "ghp_new" {
		t.Fatalf("rotate did not replace value: %q", secret)
	}

	if err := st.RevokeCredential(ten.ID, "git_pat", "specs"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.GetCredentialSecret(ten.ID, "git_pat", "specs"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after revoke want ErrNotFound, got %v", err)
	}
}

// A credential stored for one tenant must not be resolvable by another, and its
// ciphertext must not decrypt in the other tenant's context even if the row is
// copied across (AAD binding).
func TestCredentialCrossTenantIsolation(t *testing.T) {
	st, acme := credStore(t)
	beta, err := st.EnsureTenant("beta", "github", 8, "Beta")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutCredential(acme.ID, "git_basic", "", "u", []byte("s3cr3t"), 0); err != nil {
		t.Fatal(err)
	}

	// beta cannot read acme's slot
	if _, _, err := st.GetCredentialSecret(beta.ID, "git_basic", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("beta read of acme credential: want ErrNotFound, got %v", err)
	}

	// copy acme's ciphertext into a beta row and confirm it will not decrypt
	var blob []byte
	if err := st.queryRow(`SELECT secret_blob FROM tenant_credentials WHERE tenant_id=? AND kind=? AND ref=?`,
		acme.ID, "git_basic", "").Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if _, err := st.exec(`INSERT INTO tenant_credentials (tenant_id, kind, ref, username, secret_blob, key_version, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, beta.ID, "git_basic", "", "u", blob, 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.GetCredentialSecret(beta.ID, "git_basic", ""); err == nil {
		t.Fatal("acme's ciphertext decrypted in beta's context — AAD binding not enforced")
	}
}

func TestCredentialDisabledWithoutSealer(t *testing.T) {
	st := OpenTest(t)
	ten, err := st.EnsureTenant("x", "config", 0, "X")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutCredential(ten.ID, "git_pat", "", "", []byte("s"), 0); !errors.Is(err, ErrSecretsDisabled) {
		t.Fatalf("want ErrSecretsDisabled, got %v", err)
	}
}
