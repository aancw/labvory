package store

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrCycle is returned when a hierarchy edge would close a loop.
var ErrCycle = errors.New("hierarchy cycle")

// Location is a physical placement node (rack, room, building) forming a tree.
type Location struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	ParentID    *string   `json:"parent_id,omitempty"`
	AssetCount  int       `json:"asset_count"`
	ChildCount  int       `json:"child_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Locations struct{ pool *pgxpool.Pool }

func NewLocations(pool *pgxpool.Pool) *Locations { return &Locations{pool: pool} }

// Get fetches one location by id.
func (s *Locations) Get(ctx context.Context, id string) (Location, error) {
	var l Location
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, slug, description, TO_JSONB(parent_id) #>> '{}', created_at, updated_at
		FROM locations WHERE id = $1`, id,
	).Scan(&l.ID, &l.Name, &l.Slug, &l.Description, &l.ParentID, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Location{}, ErrNotFound
	}
	return l, err
}

// List returns all locations with usage counts; clients assemble the tree.
func (s *Locations) List(ctx context.Context) ([]Location, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.id, l.name, l.slug, l.description,
		       TO_JSONB(l.parent_id) #>> '{}',
		       (SELECT count(*) FROM assets a WHERE a.location_id = l.id),
		       (SELECT count(*) FROM locations c WHERE c.parent_id = l.id),
		       l.created_at, l.updated_at
		FROM locations l
		ORDER BY l.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Location{}
	for rows.Next() {
		var l Location
		if err := rows.Scan(&l.ID, &l.Name, &l.Slug, &l.Description, &l.ParentID,
			&l.AssetCount, &l.ChildCount, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Create inserts a location with a slug derived from the name.
func (s *Locations) Create(ctx context.Context, name, description string, parentID *string) (Location, error) {
	slug := slugify(name)
	var l Location
	err := s.pool.QueryRow(ctx, `
		INSERT INTO locations (name, slug, description, parent_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, slug, created_at, updated_at`,
		name, slug, description, nullableID(deref(parentID)),
	).Scan(&l.ID, &l.Slug, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return Location{}, mapConstraint(err)
	}
	l.Name, l.Description, l.ParentID = name, description, parentID
	return l, nil
}

// Update renames/re-describes/re-parents a location; parent cycles are rejected.
func (s *Locations) Update(ctx context.Context, id, name, description string, parentID *string) (Location, error) {
	if parentID != nil && *parentID != "" {
		cyclic, err := s.wouldCycle(ctx, id, *parentID)
		if err != nil {
			return Location{}, err
		}
		if cyclic {
			return Location{}, ErrCycle
		}
	} else {
		parentID = nil
	}

	var l Location
	err := s.pool.QueryRow(ctx, `
		UPDATE locations SET name=$2, slug=$3, description=$4, parent_id=$5, updated_at=now()
		WHERE id=$1
		RETURNING id, name, slug, description, TO_JSONB(parent_id) #>> '{}', created_at, updated_at`,
		id, name, slugify(name), description, nullableID(deref(parentID)),
	).Scan(&l.ID, &l.Name, &l.Slug, &l.Description, &l.ParentID, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Location{}, ErrNotFound
	}
	return l, mapConstraint(err)
}

// wouldCycle reports whether making `parentID` the parent of `id` closes a loop.
func (s *Locations) wouldCycle(ctx context.Context, id, parentID string) (bool, error) {
	var cyclic bool
	err := s.pool.QueryRow(ctx, `
		WITH RECURSIVE reach AS (
			SELECT $2::uuid AS node
			UNION
			SELECT l.parent_id
			FROM locations l
			JOIN reach ON l.id = reach.node
		)
		SELECT EXISTS(SELECT 1 FROM reach WHERE node = $1::uuid)`,
		id, parentID,
	).Scan(&cyclic)
	return cyclic, err
}

// Delete removes a location; refuses when children or assets are attached.
func (s *Locations) Delete(ctx context.Context, id string) error {
	var children, assets int
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM locations c WHERE c.parent_id = $1),
			(SELECT count(*) FROM assets a WHERE a.location_id = $1)`, id,
	).Scan(&children, &assets)
	if err != nil {
		return err
	}
	if children > 0 || assets > 0 {
		return ErrHasChildren
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM locations WHERE id = $1`, id)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "location"
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// AssetType is a device category (reference data seeded by migrations).
type AssetType struct {
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Icon     string `json:"icon"`
}

// ListAssetTypes returns the type catalogue for forms and filters.
func (s *Locations) ListAssetTypes(ctx context.Context) ([]AssetType, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, slug, label, category, icon FROM asset_types
		ORDER BY category, label`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssetType{}
	for rows.Next() {
		var t AssetType
		if err := rows.Scan(&t.ID, &t.Slug, &t.Label, &t.Category, &t.Icon); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
