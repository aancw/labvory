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

	"github.com/petruknisme/lanvory/internal/store"
)

var (
	// ErrInvalidCredentials is returned for unknown users or bad passwords (same message both ways).
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidUsername / ErrTakenUsername guard the setup flow.
	ErrInvalidUsername = errors.New("invalid username")
	ErrTakenUsername   = errors.New("username already taken")
	ErrNoUsersAllowed  = errors.New("setup is only available before the first administrator exists")
	ErrWrongPassword   = errors.New("current password is incorrect")

	usernameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{2,31}$`)
)

// Service implements authentication workflows on top of the store.
type Service struct {
	users      *store.Users
	sessions   *store.Sessions
	audit      *store.AuditEvents
	logger     *slog.Logger
	sessionTTL time.Duration
}

// NewService wires the auth service to its stores.
func NewService(users *store.Users, sessions *store.Sessions, audit *store.AuditEvents, logger *slog.Logger, sessionTTL time.Duration) *Service {
	return &Service{users: users, sessions: sessions, audit: audit, logger: logger, sessionTTL: sessionTTL}
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

// Login verifies credentials and issues a session. Timing of a missing user is masked by
// verifying against a deterministic dummy hash.
func (s *Service) Login(ctx context.Context, username, password, ip, userAgent string) (store.User, SessionToken, error) {
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

	user, token, sess, err := s.createSession(ctx, user, ip, userAgent)
	if err != nil {
		return store.User{}, zero, err
	}
	s.auditEvent(ctx, user, "auth", user.ID, user.Username, "login.success", ip, "{}")
	return user, SessionToken{Token: sess, ExpiresAt: token.ExpiresAt}, nil
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
	if err := s.sessions.DeleteOthersForUser(ctx, user.ID, currentTokenHash); err != nil {
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
