// Package secrets seals tenant-owned credentials (REQ-023) with
// AES-256-GCM under a deployment master key. Plaintext exists only in
// memory between the admin's request and the git child process.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
)

// EnvKey names the master key: base64 (std or raw) of exactly 32 bytes.
const EnvKey = "SPECQUILL_SECRET_KEY"

// ErrNotConfigured — the deployment has no master key; credential storage
// is unavailable (installation tokens and token_env still work).
var ErrNotConfigured = errors.New("secrets: " + EnvKey + " is not set")

// KeyID names the active master key inside stored rows — the rotation seam:
// a future v2 key decrypts v1 rows on read and re-seals on the next update.
const KeyID = "v1"

type Box struct {
	aead cipher.AEAD
}

// NewFromEnv builds the box from the env master key. Returns
// ErrNotConfigured (and a nil box) when the variable is unset — callers keep
// the server running and disable only the credential endpoints.
func NewFromEnv() (*Box, error) {
	raw := os.Getenv(EnvKey)
	if raw == "" {
		return nil, ErrNotConfigured
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		key, err = base64.RawStdEncoding.DecodeString(raw)
	}
	if err != nil {
		return nil, fmt.Errorf("secrets: %s is not base64: %w", EnvKey, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: %s must decode to 32 bytes (got %d)", EnvKey, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// aad binds a ciphertext to its owning tenant: a row copied across tenants
// fails to decrypt.
func aad(tenantID int64) []byte {
	return []byte("specquill:cred:" + strconv.FormatInt(tenantID, 10))
}

// Seal encrypts a token for the tenant. Returns the random nonce, the
// ciphertext and the key id to store alongside.
func (b *Box) Seal(tenantID int64, plaintext string) (nonce, ciphertext []byte, keyID string, err error) {
	if b == nil {
		return nil, nil, "", ErrNotConfigured
	}
	nonce = make([]byte, b.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, "", err
	}
	ciphertext = b.aead.Seal(nil, nonce, []byte(plaintext), aad(tenantID))
	return nonce, ciphertext, KeyID, nil
}

// Open decrypts a stored token. Error messages never carry plaintext.
func (b *Box) Open(tenantID int64, nonce, ciphertext []byte, keyID string) (string, error) {
	if b == nil {
		return "", ErrNotConfigured
	}
	if keyID != KeyID {
		return "", fmt.Errorf("secrets: unknown key id %q", keyID)
	}
	plain, err := b.aead.Open(nil, nonce, ciphertext, aad(tenantID))
	if err != nil {
		return "", errors.New("secrets: decrypt failed (wrong key or tenant)")
	}
	return string(plain), nil
}
