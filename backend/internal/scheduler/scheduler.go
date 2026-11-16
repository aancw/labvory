// Package scheduler runs the periodic background work Lanvory performs on
// its own: integration syncs that are due (per-connector auto-sync interval)
// and, later, service-endpoint health checks. Everything runs on one loop —
// homelab scale means sequential execution is plenty and can never overlap.
package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/lanvory/internal/integrations"
	"github.com/aancw/lanvory/internal/store"
)

const tickInterval = 30 * time.Second

// Scheduler is the single background worker.
type Scheduler struct {
	pool         *pgxpool.Pool
	integrations *store.Integrations
	audit        *store.AuditEvents
	runner       *integrations.Runner
	logger       *slog.Logger
}

func New(pool *pgxpool.Pool, runner *integrations.Runner, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		pool:         pool,
		integrations: store.NewIntegrations(pool),
		audit:        store.NewAuditEvents(pool),
		runner:       runner,
		logger:       logger,
	}
}

// Run blocks until ctx is canceled, ticking every 30s (plus once at startup).
func (s *Scheduler) Run(ctx context.Context) {
	s.tick(ctx)
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	s.syncDueIntegrations(ctx)
}

// syncDueIntegrations runs every enabled integration whose auto-sync interval
// has elapsed. Runner.Run records the run history and last_sync_at itself.
func (s *Scheduler) syncDueIntegrations(ctx context.Context) {
	items, err := s.integrations.ListScheduled(ctx)
	if err != nil {
		s.logger.Error("scheduler: list scheduled integrations failed", "error", err)
		return
	}
	// Bookkeeping (run history, last_sync_at) must survive scheduler shutdown.
	book := context.WithoutCancel(ctx)
	for _, integ := range items {
		if !due(integ, time.Now()) {
			continue
		}
		summary, syncErr := s.runner.Run(book, integ)
		if syncErr != nil {
			s.logger.Warn("scheduled sync failed", "integration", integ.Name, "error", syncErr)
			s.recordAudit(book, "integration", integ.ID, integ.Name, "sync_failed",
				map[string]any{"error": syncErr.Error(), "scheduled": true})
			continue
		}
		s.recordAudit(book, "integration", integ.ID, integ.Name, "synced", summary)
	}
}

// due reports whether an integration's interval has elapsed since its last sync.
func due(integ store.Integration, now time.Time) bool {
	if integ.SyncIntervalSeconds == nil {
		return false
	}
	if integ.LastSyncAt == nil {
		return true
	}
	return now.Sub(*integ.LastSyncAt) >= time.Duration(*integ.SyncIntervalSeconds)*time.Second
}

// audit records a scheduler-initiated event; failures are logged, never fatal.
func (s *Scheduler) recordAudit(ctx context.Context, entityType, entityID, entityLabel, action string, changes any) {
	payload := []byte("{}")
	if changes != nil {
		payload, _ = json.Marshal(changes)
	}
	err := s.audit.Record(ctx, store.AuditEvent{
		ActorLabel:  "scheduler",
		EntityType:  entityType,
		EntityID:    entityID,
		EntityLabel: entityLabel,
		Action:      action,
		Changes:     payload,
	})
	if err != nil {
		s.logger.Error("scheduler: audit write failed", "error", err, "action", action)
	}
}
