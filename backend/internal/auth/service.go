package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"regexp"
	"time"

	"github.com/aancw/lanvory/internal/secrets"
	"github.com/aancw/lanvory/internal/store"
)

var (
	// ErrInvalidCredentials is returned for unknown users or bad passwords (same message both ways).
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidUsername / ErrTakenUsername guard the setup flow.
	ErrInvalidUsername = errors.New("invalid username")
	ErrTakenUsername   = errors.New("username already taken")
	ErrNoUsersAllowed  = errors.New("setup is only available before the first administrator exists")
	ErrWrongPassword   = errors.New("current password is incorrect")

	// Two-factor errors.
	ErrTOTPRequired = errors.New("verification code required")
	ErrInvalidTOTP  = errors.New("invalid verification code")
	ErrTOTPDisabled = errors.New("two-factor is not enabled")
	ErrNoSecretBox  = errors.New("secret key not configured")
	ErrNoTOTPEnroll = errors.New("no pending two-factor enrollment")

	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{2,31}$`)
)

// Service implements authentication workflows on top of the store.
type Service struct {
	users      *store.Users
	sessions   *store.Sessions
	audit      *store.AuditEvents
	logger     *slog.Logger
	sessionTTL time.Duration
	box        *secrets.Box // optional: TOTP secrets require it
}

// NewService wires the auth service to its stores. The secrets box is
// optional; without it TOTP enrollment is refused (login of already-enabled
// users fails closed).
func NewService(users *store.Users, sessions *store.Sessions, audit *store.AuditEvents, logger *slog.Logger, sessionTTL time.Duration, box *secrets.Box) *Service {
	return &Service{users: users, sessions: sessions, audit: audit, logger: logger, sessionTTL: sessionTTL, box: box}
}

// SetupStatus reports whether the first-run administrator can still be created.
func (s *Service) SetupStatus(ctx context.Context) (bool, error) {
	n, err := s.users.Count(ctx)
	if err != nil {
		return false, err
	}
	return n == 0, nil
}

// ValidateUsername enforces the username policy.
func ValidateUsername(username string) error {
	if !usernameRe.MatchString(username) {
		return ErrInvalidUsername
	}
	return nil
}

// CreateAdmin performs first-run setup: it is refused once any user exists.
// On success it also creates a session so the operator lands directly in the app.
func (s *Service) CreateAdmin(ctx context.Context, username, displayName, password, ip, userAgent string) (store.User, SessionToken, error) {
	needsSetup, err := s.SetupStatus(ctx)
	if err != nil {
		return store.User{}, SessionToken{}, err
	}
	if !needsSetup {
		return store.User{}, SessionToken{}, ErrNoUsersAllowed
	}
	if err := ValidateUsername(username); err != nil {
		return store.User{}, SessionToken{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return store.User{}, SessionToken{}, err
	}
	if displayName == "" {
		displayName = username
	}

	hash, err := HashPassword(password)
	if err != nil {
		return store.User{}, SessionToken{}, err
	}
	user, err := s.users.Create(ctx, username, displayName, hash, "admin")
	if errors.Is(err, store.ErrConflict) {
		return store.User{}, SessionToken{}, ErrTakenUsername
	}
	if err != nil {
		return store.User{}, SessionToken{}, err
	}

	user, sessToken, _, err := s.createSession(ctx, user, ip, userAgent)
	if err != nil {
		s.logger.Error("create session after setup failed", "error", err, "user", user.Username)
		return user, SessionToken{}, nil // user exists; operator can log in manually
	}
	s.auditEvent(ctx, user, "user", user.ID, user.Username, "created", ip, "{}")
	return user, sessToken, nil
}

// SessionToken is the plaintext session token to store in the cookie.
type SessionToken struct {
	Token     string
	ExpiresAt time.Time
}

// Login verifies credentials (plus the TOTP code when the user has 2FA
// enabled) and issues a session. Timing of a missing user is masked by
// verifying against a deterministic dummy hash.
func (s *Service) Login(ctx context.Context, username, password, totpCode, ip, userAgent string) (store.User, SessionToken, error) {
	var zero SessionToken
	user, err := s.users.GetByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		// Burn comparable time to reduce username enumeration.
		_, _ = VerifyPassword(password, dummyHash)
		return store.User{}, zero, ErrInvalidCredentials
	}
	if err != nil {
		return store.User{}, zero, err
	}

	match, err := VerifyPassword(password, user.PasswordHash)
	if err != nil || !match {
		s.auditEvent(ctx, user, "auth", user.ID, user.Username, "login.failed", ip, "{}")
		return store.User{}, zero, ErrInvalidCredentials
	}

	if user.TOTPEnabledAt != nil {
		if totpCode == "" {
			return store.User{}, zero, ErrTOTPRequired
		}
		secret, derr := s.decryptSecret(user.TOTPSecret)
		if derr != nil {
			return store.User{}, zero, derr
		}
		step, ok := ValidateTOTP(secret, totpCode, time.Now())
		if !ok || step <= user.TOTPLastStep {
			// A reused (replayed) code is as bad as a wrong one.
			s.auditEvent(ctx, user, "auth", user.ID, user.Username, "login.failed", ip, `{"reason":"totp"}`)
			return store.User{}, zero, ErrInvalidTOTP
		}
		advanced, aerr := s.users.AdvanceTOTPStep(ctx, user.ID, step)
		if aerr != nil {
			return store.User{}, zero, aerr
		}
		if !advanced {
			// Lost a race against a concurrent login with the same code.
			s.auditEvent(ctx, user, "auth", user.ID, user.Username, "login.failed", ip, `{"reason":"totp"}`)
			return store.User{}, zero, ErrInvalidTOTP
		}
	}

	user, token, sess, err := s.createSession(ctx, user, ip, userAgent)
	if err != nil {
		return store.User{}, zero, err
	}
	s.auditEvent(ctx, user, "auth", user.ID, user.Username, "login.success", ip, "{}")
	return user, SessionToken{Token: sess, ExpiresAt: token.ExpiresAt}, nil
}

// SetupTOTP starts 2FA enrollment: it generates a secret, stores it encrypted
// as the pending enrollment, and returns the plaintext secret plus the
// otpauth:// URI for the authenticator app.
func (s *Service) SetupTOTP(ctx context.Context, user store.User) (secret, uri string, err error) {
	if s.box == nil {
		return "", "", ErrNoSecretBox
	}
	secret, err = GenerateTOTPSecret()
	if err != nil {
		return "", "", err
	}
	encrypted, err := s.box.Encrypt(secret)
	if err != nil {
		return "", "", err
	}
	if err := s.users.SetTOTPPending(ctx, user.ID, encrypted); err != nil {
		return "", "", err
	}
	return secret, OTPAuthURI(user.Username, secret), nil
}

// EnableTOTP confirms a pending enrollment by verifying one code against the
// stored (pending) secret.
func (s *Service) EnableTOTP(ctx context.Context, user store.User, code, ip string) error {
	fresh, err := s.users.GetByID(ctx, user.ID)
	if err != nil {
		return err
	}
	if fresh.TOTPEnabledAt != nil {
		return ErrTOTPDisabled // already active; use disable instead
	}
	if fresh.TOTPSecret == "" {
		return ErrNoTOTPEnroll
	}
	secret, err := s.decryptSecret(fresh.TOTPSecret)
	if err != nil {
		return err
	}
	step, ok := ValidateTOTP(secret, code, time.Now())
	if !ok {
		return ErrInvalidTOTP
	}
	if err := s.users.EnableTOTP(ctx, user.ID, step); err != nil {
		return err
	}
	s.auditEvent(ctx, user, "user", user.ID, user.Username, "totp.enabled", ip, "{}")
	return nil
}

// DisableTOTP removes the authenticator. When it is active the operator must
// confirm with both their password and a valid code.
func (s *Service) DisableTOTP(ctx context.Context, user store.User, password, code, ip string) error {
	fresh, err := s.users.GetByID(ctx, user.ID)
	if err != nil {
		return err
	}
	if fresh.TOTPEnabledAt == nil {
		return ErrTOTPDisabled
	}
	match, err := VerifyPassword(password, fresh.PasswordHash)
	if err != nil || !match {
		return ErrWrongPassword
	}
	secret, err := s.decryptSecret(fresh.TOTPSecret)
	if err != nil {
		return err
	}
	if _, ok := ValidateTOTP(secret, code, time.Now()); !ok {
		return ErrInvalidTOTP
	}
	if err := s.users.DisableTOTP(ctx, user.ID); err != nil {
		return err
	}
	s.auditEvent(ctx, user, "user", user.ID, user.Username, "totp.disabled", ip, "{}")
	return nil
}

// decryptSecret unwraps an encrypted TOTP secret; without a configured box it
// fails closed.
func (s *Service) decryptSecret(encrypted string) (string, error) {
	if s.box == nil {
		return "", ErrNoSecretBox
	}
	return s.box.Decrypt(encrypted)
}

// Logout revokes the session and records the event.
func (s *Service) Logout(ctx context.Context, tokenHash, ip string) error {
	err := s.sessions.Delete(ctx, tokenHash)
	if err != nil {
		return err
	}
	s.auditEvent(ctx, store.User{}, "auth", "", "", "logout", ip, "{}")
	return nil
}

// ChangePassword verifies the current password, sets the new one, and revokes other sessions.
func (s *Service) ChangePassword(ctx context.Context, user store.User, currentPassword, newPassword, currentTokenHash, ip string) error {
	match, err := VerifyPassword(currentPassword, user.PasswordHash)
	if err != nil || !match {
		return ErrWrongPassword
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.users.UpdatePassword(ctx, user.ID, hash); err != nil {
		return err
	}
	if _, err := s.sessions.DeleteOthersForUser(ctx, user.ID, currentTokenHash); err != nil {
		return err
	}
	s.auditEvent(ctx, user, "user", user.ID, user.Username, "password.changed", ip, "{}")
	return nil
}

// ResolveSession maps a plaintext cookie token to the active user, sliding the expiry forward
// when more than half the TTL has elapsed.
func (s *Service) ResolveSession(ctx context.Context, token string) (store.User, bool, error) {
	if token == "" {
		return store.User{}, false, nil
	}
	hash := TokenHash(token)
	user, expiresAt, err := s.sessions.ActiveUserByTokenHash(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, false, nil
	}
	if err != nil {
		return store.User{}, false, err
	}
	remaining := time.Until(expiresAt)
	if remaining < s.sessionTTL/2 {
		if err := s.sessions.Extend(ctx, hash, time.Now().Add(s.sessionTTL)); err != nil {
			s.logger.Warn("extend session failed", "error", err)
		}
	}
	return user, true, nil
}

// NewSessionToken generates a 256-bit random session token.
func NewSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// TokenHash hashes a session token for storage.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// dummyHash is a precomputed Argon2id hash used to equalize timing for unknown users.
var dummyHash = "$argon2id$v=19$m=19456,t=2,p=1$Z3JvdW53b3JrLWR1bW15LXNhbHQ$Yp3JqNklzMkF5jfCGbMq1cYwOlkPQSiNb9CiMshxSy0"

func (s *Service) createSession(ctx context.Context, user store.User, ip, userAgent string) (store.User, SessionToken, string, error) {
	token, err := NewSessionToken()
	if err != nil {
		return user, SessionToken{}, "", err
	}
	expires := time.Now().Add(s.sessionTTL)
	err = s.sessions.Create(ctx, store.Session{
		UserID:    user.ID,
		TokenHash: TokenHash(token),
		ExpiresAt: expires,
		SourceIP:  clientIP(ip),
		UserAgent: truncate(userAgent, 300),
	})
	if err != nil {
		return user, SessionToken{}, "", err
	}
	return user, SessionToken{Token: token, ExpiresAt: expires}, token, nil
}

func (s *Service) auditEvent(ctx context.Context, actor store.User, entityType, entityID, entityLabel, action, ip, changes string) {
	e := store.AuditEvent{
		ActorUserID: actor.ID,
		ActorLabel:  actor.Username,
		EntityType:  entityType,
		EntityID:    entityID,
		EntityLabel: entityLabel,
		Action:      action,
		SourceIP:    clientIP(ip),
	}
	// changes column is NOT NULL: always persist at least an empty object.
	if changes == "" || changes == "{}" {
		changes = "{}"
	}
	e.Changes = []byte(changes)
	if err := s.audit.Record(ctx, e); err != nil {
		s.logger.Error("audit write failed", "error", err, "action", action)
	}
}

func clientIP(remoteAddr string) string {
	if ip, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return ip
	}
	return remoteAddr
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
