package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Token purposes. A token only authenticates the surface it was issued for:
// "mcp" tokens work on /mcp, "api" tokens work on the /api/v1 REST endpoints.
const (
	TokenPurposeMCP = "mcp"
	TokenPurposeAPI = "api"
)

// ValidTokenPurpose reports whether p is an accepted purpose value.
func ValidTokenPurpose(p string) bool {
	return p == TokenPurposeMCP || p == TokenPurposeAPI
}

// APIToken is a bearer credential for machine clients (MCP agents, scripts).
// Tokens are stored hashed; the plaintext is returned only at creation.
type APIToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"-"`
	Purpose    string     `json:"purpose"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

// APITokenCreated pairs the stored token with the one-time plaintext.
type APITokenCreated struct {
	APIToken
	// Token is the plaintext bearer value (prefix "lv_"), shown exactly once.
	Token string `json:"token"`
}

// ErrTokenRevoked is returned when resolving a revoked token.
var ErrTokenRevoked = errors.New("api token revoked")

type APITokens struct{ pool *pgxpool.Pool }

func NewAPITokens(pool *pgxpool.Pool) *APITokens { return &APITokens{pool: pool} }

const apiTokenColumns = `id, user_id, name, token_hash, purpose, created_at, last_used_at, revoked_at`

func scanAPIToken(row pgx.Row) (APIToken, error) {
	var t APIToken
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.Purpose, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
	return t, err
}

// NewAPITokenValue generates a fresh plaintext bearer token (lv_-prefixed,
// 256 bits of entropy) and its SHA-256 hex hash for storage.
func NewAPITokenValue() (plaintext, hash string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	plaintext = "lv_" + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, hex.EncodeToString(sum[:]), nil
}

// HashAPIToken returns the SHA-256 hex hash of a plaintext token.
func HashAPIToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// Create stores a new token for the user and returns it with the plaintext.
func (s *APITokens) Create(ctx context.Context, userID, name, purpose string) (APITokenCreated, error) {
	plaintext, hash, err := NewAPITokenValue()
	if err != nil {
		return APITokenCreated{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO api_tokens (user_id, name, token_hash, purpose)
		VALUES ($1, $2, $3, $4)
		RETURNING `+apiTokenColumns, userID, name, hash, purpose)
	t, err := scanAPIToken(row)
	if err != nil {
		return APITokenCreated{}, err
	}
	return APITokenCreated{APIToken: t, Token: plaintext}, nil
}

// List returns the caller's tokens, newest first.
func (s *APITokens) List(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+apiTokenColumns+`
		FROM api_tokens WHERE user_id = $1
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Revoke marks a token revoked; ErrNotFound when the id is unknown or not
// owned by the user.
func (s *APITokens) Revoke(ctx context.Context, id, userID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE api_tokens SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Resolve maps a plaintext bearer token to its owning user. Revoked tokens
// never resolve.
func (s *APITokens) Resolve(ctx context.Context, plaintext string) (APIToken, User, error) {
	var t APIToken
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, t.user_id, t.name, t.token_hash, t.purpose, t.created_at, t.last_used_at, t.revoked_at,
		       u.id, u.username, u.display_name, u.role
		FROM api_tokens t
		JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1 AND t.revoked_at IS NULL`, HashAPIToken(plaintext)).
		Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.Purpose, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt,
			&u.ID, &u.Username, &u.DisplayName, &u.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIToken{}, User{}, ErrNotFound
	}
	if err != nil {
		return APIToken{}, User{}, err
	}
	return t, u, nil
}

// Touch updates last_used_at (best effort; failures are ignored).
func (s *APITokens) Touch(ctx context.Context, id string) {
	_, _ = s.pool.Exec(ctx, `UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, id)
}
