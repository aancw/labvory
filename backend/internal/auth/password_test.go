package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	password := "correct horse battery staple"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("expected argon2id PHC format, got %q", hash)
	}

	match, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !match {
		t.Fatal("correct password must verify")
	}

	match, err = VerifyPassword("wrong password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword wrong: %v", err)
	}
	if match {
		t.Fatal("wrong password must not verify")
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{"ok minimal", "longenough1", false},
		{"too short", "short", true},
		{"exactly 10", "1234567890", false},
		{"too long", strings.Repeat("a", 201), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.password)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestValidateUsername(t *testing.T) {
	valid := []string{"admin", "ops.user", "ops_user", "ops-user", "abc"}
	invalid := []string{"", "ab", "-lead", "has space", "way-too-long-username-12345678901234"}
	for _, u := range valid {
		if err := ValidateUsername(u); err != nil {
			t.Errorf("expected %q valid, got %v", u, err)
		}
	}
	for _, u := range invalid {
		if err := ValidateUsername(u); err == nil {
			t.Errorf("expected %q invalid, got nil", u)
		}
	}
}

func TestTokenHashStable(t *testing.T) {
	token := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	h1 := TokenHash(token)
	h2 := TokenHash(token)
	if h1 != h2 {
		t.Fatal("TokenHash must be deterministic")
	}
	if len(h1) != 64 {
		t.Fatalf("expected sha256 hex length 64, got %d", len(h1))
	}
}
