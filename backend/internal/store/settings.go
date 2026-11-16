package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Setting is a single instance-level setting entry.
type Setting struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type Settings struct{ pool *pgxpool.Pool }

func NewSettings(pool *pgxpool.Pool) *Settings { return &Settings{pool: pool} }

// All returns every setting keyed by name.
func (s *Settings) All(ctx context.Context) (map[string]json.RawMessage, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, value FROM app_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Upsert writes one setting with actor attribution.
func (s *Settings) Upsert(ctx context.Context, key string, value json.RawMessage, updatedBy string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now(), updated_by = EXCLUDED.updated_by`,
		key, value, nullableID(updatedBy))
	return err
}
