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
		SELECT id, username, display_name, role, created_at, updated_at, password_hash
		FROM users WHERE username = $1`, username))
}

// GetByID fetches a user by id.
func (s *Users) GetByID(ctx context.Context, id string) (User, error) {
	return s.scanOne(s.pool.QueryRow(ctx, `
		SELECT id, username, display_name, role, created_at, updated_at, password_hash
		FROM users WHERE id = $1`, id))
}

// UpdatePassword replaces a user's password hash and bumps updated_at.
func (s *Users) UpdatePassword(ctx context.Context, id, passwordHash string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, passwordHash)
	return err
}

func (s *Users) scanOne(row pgx.Row) (User, error) {
	u := User{}
	err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.CreatedAt, &u.UpdatedAt, &u.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}
