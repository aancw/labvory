// Package auth implements password hashing and the session-based authentication service.
package auth

import (
	"errors"

	"github.com/alexedwards/argon2id"
)

// Password policy violations.
var (
	ErrPasswordTooShort = errors.New("password must be at least 10 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 200 characters")
)

// Params follow the OWASP Password Storage Cheat Sheet recommendations (m=19456 KiB, t=2, p=1).
var defaultParams = &argon2id.Params{
	Memory:      19456,
	Iterations:  2,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

// HashPassword derives an Argon2id PHC-formatted hash.
func HashPassword(password string) (string, error) {
	return argon2id.CreateHash(password, defaultParams)
}

// VerifyPassword checks a plaintext password against a stored PHC hash.
func VerifyPassword(password, hash string) (match bool, err error) {
	return argon2id.ComparePasswordAndHash(password, hash)
}

// ValidatePassword enforces the minimum strength policy before hashing.
func ValidatePassword(password string) error {
	if len(password) < 10 {
		return ErrPasswordTooShort
	}
	if len(password) > 200 {
		return ErrPasswordTooLong
	}
	return nil
}
