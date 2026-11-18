package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service is a tracked application running on the infrastructure.
type Service struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	Description    string    `json:"description"`
	Category       string    `json:"category"`
	DeploymentType string    `json:"deployment_type"`
	Environment    string    `json:"environment"`
	Status         string    `json:"status"`
	RepoURL        string    `json:"repo_url"`
	DocsURL        string    `json:"docs_url"`
	Notes          string    `json:"notes"`
	HostCount      int       `json:"host_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ServiceEndpoint struct {
	ID         string `json:"id"`
	ServiceID  string `json:"service_id"`
	Label      string `json:"label"`
	Protocol   string `json:"protocol"`
	Port       *int   `json:"port,omitempty"`
	URL        string `json:"url"`
	IsExternal bool   `json:"is_external"`

	// Observed health-check state (scheduler-written; declared data above).
	HealthEnabled bool       `json:"health_enabled"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	LastStatus    *int       `json:"last_status,omitempty"`
	LastLatencyMS *int       `json:"last_latency_ms,omitempty"`
	LastError     string     `json:"last_error"`

	// Observed TLS leaf certificate from the last HTTPS probe. Empty until a
	// TLS-protected probe succeeded.
	CertNotAfter *time.Time `json:"cert_not_after,omitempty"`
	CertSubject  string     `json:"cert_subject"`

	// Operator-declared planned maintenance: while in the future, down/
	// recovered notifications are suppressed and the endpoint is excluded
	// from the dashboard "services down" alert.
	MaintenanceUntil *time.Time `json:"maintenance_until,omitempty"`
}

// InMaintenance reports whether the endpoint's declared maintenance window
// is currently active.
func (e ServiceEndpoint) InMaintenance(now time.Time) bool {
	return e.MaintenanceUntil != nil && e.MaintenanceUntil.After(now)
}

// CertObservation is the leaf certificate observed on one probe.
type CertObservation struct {
	NotAfter time.Time
	Subject  string
}

// EndpointCheckSample is one recorded probe outcome, oldest first.
type EndpointCheckSample struct {
	Up        bool      `json:"up"`
	CheckedAt time.Time `json:"checked_at"`
}

// EndpointUptime aggregates observed probe outcomes for one endpoint.
type EndpointUptime struct {
	Up24h  *float64              `json:"up_24h"`
	Up7d   *float64              `json:"up_7d"`
	Checks []EndpointCheckSample `json:"checks"`
}

type ServiceDeployment struct {
	ID             string    `json:"id"`
	ServiceID      string    `json:"service_id"`
	AssetID        string    `json:"asset_id"`
	AssetName      string    `json:"asset_name"`
	AssetHostname  string    `json:"asset_hostname"`
	AssetCategory  string    `json:"asset_category"`
	DeploymentType string    `json:"deployment_type"`
	Notes          string    `json:"notes"`
	CreatedAt      time.Time `json:"created_at"`
}

type Services struct{ pool *pgxpool.Pool }

func NewServices(pool *pgxpool.Pool) *Services { return &Services{pool: pool} }

const serviceColumns = `
	SELECT s.id, s.name, s.slug, s.description, s.category, s.deployment_type, s.environment,
	       s.status, s.repo_url, s.docs_url, s.notes, s.created_at, s.updated_at,
	       (SELECT count(*) FROM service_deployments d WHERE d.service_id = s.id)
	FROM services s`

func scanService(row pgx.Row) (Service, error) {
	var v Service
	err := row.Scan(&v.ID, &v.Name, &v.Slug, &v.Description, &v.Category, &v.DeploymentType,
		&v.Environment, &v.Status, &v.RepoURL, &v.DocsURL, &v.Notes, &v.CreatedAt, &v.UpdatedAt,
		&v.HostCount)
	return v, err
}

