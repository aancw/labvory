// Package secrets encrypts integration credentials at rest with AES-256-GCM.
// The key comes from LV_SECRET_KEY (hex or base64, 32 bytes). Plaintext secrets
// are never stored in the database and never returned by the API.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

var (
	// ErrNoKey is returned when LV_SECRET_KEY is not configured.
	ErrNoKey = errors.New("LV_SECRET_KEY not configured: set a 32-byte hex key to store integration secrets")
	// ErrInvalidKey indicates a malformed key.
	ErrInvalidKey = errors.New("LV_SECRET_KEY must be 32 bytes (64 hex chars) or any passphrase of >= 16 chars")
	ErrCiphertext = errors.New("secret ciphertext malformed")
)

// Box encrypts and decrypts secret values.
type Box struct {
	aead cipher.AEAD
}

// NewBox builds an encryption box from a key value (LV_SECRET_KEY content:
// 32-byte hex/base64, or a passphrase of >= 16 chars).
func NewBox(raw string) (*Box, error) {
	if raw == "" {
		return nil, ErrNoKey
	}
	var key []byte
	if h, err := hex.DecodeString(raw); err == nil && len(h) == 32 {
		key = h
	} else if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		key = b
	} else if len(raw) >= 16 {
		// Passphrase mode: derive a 32-byte key (deterministic, adequate for
		// at-rest obfuscation in a homelab; not a KDF-hardened scheme).
		sum := sha256.Sum256([]byte(raw))
		key = sum[:]
	} else {
		return nil, ErrInvalidKey
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Encrypt returns "enc:v1:<base64>" or an error.
func (b *Box) Encrypt(plaintext string) (string, error) {
	if b == nil {
		return "", ErrNoKey
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return "enc:v1:" + base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt reverses Encrypt. Values without the enc:v1 prefix pass through unchanged
// (operator-entered non-secret config strings).
func (b *Box) Decrypt(value string) (string, error) {
	if b == nil {
		return "", ErrNoKey
	}
	if !strings.HasPrefix(value, "enc:v1:") {
		return value, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "enc:v1:"))
	if err != nil {
		return "", ErrCiphertext
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", ErrCiphertext
	}
	pt, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", ErrCiphertext
	}
	return string(pt), nil
}

// IsEncrypted reports whether a stored value is an encrypted blob.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, "enc:v1:")
}

// GenerateKey prints a fresh 32-byte hex key (CLI helper output).
func GenerateKey() (string, error) {
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return "", err
	}
	return hex.EncodeToString(k), nil
}
