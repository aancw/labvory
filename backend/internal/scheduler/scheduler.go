// Package scheduler runs the periodic background work Lanvory performs on
// its own: integration syncs that are due (per-connector auto-sync interval)
// and, later, service-endpoint health checks. Everything runs on one loop —
// homelab scale means sequential execution is plenty and can never overlap.
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/lanvory/internal/integrations"
	"github.com/aancw/lanvory/internal/notify"
	"github.com/aancw/lanvory/internal/store"
)

const (
	tickInterval       = 30 * time.Second
	healthCheckEvery   = 60 * time.Second // per-endpoint re-check cadence
	healthProbeTimeout = 8 * time.Second

	// Probe history retention and prune cadence: seven days is plenty for
	// uptime trends and keeps the checks table bounded.
	checksRetention  = 7 * 24 * time.Hour
	checksPruneEvery = time.Hour

	// Certificates expiring within this window are surfaced on the dashboard
	// and announced once via webhook.
	certWarnDays = 30

	auditPruneEvery = 24 * time.Hour
)

// Scheduler is the single background worker.
type Scheduler struct {
	pool         *pgxpool.Pool
	integrations *store.Integrations
	services     *store.Services
	settings     *store.Settings
	audit        *store.AuditEvents
	runner       *integrations.Runner
	logger       *slog.Logger

	lastChecksPrune time.Time
	lastAuditPrune  time.Time
}

func New(pool *pgxpool.Pool, runner *integrations.Runner, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		pool:         pool,
		integrations: store.NewIntegrations(pool),
		services:     store.NewServices(pool),
		settings:     store.NewSettings(pool),
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
	hook, retentionDays := s.loadSettings(ctx)
	s.syncDueIntegrations(ctx, hook)
	s.checkDueEndpoints(ctx, hook)
	s.pruneHistory(ctx, retentionDays)
}

// loadSettings reads the notification webhook and audit retention once per
// tick. Empty webhook = notifications disabled; retention 0 = keep forever.
func (s *Scheduler) loadSettings(ctx context.Context) (webhookURL string, retentionDays int) {
	all, err := s.settings.All(ctx)
	if err != nil {
		s.logger.Error("scheduler: load settings failed", "error", err)
		return "", 0
	}
	if raw := all["notifications.webhook_url"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &webhookURL)
	}
	if raw := all["audit.retention_days"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &retentionDays)
	}
	return webhookURL, retentionDays
}

// notify delivers one webhook message; delivery problems are logged and
// never interrupt the tick.
func (s *Scheduler) notify(ctx context.Context, url, title, message string) {
	if url == "" {
		return
	}
	if err := notify.Post(context.WithoutCancel(ctx), url, title, message); err != nil {
		s.logger.Warn("scheduler: webhook delivery failed", "error", err, "title", title)
	}
}

// checkDueEndpoints probes every service endpoint with health checks enabled
// and records the observed result. Up↔down transitions and TLS-certificate
// expiry transitions are audited and notified so the Activity page shows when
// a service flapped or a certificate crossed the warning threshold.
func (s *Scheduler) checkDueEndpoints(ctx context.Context, hook string) {
	endpoints, err := s.services.DueHealthEndpoints(ctx, healthCheckEvery)
	if err != nil {
		s.logger.Error("scheduler: list due health endpoints failed", "error", err)
		return
	}
	book := context.WithoutCancel(ctx)
	client := &http.Client{Timeout: healthProbeTimeout}
	for _, ep := range endpoints {
		wasUp := wasEndpointUp(ep)
		status, latency, errText, cert := probeHTTP(ctx, client, ep.URL)
		if err := s.services.RecordHealthCheck(book, ep.ID, status, latency, errText, cert); err != nil {
			s.logger.Error("scheduler: record health check failed", "error", err, "endpoint", ep.ID)
			continue
		}
		isUp := errText == "" && status >= 200 && status < 400
		if err := s.services.RecordEndpointCheck(book, ep.ID, isUp, status, latency, errText); err != nil {
			s.logger.Error("scheduler: record endpoint check failed", "error", err, "endpoint", ep.ID)
		}
		if ep.LastCheckedAt != nil && isUp != wasUp {
			action, detail := "health_down", map[string]any{
				"url": ep.URL, "status": status, "error": errText,
			}
			title, message := ep.ServiceName+" is down", ep.URL
			if errText != "" {
				message = ep.URL + " — " + errText
			} else {
				message = fmt.Sprintf("%s — HTTP %d", ep.URL, status)
			}
			if isUp {
				action = "health_recovered"
				detail = map[string]any{"url": ep.URL, "status": status, "latency_ms": latency}
				title, message = ep.ServiceName+" recovered", fmt.Sprintf("%s is answering again (HTTP %d, %d ms).", ep.URL, status, latency)
			}
			s.recordAudit(book, "service_endpoint", ep.ID, ep.ServiceName+" · "+ep.Label, action, detail)
			s.notify(book, hook, title, message)
		}
		s.checkCertExpiry(book, hook, ep, cert)
	}
}

