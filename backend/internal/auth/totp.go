package auth

// RFC 6238 TOTP (HMAC-SHA1, 6 digits, 30-second period) implemented on the
// standard library so two-factor authentication needs no external dependency.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	totpPeriod = 30 // seconds per step
	totpDigits = 6
	// totpWindow accepts one step back and forward to absorb small clock skew.
	totpWindow = 1
	// totpSecretLen is the raw secret size; RFC 4226 recommends ≥160 bits.
	totpSecretLen = 20
)

var totpB32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret returns a fresh Base32 secret for enrollment in an
// authenticator app.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, totpSecretLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return totpB32.EncodeToString(b), nil
}

// TOTPCode computes the 6-digit code for a Base32 secret at time t.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := totpB32.DecodeString(strings.ToUpper(strings.ReplaceAll(secret, " ", "")))
	if err != nil {
		return "", fmt.Errorf("decode totp secret: %w", err)
	}
	return hotpSHA1(key, t.Unix()/totpPeriod), nil
}

// ValidateTOTP checks code against the step at now and ± the skew window. It
// returns the matched step counter (for the replay guard) and whether the code
// matched.
func ValidateTOTP(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	step := now.Unix() / totpPeriod
	for i := int64(-totpWindow); i <= totpWindow; i++ {
		want, err := TOTPCode(secret, time.Unix((step+i)*totpPeriod, 0))
		if err != nil {
			return 0, false
		}
		if hmac.Equal([]byte(want), []byte(code)) {
			return step + i, true
		}
	}
	return 0, false
}

// OTPAuthURI builds the enrollment URI an authenticator app scans or accepts
// via manual entry.
func OTPAuthURI(username, secret string) string {
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", "LabVory")
	q.Set("period", strconv.Itoa(totpPeriod))
	q.Set("digits", strconv.Itoa(totpDigits))
	return "otpauth://totp/" + url.PathEscape("LabVory:"+username) + "?" + q.Encode()
}

// hotpSHA1 implements the RFC 4226 HMAC-SHA1 HOTP algorithm with 6 digits.
func hotpSHA1(key []byte, counter int64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	// Dynamic truncation per RFC 4226 §5.3.
	off := sum[len(sum)-1] & 0x0f
	value := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1_000_000
	return fmt.Sprintf("%0*d", totpDigits, value)
}
