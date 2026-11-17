package auth

import (
	"testing"
	"time"
)

// RFC 6238 test vectors for SHA-1, rendered as 6 digits. The secret is the
// ASCII string "12345678901234567890" Base32-encoded.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestTOTPRFC6238Vectors(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"}, // RFC vector 14050471, truncated to 6 digits
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, tc := range cases {
		got, err := TOTPCode(rfcSecret, time.Unix(tc.unix, 0))
		if err != nil {
			t.Fatalf("TOTPCode(%d): %v", tc.unix, err)
		}
		if got != tc.want {
			t.Fatalf("TOTPCode at %d = %s, want %s", tc.unix, got, tc.want)
		}
	}
}

func TestValidateTOTP(t *testing.T) {
	now := time.Unix(1111111109, 0) // code 081804

	if step, ok := ValidateTOTP(rfcSecret, "081804", now); !ok || step != now.Unix()/30 {
		t.Fatalf("current code should validate (step=%d ok=%v)", step, ok)
	}
	// Skew window: previous step accepted.
	prevCode, _ := TOTPCode(rfcSecret, time.Unix(1111111109-30, 0))
	if _, ok := ValidateTOTP(rfcSecret, prevCode, now); !ok {
		t.Fatal("previous-step code should validate inside the skew window")
	}
	if _, ok := ValidateTOTP(rfcSecret, "000000", now); ok {
		t.Fatal("wrong code must not validate")
	}
	if _, ok := ValidateTOTP(rfcSecret, "08180", now); ok {
		t.Fatal("malformed code must not validate")
	}
	// Two steps away is outside the skew window.
	farCode, _ := TOTPCode(rfcSecret, time.Unix(1111111109-90, 0))
	if _, ok := ValidateTOTP(rfcSecret, farCode, now); ok {
		t.Fatal("code two steps old must not validate")
	}
}

func TestGenerateTOTPSecret(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(secret) != 32 { // 20 bytes → 32 Base32 chars, unpadded
		t.Fatalf("secret length = %d, want 32", len(secret))
	}
	// Round-trip: a generated secret must validate its own code.
	code, err := TOTPCode(secret, time.Unix(1234567890, 0))
	if err != nil {
		t.Fatalf("code: %v", err)
	}
	if _, ok := ValidateTOTP(secret, code, time.Unix(1234567890, 0)); !ok {
		t.Fatal("generated secret should validate its own code")
	}
}

func TestOTPAuthURI(t *testing.T) {
	uri := OTPAuthURI("aan", rfcSecret)
	want := "otpauth://totp/Lanvory:aan?digits=6&issuer=Lanvory&period=30&secret=" + rfcSecret
	if uri != want {
		t.Fatalf("OTPAuthURI = %s, want %s", uri, want)
	}
}
