// Package scheduler runs the periodic background work LabVory performs on
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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/labvory/internal/integrations"
	"github.com/aancw/labvory/internal/notify"
	"github.com/aancw/labvory/internal/store"
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

// backupSettings mirrors the backup.* app settings, with defaults applied.
type backupSettings struct {
	enabled       bool
	dir           string
	intervalHours int
	retention     int
}

// schedSettings is the per-tick snapshot of the instance settings the
// scheduler acts on.
type schedSettings struct {
	webhookURL    string
	retentionDays int
	backup        backupSettings
}

// Scheduler is the single background worker.
type Scheduler struct {
	pool         *pgxpool.Pool
	integrations *store.Integrations
	services     *store.Services
	settings     *store.Settings
	audit        *store.AuditEvents
	importer     *store.Importer
	runner       *integrations.Runner
	logger       *slog.Logger

	lastChecksPrune time.Time
	lastAuditPrune  time.Time
	lastBackupAt    time.Time
}

func New(pool *pgxpool.Pool, runner *integrations.Runner, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		pool:         pool,
		integrations: store.NewIntegrations(pool),
		services:     store.NewServices(pool),
		settings:     store.NewSettings(pool),
		audit:        store.NewAuditEvents(pool),
		importer:     store.NewImporter(pool),
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
	cfg := s.loadSettings(ctx)
	s.syncDueIntegrations(ctx, cfg.webhookURL)
	s.checkDueEndpoints(ctx, cfg.webhookURL)
	s.pruneHistory(ctx, cfg.retentionDays)
	s.runDueBackups(ctx, cfg)
}

// loadSettings reads the settings the scheduler acts on once per tick.
// Empty webhook = notifications disabled; retention 0 = keep forever.
func (s *Scheduler) loadSettings(ctx context.Context) schedSettings {
	var cfg schedSettings
	all, err := s.settings.All(ctx)
	if err != nil {
		s.logger.Error("scheduler: load settings failed", "error", err)
		return cfg
	}
	if raw := all["notifications.webhook_url"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg.webhookURL)
	}
	if raw := all["audit.retention_days"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg.retentionDays)
	}
	cfg.backup = backupSettings{
		enabled:       false,
		dir:           "./backups",
		intervalHours: 24,
		retention:     14,
	}
	if raw := all["backup.enabled"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg.backup.enabled)
	}
	if raw := all["backup.dir"]; len(raw) > 0 {
		var dir string
		if json.Unmarshal(raw, &dir) == nil && strings.TrimSpace(dir) != "" {
			cfg.backup.dir = dir
		}
	}
	if raw := all["backup.interval_hours"]; len(raw) > 0 {
		var n int
		if json.Unmarshal(raw, &n) == nil && n >= 1 {
			cfg.backup.intervalHours = n
		}
	}
	if raw := all["backup.retention"]; len(raw) > 0 {
		var n int
		if json.Unmarshal(raw, &n) == nil && n >= 1 {
			cfg.backup.retention = n
		}
	}
	return cfg
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
			// Inside a declared maintenance window the flapping is expected:
			// the transition stays in the Activity log but no push is sent.
			if !ep.InMaintenance(time.Now()) {
				s.notify(book, hook, title, message)
			}
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
	req.Header.Set("User-Agent", "labvory-health-check")
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

// ---------- scheduled inventory backups ----------

// backupFilePrefix is the filename pattern of the JSON snapshots the backup
// job writes; pruning sorts by name, so the embedded timestamp is the order.
const backupFilePrefix = "labvory-inventory-"

// runDueBackups writes a full inventory JSON snapshot when the configured
// interval has elapsed. Each run — success or failure — is audited, and a
// failed run notifies the webhook because silent backup rot is worse than a
// noisy alert. The interval is tracked in memory: a restart simply takes one
// extra snapshot, which retention prunes.
func (s *Scheduler) runDueBackups(ctx context.Context, cfg schedSettings) {
	if !cfg.backup.enabled {
		return
	}
	interval := time.Duration(cfg.backup.intervalHours) * time.Hour
	now := time.Now()
	if !s.lastBackupAt.IsZero() && now.Sub(s.lastBackupAt) < interval {
		return
	}
	s.lastBackupAt = now

	// The snapshot must survive a shutdown mid-write, hence temp-file rename.
	book := context.WithoutCancel(ctx)
	name, err := s.writeBackupSnapshot(book, now, cfg.backup)
	if err != nil {
		s.logger.Error("scheduler: inventory backup failed", "error", err)
		s.recordAudit(book, "settings", "", "inventory backup", "backup_failed",
			map[string]any{"error": err.Error(), "dir": cfg.backup.dir})
		s.notify(book, cfg.webhookURL, "LabVory inventory backup failed", err.Error())
		return
	}
	s.recordAudit(book, "settings", "", "inventory backup", "backup_completed",
		map[string]any{"file": name, "dir": cfg.backup.dir, "retention": cfg.backup.retention})
}

// writeBackupSnapshot exports the inventory (same document the UI download
// produces, so files restore losslessly), writes it atomically into dir, and
// prunes the oldest snapshots beyond the retention count.
func (s *Scheduler) writeBackupSnapshot(ctx context.Context, now time.Time, b backupSettings) (string, error) {
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	doc, err := s.importer.ExportInventory(ctx)
	if err != nil {
		return "", fmt.Errorf("export inventory: %w", err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode inventory: %w", err)
	}

	name := backupFilePrefix + now.Format("20060102-150405") + ".json"
	tmp := filepath.Join(b.dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", fmt.Errorf("write snapshot: %w", err)
	}
	final := filepath.Join(b.dir, name)
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("move snapshot into place: %w", err)
	}

	if err := pruneBackupSnapshots(b.dir, b.retention); err != nil {
		// A failed prune must not fail the backup that just succeeded.
		s.logger.Warn("scheduler: backup prune failed", "error", err)
	}
	return name, nil
}

// pruneBackupSnapshots deletes the oldest snapshot files so at most retention
// remain. Names sort chronologically because of the embedded timestamp.
func pruneBackupSnapshots(dir string, retention int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var snapshots []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if name := e.Name(); strings.HasPrefix(name, backupFilePrefix) && strings.HasSuffix(name, ".json") {
			snapshots = append(snapshots, name)
		}
	}
	if len(snapshots) <= retention {
		return nil
	}
	sort.Strings(snapshots)
	for _, name := range snapshots[:len(snapshots)-retention] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}
