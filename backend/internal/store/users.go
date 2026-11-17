// Package store contains SQL-backed queries per aggregate.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a query matches no rows.
var ErrNotFound = errors.New("not found")

// User is an application account.
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	PasswordHash string    `json:"-"` // never serialized
	// Two-factor state. TOTPSecret is stored encrypted; a non-nil
	// TOTPEnabledAt means the authenticator is active (otherwise any stored
	// secret is a pending setup enrollment).
	TOTPEnabledAt *time.Time `json:"totp_enabled_at,omitempty"`
	TOTPSecret    string     `json:"-"`
	TOTPLastStep  int64      `json:"-"`
}

type Users struct{ pool *pgxpool.Pool }

func NewUsers(pool *pgxpool.Pool) *Users { return &Users{pool: pool} }

// Count returns the total number of users (zero during first-run setup).
func (s *Users) Count(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// Create inserts a new user.
func (s *Users) Create(ctx context.Context, username, displayName, passwordHash, role string) (User, error) {
	u := User{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (username, display_name, password_hash, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id, username, display_name, role, created_at, updated_at, password_hash`,
		username, displayName, passwordHash, role,
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt, &u.UpdatedAt, &u.PasswordHash)
	if err != nil {
		return User{}, mapConstraint(err)
	}
	return u, nil
}

// GetByUsername fetches a user by exact username.
func (s *Users) GetByUsername(ctx context.Context, username string) (User, error) {
	return s.scanOne(s.pool.QueryRow(ctx, `
		SELECT id, username, display_name, role, created_at, updated_at, password_hash,
		       totp_enabled_at, totp_secret, totp_last_step
		FROM users WHERE username = $1`, username))
}

// GetByID fetches a user by id.
func (s *Users) GetByID(ctx context.Context, id string) (User, error) {
	return s.scanOne(s.pool.QueryRow(ctx, `
		SELECT id, username, display_name, role, created_at, updated_at, password_hash,
		       totp_enabled_at, totp_secret, totp_last_step
		FROM users WHERE id = $1`, id))
}

// UpdatePassword replaces a user's password hash and bumps updated_at.
func (s *Users) UpdatePassword(ctx context.Context, id, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, passwordHash)
	return err
}

// SetTOTPPending stores an encrypted enrollment secret while 2FA is not yet
// confirmed. Any previous enrollment (pending or active) is replaced.
func (s *Users) SetTOTPPending(ctx context.Context, id, encryptedSecret string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET totp_secret = $2, totp_enabled_at = NULL, totp_last_step = 0,
		       updated_at = now()
		WHERE id = $1`, id, encryptedSecret)
	return err
}

// EnableTOTP confirms the pending enrollment: the stored secret becomes the
// active one and the matched step seeds the replay guard.
func (s *Users) EnableTOTP(ctx context.Context, id string, matchedStep int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET totp_enabled_at = now(), totp_last_step = $2, updated_at = now()
		WHERE id = $1 AND totp_secret <> ''`, id, matchedStep)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DisableTOTP removes the authenticator entirely.
func (s *Users) DisableTOTP(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET totp_secret = '', totp_enabled_at = NULL, totp_last_step = 0,
		       updated_at = now()
		WHERE id = $1`, id)
	return err
}

// AdvanceTOTPStep records the matched step and refuses a replay: it succeeds
// only when the stored guard is strictly older. Returns false on replay.
func (s *Users) AdvanceTOTPStep(ctx context.Context, id string, step int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET totp_last_step = $2 WHERE id = $1 AND totp_last_step < $2`,
		id, step)
	return tag.RowsAffected() == 1, err
}

// List returns all accounts, oldest first (password hash is never serialized).
func (s *Users) List(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, display_name, role, created_at, updated_at, password_hash,
		       totp_enabled_at, totp_secret, totp_last_step
		FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt, &u.UpdatedAt,
			&u.PasswordHash, &u.TOTPEnabledAt, &u.TOTPSecret, &u.TOTPLastStep); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountAdmins returns the number of admin accounts (last-admin guard).
func (s *Users) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE role = 'admin'`).Scan(&n)
	return n, err
}

// UpdateProfile changes an account's display name and role.
func (s *Users) UpdateProfile(ctx context.Context, id, displayName, role string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET display_name = $2, role = $3, updated_at = now() WHERE id = $1`,
		id, displayName, role)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Delete removes an account; its sessions cascade.
func (s *Users) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Users) scanOne(row pgx.Row) (User, error) {
	u := User{}
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt, &u.UpdatedAt,
		&u.PasswordHash, &u.TOTPEnabledAt, &u.TOTPSecret, &u.TOTPLastStep)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}
