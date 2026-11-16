package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TagColors are the allowed badge colors for tags.
var TagColors = map[string]bool{
	"zinc": true, "blue": true, "green": true, "amber": true, "red": true,
	"violet": true, "cyan": true, "pink": true,
}

type Tags struct{ pool *pgxpool.Pool }

func NewTags(pool *pgxpool.Pool) *Tags { return &Tags{pool: pool} }

// List returns all tags with usage counts.
func (s *Tags) List(ctx context.Context) ([]Tag, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name, t.color,
		       (SELECT count(*) FROM entity_tags e WHERE e.tag_id = t.id)
		FROM tags t
		ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Usage); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Create inserts a tag; duplicate names map to ErrConflict.
func (s *Tags) Create(ctx context.Context, name, color string) (Tag, error) {
	var t Tag
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tags (name, color) VALUES ($1, $2)
		RETURNING id, name, color`,
		name, color,
	).Scan(&t.ID, &t.Name, &t.Color)
	if err != nil {
		return Tag{}, mapConstraint(err)
	}
	return t, nil
}

// Update renames/re-colors a tag.
func (s *Tags) Update(ctx context.Context, id, name, color string) (Tag, error) {
	var t Tag
	err := s.pool.QueryRow(ctx, `
		UPDATE tags SET name=$2, color=$3 WHERE id=$1
		RETURNING id, name, color`,
		id, name, color,
	).Scan(&t.ID, &t.Name, &t.Color)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tag{}, ErrNotFound
	}
	return t, mapConstraint(err)
}

// Delete removes a tag; entity_tags rows cascade via FK.
func (s *Tags) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tags WHERE id = $1`, id)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetEntityTags replaces the tag set for an entity. The caller must pass an
// entity_type from the allowed set (mirrors the entity_tags CHECK constraint).
func (s *Tags) SetEntityTags(ctx context.Context, entityType, entityID string, tagIDs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := setEntityTagsTx(ctx, tx, entityType, entityID, tagIDs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// setEntityTagsTx rewrites entity_tags for one entity inside an open transaction.
func setEntityTagsTx(ctx context.Context, tx pgx.Tx, entityType, entityID string, tagIDs []string) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM entity_tags WHERE entity_type = $1 AND entity_id = $2`, entityType, entityID); err != nil {
		return err
	}
	for _, tagID := range tagIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO entity_tags (entity_type, entity_id, tag_id) VALUES ($1, $2, $3)
			 ON CONFLICT DO NOTHING`, entityType, entityID, tagID); err != nil {
			return err
		}
	}
	return nil
}

// validEntityTypes mirrors the entity_tags.entity_type CHECK constraint.
var validEntityTypes = map[string]bool{
	"asset": true, "subnet": true, "service": true, "vlan": true, "ip_address": true, "location": true,
}

// EntityTypes exposes the allowed entity types for validation.
func EntityTypes() []string {
	return []string{"asset", "subnet", "service", "vlan", "ip_address", "location"}
}

func isValidEntityType(t string) bool { return validEntityTypes[t] }
