package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Session is a server-side session record. The cookie carries a random token; only its
// SHA-256 hash is persisted.
type Session struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	SourceIP  string
	UserAgent string
}

// SessionView is a client-safe projection of a session (no token hash).
type SessionView struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	SourceIP  string    `json:"source_ip"`
	UserAgent string    `json:"user_agent"`
}

type Sessions struct{ pool *pgxpool.Pool }

func NewSessions(pool *pgxpool.Pool) *Sessions { return &Sessions{pool: pool} }

// Create inserts a new session.
func (s *Sessions) Create(ctx context.Context, sess Session) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (user_id, token_hash, expires_at, source_ip, user_agent)
		VALUES ($1, $2, $3, $4, $5)`,
		sess.UserID, sess.TokenHash, sess.ExpiresAt, sess.SourceIP, sess.UserAgent)
	return err
}

// ActiveUserByTokenHash returns the user for a valid, unexpired session token hash.
func (s *Sessions) ActiveUserByTokenHash(ctx context.Context, tokenHash string) (User, time.Time, error) {
	var (
		u         User
		expiresAt time.Time
	)
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.display_name, u.role, u.created_at, u.updated_at, u.password_hash,
		       u.totp_enabled_at, u.totp_secret, u.totp_last_step,
		       s.expires_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`,
		tokenHash,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt, &u.UpdatedAt, &u.PasswordHash,
		&u.TOTPEnabledAt, &u.TOTPSecret, &u.TOTPLastStep, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, time.Time{}, ErrNotFound
	}
	if err != nil {
		return User{}, time.Time{}, err
	}
	return u, expiresAt, nil
}

// Extend pushes a session's expiry forward (sliding window).
func (s *Sessions) Extend(ctx context.Context, tokenHash string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sessions SET expires_at = $2 WHERE token_hash = $1`, tokenHash, expiresAt)
	return err
}

// Delete removes a session by token hash (logout).
func (s *Sessions) Delete(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

// DeleteOthersForUser revokes all sessions of a user except the provided token hash
// and returns how many were removed.
func (s *Sessions) DeleteOthersForUser(ctx context.Context, userID, keepTokenHash string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM sessions WHERE user_id = $1 AND token_hash <> $2`, userID, keepTokenHash)
	return tag.RowsAffected(), err
}

// ListForUser returns the user's sessions, newest first.
func (s *Sessions) ListForUser(ctx context.Context, userID string) ([]SessionView, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, created_at, expires_at, COALESCE(source_ip::text, ''), user_agent
		FROM sessions
		WHERE user_id = $1 AND expires_at > now()
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionView{}
	for rows.Next() {
		var v SessionView
		if err := rows.Scan(&v.ID, &v.CreatedAt, &v.ExpiresAt, &v.SourceIP, &v.UserAgent); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ByTokenHash resolves one active session view by token hash (for marking the
// current session in the list).
func (s *Sessions) ByTokenHash(ctx context.Context, tokenHash string) (SessionView, error) {
	var v SessionView
	err := s.pool.QueryRow(ctx, `
		SELECT id, created_at, expires_at, COALESCE(source_ip::text, ''), user_agent
		FROM sessions
		WHERE token_hash = $1 AND expires_at > now()`, tokenHash,
	).Scan(&v.ID, &v.CreatedAt, &v.ExpiresAt, &v.SourceIP, &v.UserAgent)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionView{}, ErrNotFound
	}
	return v, err
}

// DeleteAllForUser revokes every session of a user (admin password reset).
func (s *Sessions) DeleteAllForUser(ctx context.Context, userID string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return tag.RowsAffected(), err
}

// DeleteExpired removes expired sessions (invoked periodically).
func (s *Sessions) DeleteExpired(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now()`)
	return err
}
