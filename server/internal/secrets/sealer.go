// Package secrets encrypts per-tenant credentials at rest. A single 32-byte
// master key (AES-256) supplied out-of-band seals each secret with AES-GCM;
// the ciphertext is authenticated against additional data that binds it to a
// specific (tenant, kind, ref), so a row copied into another tenant or slot
// physically fails to decrypt — cross-tenant isolation enforced by the cipher,
// not only by a WHERE clause.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"specquill/server/internal/config"
)

// KeyVersion is stamped on every sealed row. Bump only when the key-derivation
// scheme changes; the master key itself can be swapped without a version bump
// once retired-key support lands (rotation is a documented follow-up).
const KeyVersion = 1

// Sealer holds the loaded master key and its AES-GCM AEAD.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer loads the 32-byte master key from the configured env var or file
// (base64- or hex-encoded) and builds the AEAD. Returns (nil, nil) when no key
// source is configured, so callers can treat a nil Sealer as "encryption off".
func NewSealer(cfg config.SecretsConfig) (*Sealer, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	var raw string
	switch {
	case cfg.MasterKeyEnv != "":
		raw = os.Getenv(cfg.MasterKeyEnv)
		if raw == "" {
			return nil, fmt.Errorf("secrets.master_key_env: %s is not set", cfg.MasterKeyEnv)
		}
	case cfg.MasterKeyPath != "":
		b, err := os.ReadFile(cfg.MasterKeyPath)
		if err != nil {
			return nil, fmt.Errorf("secrets.master_key_path: %w", err)
		}
		raw = string(b)
	}
	key, err := decodeKey(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// decodeKey accepts a 32-byte key encoded as base64 (std or raw-url) or hex.
func decodeKey(s string) ([]byte, error) {
	for _, dec := range []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
		hex.DecodeString,
	} {
		if b, err := dec(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, fmt.Errorf("secrets master key must decode (base64 or hex) to exactly 32 bytes")
}

// Seal encrypts plaintext, authenticating it against aad (which must uniquely
// identify the slot, e.g. tenant_id|kind|ref). The returned blob is
// nonce||ciphertext; store it verbatim with KeyVersion.
func (s *Sealer) Seal(aad, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	// Seal appends ciphertext to nonce, so the nonce prefixes the blob
	return s.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open reverses Seal. It fails if the blob was tampered with OR if aad does
// not match the value used at seal time (wrong tenant/kind/ref).
func (s *Sealer) Open(aad, blob []byte) ([]byte, error) {
	ns := s.aead.NonceSize()
	if len(blob) < ns {
		return nil, fmt.Errorf("sealed blob too short")
	}
	nonce, ct := blob[:ns], blob[ns:]
	return s.aead.Open(nil, nonce, ct, aad)
}

// AAD builds the canonical additional-data binding for a credential slot.
func AAD(tenantID int64, kind, ref string) []byte {
	return []byte(fmt.Sprintf("%d\x1f%s\x1f%s", tenantID, kind, ref))
}
