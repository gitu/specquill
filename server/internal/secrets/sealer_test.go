package secrets

import (
	"encoding/base64"
	"testing"

	"specquill/server/internal/config"
)

func testSealer(t *testing.T) *Sealer {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i * 7)
	}
	t.Setenv("TEST_MASTER_KEY", base64.StdEncoding.EncodeToString(key))
	sl, err := NewSealer(config.SecretsConfig{MasterKeyEnv: "TEST_MASTER_KEY"})
	if err != nil || sl == nil {
		t.Fatalf("NewSealer: %v", err)
	}
	return sl
}

func TestSealOpenRoundTrip(t *testing.T) {
	sl := testSealer(t)
	aad := AAD(42, "git_pat", "repo1")
	plain := []byte("ghp_supersecrettoken")
	blob, err := sl.Seal(aad, plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := sl.Open(aad, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != string(plain) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
}

// A ciphertext must not decrypt under a different (tenant, kind, ref) — this is
// the cryptographic cross-tenant isolation, independent of any WHERE clause.
func TestOpenRejectsWrongAAD(t *testing.T) {
	sl := testSealer(t)
	blob, err := sl.Seal(AAD(1, "git_pat", "repo1"), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	for _, wrong := range [][]byte{
		AAD(2, "git_pat", "repo1"),   // other tenant
		AAD(1, "git_basic", "repo1"), // other kind
		AAD(1, "git_pat", "repo2"),   // other slot
	} {
		if _, err := sl.Open(wrong, blob); err == nil {
			t.Fatalf("Open succeeded under wrong AAD %q — cross-slot decryption possible", wrong)
		}
	}
}

func TestOpenRejectsTamper(t *testing.T) {
	sl := testSealer(t)
	aad := AAD(1, "git_pat", "r")
	blob, err := sl.Seal(aad, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)-1] ^= 0xff // flip a ciphertext bit
	if _, err := sl.Open(aad, blob); err == nil {
		t.Fatal("Open accepted a tampered blob")
	}
}

func TestNewSealerDisabledWhenNoKey(t *testing.T) {
	sl, err := NewSealer(config.SecretsConfig{})
	if err != nil || sl != nil {
		t.Fatalf("expected (nil, nil) when no key configured, got (%v, %v)", sl, err)
	}
}
