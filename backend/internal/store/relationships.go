package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AssetRelationship is a typed, directional edge between two assets. Physical
// connections may carry interface and port metadata (topology rendering).
type AssetRelationship struct {
	ID        string    `json:"id"`
	SourceID  string    `json:"source_asset_id"`
	TargetID  string    `json:"target_asset_id"`
	Kind      string    `json:"kind"`
	Note      string    `json:"note"`
	SourceInterfaceID *string `json:"source_interface_id,omitempty"`
	TargetInterfaceID *string `json:"target_interface_id,omitempty"`
	SourcePort string   `json:"source_port"`
	TargetPort string   `json:"target_port"`
	// Direction relative to the asset the relationship was listed for.
	Direction string `json:"direction"` // "outgoing" | "incoming"
	Other     AssetStub `json:"other_asset"`
	CreatedAt time.Time `json:"created_at"`
}

type Relationships struct{ pool *pgxpool.Pool }

func NewRelationships(pool *pgxpool.Pool) *Relationships { return &Relationships{pool: pool} }

// RelationshipMeta carries optional connection metadata for physical links.
type RelationshipMeta struct {
	Note              string
	SourceInterfaceID string
	TargetInterfaceID string
	SourcePort        string
	TargetPort        string
}

// Create links source→target with kind validation done by the caller. Cycle detection
// for hierarchical kinds (runs_on, depends_on) rejects edges that would close a loop.
func (s *Relationships) Create(ctx context.Context, sourceID, targetID, kind string, meta RelationshipMeta) (AssetRelationship, error) {
	if kind == "runs_on" || kind == "depends_on" {
		cyclic, err := s.wouldCycle(ctx, sourceID, targetID, kind)
		if err != nil {
			return AssetRelationship{}, err
		}
		if cyclic {
			return AssetRelationship{}, ErrCycle
		}
	}

	var rel AssetRelationship
	err := s.pool.QueryRow(ctx, `
		INSERT INTO asset_relationships
			(source_asset_id, target_asset_id, kind, note, source_interface_id, target_interface_id, source_port, target_port)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at`,
		sourceID, targetID, kind, meta.Note,
		nullableID(meta.SourceInterfaceID), nullableID(meta.TargetInterfaceID),
		meta.SourcePort, meta.TargetPort,
	).Scan(&rel.ID, &rel.CreatedAt)
	if err != nil {
		return AssetRelationship{}, mapConstraint(err)
	}
	rel.SourceID, rel.TargetID, rel.Kind, rel.Note = sourceID, targetID, kind, meta.Note
	rel.SourcePort, rel.TargetPort = meta.SourcePort, meta.TargetPort
	rel.Direction = "outgoing"
	return rel, nil
}

// Update edits the note and connection metadata of an existing edge.
func (s *Relationships) Update(ctx context.Context, id string, meta RelationshipMeta) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE asset_relationships SET note=$2, source_interface_id=$3, target_interface_id=$4,
		       source_port=$5, target_port=$6
		WHERE id=$1`,
		id, meta.Note, nullableID(meta.SourceInterfaceID), nullableID(meta.TargetInterfaceID),
		meta.SourcePort, meta.TargetPort)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// wouldCycle reports whether adding source→target of kind closes a directed cycle.
func (s *Relationships) wouldCycle(ctx context.Context, sourceID, targetID, kind string) (bool, error) {
	var cyclic bool
	err := s.pool.QueryRow(ctx, `
		WITH RECURSIVE reach AS (
			SELECT $2::uuid AS node
			UNION
			SELECT r.target_asset_id
			FROM asset_relationships r
			JOIN reach ON r.source_asset_id = reach.node
			WHERE r.kind = $3
		)
		SELECT EXISTS(SELECT 1 FROM reach WHERE node = $1::uuid)`,
		sourceID, targetID, kind,
	).Scan(&cyclic)
	return cyclic, err
}

// Delete removes a relationship edge.
func (s *Relationships) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM asset_relationships WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListForAsset returns both directions, each annotated with the peer asset stub.
func (s *Relationships) ListForAsset(ctx context.Context, assetID string) ([]AssetRelationship, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.source_asset_id, r.target_asset_id, r.kind, r.note, r.created_at,
		       TO_JSONB(r.source_interface_id) #>> '{}', TO_JSONB(r.target_interface_id) #>> '{}',
		       r.source_port, r.target_port,
		       t.id, t.name, t.hostname, t2.label, t2.category, t.status
		FROM asset_relationships r
		JOIN assets t ON t.id = CASE WHEN r.source_asset_id = $1 THEN r.target_asset_id ELSE r.source_asset_id END
		JOIN asset_types t2 ON t2.id = t.type_id
		WHERE r.source_asset_id = $1 OR r.target_asset_id = $1
		ORDER BY r.kind, t.name`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rels := []AssetRelationship{}
	for rows.Next() {
		var rel AssetRelationship
		if err := rows.Scan(&rel.ID, &rel.SourceID, &rel.TargetID, &rel.Kind, &rel.Note, &rel.CreatedAt,
			&rel.SourceInterfaceID, &rel.TargetInterfaceID, &rel.SourcePort, &rel.TargetPort,
			&rel.Other.ID, &rel.Other.Name, &rel.Other.Hostname, &rel.Other.TypeLabel,
			&rel.Other.Category, &rel.Other.Status); err != nil {
			return nil, err
		}
		if rel.SourceID == assetID {
			rel.Direction = "outgoing"
		} else {
			rel.Direction = "incoming"
		}
		rels = append(rels, rel)
	}
	return rels, rows.Err()
}

// All returns every edge in the inventory (topology graph source).
func (s *Relationships) All(ctx context.Context) ([]AssetRelationship, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.source_asset_id, r.target_asset_id, r.kind, r.note,
		       TO_JSONB(r.source_interface_id) #>> '{}', TO_JSONB(r.target_interface_id) #>> '{}',
		       r.source_port, r.target_port, r.created_at
		FROM asset_relationships r
		ORDER BY r.kind`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssetRelationship{}
	for rows.Next() {
		var rel AssetRelationship
		if err := rows.Scan(&rel.ID, &rel.SourceID, &rel.TargetID, &rel.Kind, &rel.Note,
			&rel.SourceInterfaceID, &rel.TargetInterfaceID, &rel.SourcePort, &rel.TargetPort,
			&rel.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	return out, rows.Err()
}

// GetByID exists for ownership checks and topology edges.
func (s *Relationships) GetByID(ctx context.Context, id string) (AssetRelationship, error) {
	var rel AssetRelationship
	err := s.pool.QueryRow(ctx, `
		SELECT id, source_asset_id, target_asset_id, kind, note,
		       TO_JSONB(source_interface_id) #>> '{}', TO_JSONB(target_interface_id) #>> '{}',
		       source_port, target_port, created_at
		FROM asset_relationships WHERE id = $1`, id,
	).Scan(&rel.ID, &rel.SourceID, &rel.TargetID, &rel.Kind, &rel.Note,
		&rel.SourceInterfaceID, &rel.TargetInterfaceID, &rel.SourcePort, &rel.TargetPort, &rel.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AssetRelationship{}, ErrNotFound
	}
	return rel, err
}
