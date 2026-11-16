// Package scheduler runs the periodic background work Lanvory performs on
// its own: integration syncs that are due (per-connector auto-sync interval)
// and, later, service-endpoint health checks. Everything runs on one loop —
// homelab scale means sequential execution is plenty and can never overlap.
package scheduler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/lanvory/internal/integrations"
	"github.com/aancw/lanvory/internal/store"
)

const (
	tickInterval       = 30 * time.Second
	healthCheckEvery   = 60 * time.Second // per-endpoint re-check cadence
	healthProbeTimeout = 8 * time.Second
)

// Scheduler is the single background worker.
type Scheduler struct {
	pool         *pgxpool.Pool
	integrations *store.Integrations
	services     *store.Services
	audit        *store.AuditEvents
	runner       *integrations.Runner
	logger       *slog.Logger
}

func New(pool *pgxpool.Pool, runner *integrations.Runner, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		pool:         pool,
		integrations: store.NewIntegrations(pool),
		services:     store.NewServices(pool),
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
	s.checkDueEndpoints(ctx)
}

// checkDueEndpoints probes every service endpoint with health checks enabled
// and records the observed result. Up↔down transitions are audited so the
// Activity page shows when a service flapped.
func (s *Scheduler) checkDueEndpoints(ctx context.Context) {
	endpoints, err := s.services.DueHealthEndpoints(ctx, healthCheckEvery)
	if err != nil {
		s.logger.Error("scheduler: list due health endpoints failed", "error", err)
		return
	}
	book := context.WithoutCancel(ctx)
	client := &http.Client{Timeout: healthProbeTimeout}
	for _, ep := range endpoints {
		wasUp := wasEndpointUp(ep)
		status, latency, errText := probeHTTP(ctx, client, ep.URL)
		if err := s.services.RecordHealthCheck(book, ep.ID, status, latency, errText); err != nil {
			s.logger.Error("scheduler: record health check failed", "error", err, "endpoint", ep.ID)
			continue
		}
		isUp := errText == "" && status >= 200 && status < 400
		if ep.LastCheckedAt != nil && isUp != wasUp {
			action, detail := "health_down", map[string]any{
				"url": ep.URL, "status": status, "error": errText,
			}
			if isUp {
				action = "health_recovered"
				detail = map[string]any{"url": ep.URL, "status": status, "latency_ms": latency}
			}
			s.recordAudit(book, "service_endpoint", ep.ID, ep.ServiceName+" · "+ep.Label, action, detail)
		}
	}
}

// wasEndpointUp reconstructs the endpoint's previous observed state.
func wasEndpointUp(ep store.HealthEndpoint) bool {
	if ep.LastCheckedAt == nil {
		return true // never checked: treated as up so only real failures notify
	}
	return ep.LastError == "" && ep.LastStatus != nil && *ep.LastStatus >= 200 && *ep.LastStatus < 400
}

// probeHTTP performs one GET and returns the observed status/latency/error.
func probeHTTP(ctx context.Context, client *http.Client, url string) (statusCode, latencyMS int, errText string) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err.Error()
	}
	req.Header.Set("User-Agent", "lanvory-health-check")
	resp, err := client.Do(req)
	latencyMS = int(time.Since(start).Milliseconds())
	if err != nil {
		return 0, latencyMS, err.Error()
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, latencyMS, ""
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
