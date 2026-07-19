package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func testBox(t *testing.T) *Box {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvKey, base64.StdEncoding.EncodeToString(key))
	b, err := NewFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSealOpenRoundtrip(t *testing.T) {
	b := testBox(t)
	nonce, ct, kid, err := b.Seal(7, "ghp_secret-token")
	if err != nil || kid != KeyID {
		t.Fatalf("seal: %v %q", err, kid)
	}
	if string(ct) == "ghp_secret-token" {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := b.Open(7, nonce, ct, kid)
	if err != nil || got != "ghp_secret-token" {
		t.Fatalf("open: %v %q", err, got)
	}
}

func TestTenantBinding(t *testing.T) {
	b := testBox(t)
	nonce, ct, kid, _ := b.Seal(7, "tok")
	if _, err := b.Open(8, nonce, ct, kid); err == nil {
		t.Fatal("ciphertext must not open under another tenant")
	}
	if _, err := b.Open(7, nonce, ct, "v2"); err == nil {
		t.Fatal("unknown key id must refuse")
	}
}

func TestUnconfigured(t *testing.T) {
	t.Setenv(EnvKey, "")
	if _, err := NewFromEnv(); err != ErrNotConfigured {
		t.Fatalf("want ErrNotConfigured, got %v", err)
	}
	var b *Box
	if _, _, _, err := b.Seal(1, "x"); err != ErrNotConfigured {
		t.Fatalf("nil box seal: %v", err)
	}
	if _, err := b.Open(1, nil, nil, KeyID); err != ErrNotConfigured {
		t.Fatalf("nil box open: %v", err)
	}
}

func TestBadKeys(t *testing.T) {
	t.Setenv(EnvKey, "not-base64!!")
	if _, err := NewFromEnv(); err == nil {
		t.Fatal("garbage key accepted")
	}
	t.Setenv(EnvKey, base64.StdEncoding.EncodeToString([]byte("short")))
	if _, err := NewFromEnv(); err == nil {
		t.Fatal("short key accepted")
	}
}