func (s *Services) List(ctx context.Context, q, category, environment, deploymentType string) ([]Service, error) {
	where := ""
	args := []any{}
	if q != "" {
		args = append(args, "%"+q+"%", "%"+q+"%")
		where = whereStart(where) + " (s.name ILIKE $1 OR s.description ILIKE $2)"
	}
	if category != "" {
		args = append(args, category)
		where += whereStart(where) + " s.category = $" + itoa(len(args))
	}
	if environment != "" {
		args = append(args, environment)
		where += whereStart(where) + " s.environment = $" + itoa(len(args))
	}
	if deploymentType != "" {
		args = append(args, deploymentType)
		where += whereStart(where) + " s.deployment_type = $" + itoa(len(args))
	}
	rows, err := s.pool.Query(ctx, serviceColumns+where+" ORDER BY s.name", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Service{}
	for rows.Next() {
		v, err := scanService(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Services) Get(ctx context.Context, id string) (Service, error) {
	v, err := scanService(s.pool.QueryRow(ctx, serviceColumns+" WHERE s.id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Service{}, ErrNotFound
	}
	return v, err
}

func (s *Services) Create(ctx context.Context, v *Service) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO services (name, slug, description, category, deployment_type, environment,
		                     status, repo_url, docs_url, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, created_at, updated_at`,
		v.Name, v.Slug, v.Description, v.Category, v.DeploymentType, v.Environment,
		v.Status, v.RepoURL, v.DocsURL, v.Notes,
	).Scan(&v.ID, &v.CreatedAt, &v.UpdatedAt)
	return mapConstraint(err)
}

func (s *Services) Update(ctx context.Context, v *Service) error {
	err := s.pool.QueryRow(ctx, `
		UPDATE services SET name=$2, description=$3, category=$4, deployment_type=$5,
		       environment=$6, status=$7, repo_url=$8, docs_url=$9, notes=$10, updated_at=now()
		WHERE id=$1
		RETURNING updated_at`,
		v.ID, v.Name, v.Description, v.Category, v.DeploymentType, v.Environment,
		v.Status, v.RepoURL, v.DocsURL, v.Notes,
	).Scan(&v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return mapConstraint(err)
}

func (s *Services) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM services WHERE id=$1`, id)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ---------- endpoints ----------

func (s *Services) ListEndpoints(ctx context.Context, serviceID string) ([]ServiceEndpoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, service_id, label, protocol, port, url, is_external,
		       health_enabled, last_checked_at, last_status, last_latency_ms, COALESCE(last_error, ''),
		       cert_not_after, cert_subject, maintenance_until
		FROM service_endpoints WHERE service_id=$1 ORDER BY is_external, protocol, port`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceEndpoint{}
	for rows.Next() {
		var e ServiceEndpoint
		if err := scanEndpoint(rows, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// rowScanner matches both pgx.Row and pgx.Rows so endpoint scanning can be
// shared between single-row and multi-row queries.
type rowScanner interface{ Scan(dest ...any) error }

// scanEndpoint scans the full observed endpoint column set (declared fields,
// health state, TLS certificate observation, maintenance window).
func scanEndpoint(r rowScanner, e *ServiceEndpoint) error {
	return r.Scan(&e.ID, &e.ServiceID, &e.Label, &e.Protocol, &e.Port, &e.URL, &e.IsExternal,
		&e.HealthEnabled, &e.LastCheckedAt, &e.LastStatus, &e.LastLatencyMS, &e.LastError,
		&e.CertNotAfter, &e.CertSubject, &e.MaintenanceUntil)
}

// Endpoint returns one endpoint by id.
func (s *Services) Endpoint(ctx context.Context, id string) (ServiceEndpoint, error) {
	var e ServiceEndpoint
	err := scanEndpoint(s.pool.QueryRow(ctx, `
		SELECT id, service_id, label, protocol, port, url, is_external,
		       health_enabled, last_checked_at, last_status, last_latency_ms, COALESCE(last_error, ''),
		       cert_not_after, cert_subject, maintenance_until
		FROM service_endpoints WHERE id=$1`, id), &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServiceEndpoint{}, ErrNotFound
	}
	return e, err
}

// HealthEndpoint is a service endpoint joined with its service name, used by
// the scheduler for health checks and notifications.
type HealthEndpoint struct {
	ServiceEndpoint
	ServiceName string `json:"service_name"`
}

// DueHealthEndpoints returns enabled endpoints (HTTP/HTTPS only) not checked
// within olderThan seconds.
func (s *Services) DueHealthEndpoints(ctx context.Context, olderThan time.Duration) ([]HealthEndpoint, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.service_id, e.label, e.protocol, e.port, e.url, e.is_external,
		       e.health_enabled, e.last_checked_at, e.last_status, e.last_latency_ms, COALESCE(e.last_error, ''),
		       e.cert_not_after, e.cert_subject, e.maintenance_until,
		       sv.name
		FROM service_endpoints e
		JOIN services sv ON sv.id = e.service_id
		WHERE e.health_enabled AND e.protocol IN ('http', 'https') AND e.url <> ''
		  AND (e.last_checked_at IS NULL OR e.last_checked_at < now() - make_interval(secs => $1))
		ORDER BY e.id`, int(olderThan.Seconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HealthEndpoint{}
	for rows.Next() {
		var e HealthEndpoint
		if err := rows.Scan(&e.ID, &e.ServiceID, &e.Label, &e.Protocol, &e.Port, &e.URL, &e.IsExternal,
			&e.HealthEnabled, &e.LastCheckedAt, &e.LastStatus, &e.LastLatencyMS, &e.LastError,
			&e.CertNotAfter, &e.CertSubject, &e.MaintenanceUntil,
			&e.ServiceName); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RecordHealthCheck stores the observed probe result for an endpoint. A
// non-nil cert observation replaces the stored TLS certificate; a nil one
// (probe failed before TLS completed, or plain HTTP) leaves the previous
// observation untouched — an expired cert is still worth reporting while the
// endpoint is down.
func (s *Services) RecordHealthCheck(ctx context.Context, endpointID string, statusCode, latencyMS int, errText string, cert *CertObservation) error {
	var notAfter any
	var subject any
	if cert != nil {
		notAfter, subject = cert.NotAfter, cert.Subject
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE service_endpoints
		SET last_checked_at=now(), last_status=$2, last_latency_ms=$3, last_error=$4,
		    cert_not_after=COALESCE($5, cert_not_after), cert_subject=COALESCE($6, cert_subject)
		WHERE id=$1`, endpointID, statusCode, latencyMS, errText, notAfter, subject)
	return err
}

// RecordEndpointCheck appends one probe outcome to the rolling history used
// for uptime reporting. A transport failure is recorded with status 0.
func (s *Services) RecordEndpointCheck(ctx context.Context, endpointID string, up bool, statusCode, latencyMS int, errText string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO service_endpoint_checks (endpoint_id, up, status, latency_ms, error)
		VALUES ($1, $2, $3, $4, $5)`,
		endpointID, up, statusCode, latencyMS, errText)
	return err
}

// ServiceUptime aggregates probe history for every endpoint of a service:
// uptime percentages over 24h and 7d (nil when nothing was probed yet) plus
// the most recent 32 samples per endpoint, oldest first, for sparklines.
func (s *Services) ServiceUptime(ctx context.Context, serviceID string) (map[string]EndpointUptime, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id,
		       count(c.id) FILTER (WHERE c.checked_at >= now() - interval '24 hours'),
		       count(c.id) FILTER (WHERE c.checked_at >= now() - interval '24 hours' AND c.up),
		       count(c.id) FILTER (WHERE c.checked_at >= now() - interval '7 days'),
		       count(c.id) FILTER (WHERE c.checked_at >= now() - interval '7 days' AND c.up)
		FROM service_endpoints e
		LEFT JOIN service_endpoint_checks c ON c.endpoint_id = e.id
		WHERE e.service_id = $1
		GROUP BY e.id`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]EndpointUptime{}
	for rows.Next() {
		var id string
		var total24, up24, total7, up7 int
		if err := rows.Scan(&id, &total24, &up24, &total7, &up7); err != nil {
			return nil, err
		}
		out[id] = EndpointUptime{
			Up24h:  uptimePct(up24, total24),
			Up7d:   uptimePct(up7, total7),
			Checks: []EndpointCheckSample{},
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	samples, err := s.recentEndpointChecks(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	for id, list := range samples {
		if u, ok := out[id]; ok {
			u.Checks = list
			out[id] = u
		}
	}
	return out, nil
}

// uptimePct returns the share of successful probes as 0-100, or nil when
// nothing was probed in the window.
func uptimePct(up, total int) *float64 {
	if total == 0 {
		return nil
	}
	pct := math.Round(float64(up)/float64(total)*1000) / 10
	return &pct
}

// recentEndpointChecks fetches the latest 32 samples per endpoint of one
// service, returned oldest-first for left-to-right sparklines.
func (s *Services) recentEndpointChecks(ctx context.Context, serviceID string) (map[string][]EndpointCheckSample, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT endpoint_id, up, checked_at FROM (
			SELECT c.endpoint_id, c.up, c.checked_at,
			       row_number() OVER (PARTITION BY c.endpoint_id ORDER BY c.checked_at DESC) AS rn
			FROM service_endpoint_checks c
			JOIN service_endpoints e ON e.id = c.endpoint_id
			WHERE e.service_id = $1
		) ranked
		WHERE rn <= 32
		ORDER BY endpoint_id, checked_at`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string][]EndpointCheckSample{}
	for rows.Next() {
		var id string
		var sample EndpointCheckSample
		if err := rows.Scan(&id, &sample.Up, &sample.CheckedAt); err != nil {
			return nil, err
		}
		out[id] = append(out[id], sample)
	}
	return out, rows.Err()
}

// PruneEndpointChecks deletes probe history older than the retention window.
func (s *Services) PruneEndpointChecks(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM service_endpoint_checks WHERE checked_at < now() - make_interval(secs => $1)`,
		int(olderThan.Seconds()))
	return tag.RowsAffected(), err
}

// CountCertsExpiring returns endpoints whose last observed TLS certificate
// expires within the given number of days (or already has).
func (s *Services) CountCertsExpiring(ctx context.Context, withinDays int) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM service_endpoints
		WHERE cert_not_after IS NOT NULL AND cert_not_after <= now() + make_interval(days => $1)`,
		withinDays).Scan(&n)
	return n, err
}

// CountEndpointsDown returns enabled endpoints whose last check failed
// (transport error or HTTP >= 400). Endpoints never checked count as unknown,
// not down; endpoints inside a declared maintenance window are expected to
// be down, so they are excluded.
func (s *Services) CountEndpointsDown(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM service_endpoints
		WHERE health_enabled AND last_checked_at IS NOT NULL
		  AND (last_error <> '' OR last_status IS NULL OR last_status >= 400)
		  AND (maintenance_until IS NULL OR maintenance_until <= now())`).Scan(&n)
	return n, err
}

// SetEndpointMaintenance sets or clears the planned maintenance window of one
// endpoint. A nil until clears the window.
func (s *Services) SetEndpointMaintenance(ctx context.Context, endpointID string, until *time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE service_endpoints SET maintenance_until=$2 WHERE id=$1`, endpointID, until)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Services) CreateEndpoint(ctx context.Context, e *ServiceEndpoint) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO service_endpoints (service_id, label, protocol, port, url, is_external, health_enabled)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id`, e.ServiceID, e.Label, e.Protocol, e.Port, e.URL, e.IsExternal, e.HealthEnabled,
	).Scan(&e.ID)
}

// SetEndpointHealth toggles the scheduler probe for one endpoint (declared
// endpoint fields are left untouched).
func (s *Services) SetEndpointHealth(ctx context.Context, endpointID string, enabled bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE service_endpoints SET health_enabled=$2 WHERE id=$1`, endpointID, enabled)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Services) DeleteEndpoint(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM service_endpoints WHERE id=$1`, id)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ---------- deployments ----------

func (s *Services) ListDeployments(ctx context.Context, serviceID string) ([]ServiceDeployment, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id, d.service_id, d.asset_id, a.name, a.hostname, t.category,
		       d.deployment_type, d.notes, d.created_at
		FROM service_deployments d
		JOIN assets a ON a.id = d.asset_id
		JOIN asset_types t ON t.id = a.type_id
		WHERE d.service_id=$1
		ORDER BY a.name`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceDeployment{}
	for rows.Next() {
		var d ServiceDeployment
		if err := rows.Scan(&d.ID, &d.ServiceID, &d.AssetID, &d.AssetName, &d.AssetHostname,
			&d.AssetCategory, &d.DeploymentType, &d.Notes, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Services) CreateDeployment(ctx context.Context, d *ServiceDeployment) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO service_deployments (service_id, asset_id, deployment_type, notes)
		VALUES ($1,$2,$3,$4)
		RETURNING id, created_at`,
		d.ServiceID, d.AssetID, d.DeploymentType, d.Notes,
	).Scan(&d.ID, &d.CreatedAt)
	return mapConstraint(err)
}

func (s *Services) UpdateDeployment(ctx context.Context, d *ServiceDeployment) error {
	err := s.pool.QueryRow(ctx, `
		UPDATE service_deployments SET deployment_type=$2, notes=$3 WHERE id=$1
		RETURNING created_at`, d.ID, d.DeploymentType, d.Notes,
	).Scan(&d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return mapConstraint(err)
}

func (s *Services) DeleteDeployment(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM service_deployments WHERE id=$1`, id)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ServicesWithoutDeployment counts services not mapped to any host (dashboard alert).
func (s *Services) CountWithoutDeployment(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM services s
		WHERE NOT EXISTS (SELECT 1 FROM service_deployments d WHERE d.service_id = s.id)`).Scan(&n)
	return n, err
}

// ByHost aggregates service counts per host asset (dashboard).
func (s *Services) ByHost(ctx context.Context, limit int) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.name, count(*) AS services
		FROM service_deployments d
		JOIN assets a ON a.id = d.asset_id
		GROUP BY a.id, a.name
		ORDER BY services DESC, a.name
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, name string
		var n int
		if err := rows.Scan(&id, &name, &n); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"asset_id": id, "asset_name": name, "services": n})
	}
	return out, rows.Err()
}

// ByOS aggregates distinct OS names over assets (dashboard).
func (s *Services) ByOS(ctx context.Context, limit int) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT os_name, count(*) FROM assets
		WHERE os_name <> ''
		GROUP BY os_name ORDER BY count(*) DESC, os_name
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"os": name, "count": n})
	}
	return out, rows.Err()
}

// FindBySlug returns a service by its unique slug.
func (s *Services) FindBySlug(ctx context.Context, slug string) (Service, error) {
	v, err := scanService(s.pool.QueryRow(ctx, serviceColumns+" WHERE s.slug=$1", slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return Service{}, ErrNotFound
	}
	return v, err
}
