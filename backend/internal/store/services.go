package store

import (
	"context"
	"errors"
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
		SELECT id, service_id, label, protocol, port, url, is_external
		FROM service_endpoints WHERE service_id=$1 ORDER BY is_external, protocol, port`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceEndpoint{}
	for rows.Next() {
		var e ServiceEndpoint
		if err := rows.Scan(&e.ID, &e.ServiceID, &e.Label, &e.Protocol, &e.Port, &e.URL, &e.IsExternal); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Services) CreateEndpoint(ctx context.Context, e *ServiceEndpoint) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO service_endpoints (service_id, label, protocol, port, url, is_external)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id`, e.ServiceID, e.Label, e.Protocol, e.Port, e.URL, e.IsExternal,
	).Scan(&e.ID)
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