// certState buckets a certificate expiry date for transition detection.
// Ordered by severity: unknown(0) < ok(1) < expiring(2) < expired(3); alerts
// fire when the state worsens, never when it stays or improves.
type certState int

const (
	certUnknown certState = iota
	certOK
	certExpiring
	certExpired
)

// classifyCert maps an expiry date onto its state.
func classifyCert(notAfter time.Time, now time.Time) certState {
	switch {
	case notAfter.Before(now):
		return certExpired
	case notAfter.Before(now.Add(certWarnDays * 24 * time.Hour)):
		return certExpiring
	default:
		return certOK
	}
}

// checkCertExpiry announces a certificate whose state worsened since the last
// observation (unknown → expiring counts as worsening, so a cert that is
// already inside the warning window on first sight still alerts — once).
func (s *Scheduler) checkCertExpiry(ctx context.Context, hook string, ep store.HealthEndpoint, cert *store.CertObservation) {
	if cert == nil {
		return
	}
	now := time.Now()
	before := certUnknown
	if ep.CertNotAfter != nil {
		before = classifyCert(*ep.CertNotAfter, now)
	}
	after := classifyCert(cert.NotAfter, now)
	if after <= before || after < certExpiring {
		return
	}
	action, title := "cert_expiring", ep.ServiceName+" certificate expiring"
	days := int(cert.NotAfter.Sub(now).Hours() / 24)
	message := fmt.Sprintf("%s — certificate expires in %d days (%s).", ep.URL, days, cert.NotAfter.Format("2006-01-02"))
	if after == certExpired {
		action, title = "cert_expired", ep.ServiceName+" certificate expired"
		message = fmt.Sprintf("%s — certificate expired on %s.", ep.URL, cert.NotAfter.Format("2006-01-02"))
	}
	s.recordAudit(ctx, "service_endpoint", ep.ID, ep.ServiceName+" · "+ep.Label, action, map[string]any{
		"url": ep.URL, "subject": cert.Subject, "not_after": cert.NotAfter.Format(time.RFC3339),
	})
	s.notify(ctx, hook, title, message)
}

// pruneHistory bounds the rolling tables: endpoint check history older than
// seven days is removed hourly, audit events are removed daily when the
// operator set a retention window. Failures are logged, never fatal.
func (s *Scheduler) pruneHistory(ctx context.Context, retentionDays int) {
	book := context.WithoutCancel(ctx)
	now := time.Now()

	if s.lastChecksPrune.IsZero() || now.Sub(s.lastChecksPrune) >= checksPruneEvery {
		s.lastChecksPrune = now
		if n, err := s.services.PruneEndpointChecks(book, checksRetention); err != nil {
			s.logger.Error("scheduler: prune endpoint checks failed", "error", err)
		} else if n > 0 {
			s.logger.Debug("scheduler: pruned endpoint checks", "rows", n)
		}
	}

	if retentionDays > 0 && (s.lastAuditPrune.IsZero() || now.Sub(s.lastAuditPrune) >= auditPruneEvery) {
		s.lastAuditPrune = now
		n, err := s.audit.Prune(book, retentionDays)
		if err != nil {
			s.logger.Error("scheduler: prune audit events failed", "error", err)
			return
		}
		if n > 0 {
			s.logger.Info("scheduler: pruned audit events", "rows", n, "retention_days", retentionDays)
			s.recordAudit(book, "settings", "", "audit log", "retention_pruned", map[string]any{
				"deleted": n, "retention_days": retentionDays,
			})
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

// probeHTTP performs one GET and returns the observed status/latency/error,
// plus the TLS leaf certificate when the endpoint answered over TLS.
func probeHTTP(ctx context.Context, client *http.Client, url string) (statusCode, latencyMS int, errText string, cert *store.CertObservation) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err.Error(), nil
	}
	req.Header.Set("User-Agent", "lanvory-health-check")
	resp, err := client.Do(req)
	latencyMS = int(time.Since(start).Milliseconds())
	if err != nil {
		return 0, latencyMS, err.Error(), nil
	}
	defer resp.Body.Close()
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		leaf := resp.TLS.PeerCertificates[0]
		cert = &store.CertObservation{NotAfter: leaf.NotAfter, Subject: leaf.Subject.String()}
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, latencyMS, "", cert
}

// syncDueIntegrations runs every enabled integration whose auto-sync interval
// has elapsed. Runner.Run records the run history and last_sync_at itself.
func (s *Scheduler) syncDueIntegrations(ctx context.Context, hook string) {
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
			s.notify(book, hook, "Sync failed: "+integ.Name, syncErr.Error())
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
