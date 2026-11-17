package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditEvent records a change or security-relevant action.
type AuditEvent struct {
	ID          string          `json:"id"`
	OccurredAt  time.Time       `json:"occurred_at"`
	ActorUserID string          `json:"actor_user_id,omitempty"`
	ActorLabel  string          `json:"actor_label"`
	EntityType  string          `json:"entity_type"`
	EntityID    string          `json:"entity_id,omitempty"`
	EntityLabel string          `json:"entity_label"`
	Action      string          `json:"action"`
	Changes     json.RawMessage `json:"changes,omitempty"`
	SourceIP    string          `json:"source_ip,omitempty"`
}

type AuditEvents struct{ pool *pgxpool.Pool }

func NewAuditEvents(pool *pgxpool.Pool) *AuditEvents { return &AuditEvents{pool: pool} }

// Record persists an audit event. Failures are surfaced to the caller for logging but never
// block the primary operation.
func (s *AuditEvents) Record(ctx context.Context, e AuditEvent) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_events
			(actor_user_id, actor_label, entity_type, entity_id, entity_label, action, changes, source_ip)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		nullableID(e.ActorUserID), e.ActorLabel, e.EntityType, nullableID(e.EntityID),
		e.EntityLabel, e.Action, e.Changes, nullableID(e.SourceIP))
	return err
}

// Prune deletes audit events older than the retention window and returns how
// many rows were removed. Used by the scheduler to honor audit.retention_days.
func (s *AuditEvents) Prune(ctx context.Context, olderThanDays int) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM audit_events WHERE occurred_at < now() - make_interval(days => $1)`, olderThanDays)
	return tag.RowsAffected(), err
}

// List returns the most recent audit events, optionally filtered by entity type
// and action, with a hard upper bound.
func (s *AuditEvents) List(ctx context.Context, limit, offset int, entityType, action string) ([]AuditEvent, int, error) {
	where := ""
	args := []any{}
	if entityType != "" {
		args = append(args, entityType)
		where += " WHERE entity_type = $" + itoa(len(args))
	}
	if action != "" {
		args = append(args, "%"+action+"%")
		where += whereStart(where) + " action ILIKE $" + itoa(len(args))
	}

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_events`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, occurred_at, COALESCE(actor_user_id::text, ''), actor_label,
		       entity_type, COALESCE(entity_id::text, ''), entity_label, action, changes,
		       COALESCE(source_ip::text, '')
		FROM audit_events` + where + ` ORDER BY occurred_at DESC LIMIT $` + itoa(len(args)+1) + ` OFFSET $` + itoa(len(args)+2)
	args = append(args, limit, offset)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	events := make([]AuditEvent, 0, limit)
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorUserID, &e.ActorLabel,
			&e.EntityType, &e.EntityID, &e.EntityLabel, &e.Action, &e.Changes, &e.SourceIP); err != nil {
			return nil, 0, err
		}
		events = append(events, e)
	}
	return events, total, rows.Err()
}

// ListForEntity returns audit events for one entity (entity-specific history).
func (s *AuditEvents) ListForEntity(ctx context.Context, entityType, entityID string, limit int) ([]AuditEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, occurred_at, COALESCE(actor_user_id::text, ''), actor_label,
		       entity_type, COALESCE(entity_id::text, ''), entity_label, action, changes,
		       COALESCE(source_ip::text, '')
		FROM audit_events
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY occurred_at DESC
		LIMIT $3`, entityType, entityID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorUserID, &e.ActorLabel,
			&e.EntityType, &e.EntityID, &e.EntityLabel, &e.Action, &e.Changes, &e.SourceIP); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
