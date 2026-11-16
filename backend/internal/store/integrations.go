package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration is a configured data source (Proxmox, Docker, ICMP…).
type Integration struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Enabled     bool            `json:"enabled"`
	Config      json.RawMessage `json:"config"` // secrets inside are redacted by the API layer
	LastSyncAt  *time.Time      `json:"last_sync_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type SyncRun struct {
	ID            string          `json:"id"`
	IntegrationID string          `json:"integration_id"`
	StartedAt     time.Time       `json:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at,omitempty"`
	Status        string          `json:"status"` // running | success | failed | partial
	Summary       json.RawMessage `json:"summary"`
	Message       string          `json:"message"`
}

type Integrations struct{ pool *pgxpool.Pool }

func NewIntegrations(pool *pgxpool.Pool) *Integrations { return &Integrations{pool: pool} }

func (s *Integrations) List(ctx context.Context) ([]Integration, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, name, description, enabled, config, last_sync_at, created_at, updated_at
		FROM integrations ORDER BY kind, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Integration{}
	for rows.Next() {
		var i Integration
		if err := rows.Scan(&i.ID, &i.Kind, &i.Name, &i.Description, &i.Enabled,
			&i.Config, &i.LastSyncAt, &i.CreatedAt, &i.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Integrations) Get(ctx context.Context, id string) (Integration, error) {
	var i Integration
	err := s.pool.QueryRow(ctx, `
		SELECT id, kind, name, description, enabled, config, last_sync_at, created_at, updated_at
		FROM integrations WHERE id=$1`, id,
	).Scan(&i.ID, &i.Kind, &i.Name, &i.Description, &i.Enabled, &i.Config, &i.LastSyncAt, &i.CreatedAt, &i.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Integration{}, ErrNotFound
	}
	return i, err
}

func (s *Integrations) Create(ctx context.Context, i *Integration) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO integrations (kind, name, description, enabled, config)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, created_at, updated_at`,
		i.Kind, i.Name, i.Description, i.Enabled, i.Config,
	).Scan(&i.ID, &i.CreatedAt, &i.UpdatedAt)
	return mapConstraint(err)
}

func (s *Integrations) Update(ctx context.Context, i *Integration) error {
	err := s.pool.QueryRow(ctx, `
		UPDATE integrations SET name=$2, description=$3, enabled=$4, config=$5, updated_at=now()
		WHERE id=$1
		RETURNING updated_at`,
		i.ID, i.Name, i.Description, i.Enabled, i.Config,
	).Scan(&i.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return mapConstraint(err)
}

func (s *Integrations) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM integrations WHERE id=$1`, id)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// TouchSync updates last_sync_at after a run.
func (s *Integrations) TouchSync(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE integrations SET last_sync_at=now() WHERE id=$1`, id)
	return err
}

// ---------- sync runs ----------

func (s *Integrations) StartRun(ctx context.Context, integrationID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO integration_sync_runs (integration_id, status) VALUES ($1, 'running')
		RETURNING id`, integrationID).Scan(&id)
	return id, err
}

func (s *Integrations) FinishRun(ctx context.Context, runID, status, message string, summary json.RawMessage) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE integration_sync_runs SET finished_at=now(), status=$2, summary=$3, message=$4
		WHERE id=$1`, runID, status, summary, message)
	return err
}

func (s *Integrations) ListRuns(ctx context.Context, integrationID string, limit int) ([]SyncRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, integration_id, started_at, finished_at, status, summary, message
		FROM integration_sync_runs
		WHERE integration_id=$1
		ORDER BY started_at DESC LIMIT $2`, integrationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SyncRun{}
	for rows.Next() {
		var r SyncRun
		if err := rows.Scan(&r.ID, &r.IntegrationID, &r.StartedAt, &r.FinishedAt, &r.Status, &r.Summary, &r.Message); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------- sync helpers: asset upsert for discovered entities ----------

// FindAssetByExternalRef locates an asset previously created by the same source
// using its metadata external reference, falling back to source+type+name
// matching for rows whose metadata was created before external refs existed.
func (s *Integrations) FindAssetByExternalRef(ctx context.Context, source, externalKey, externalValue, typeSlug, fallbackName string) (string, bool, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT a.id FROM assets a
		JOIN asset_types t ON t.id = a.type_id
		WHERE a.source = $1
		  AND a.metadata -> $2 ->> 'value' = $3
		  AND t.slug = $4
		LIMIT 1`, source, externalKey, externalValue, typeSlug).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	if fallbackName == "" {
		return "", false, nil
	}
	err = s.pool.QueryRow(ctx, `
		SELECT a.id FROM assets a
		JOIN asset_types t ON t.id = a.type_id
		WHERE a.source = $1 AND t.slug = $2 AND lower(a.name) = lower($3)
		LIMIT 1`, source, typeSlug, fallbackName).Scan(&id)
	if err == nil {
		return id, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	return "", false, nil
}

// UpsertDiscoveredAsset creates or refreshes a source-managed asset. Only the
// fields listed below are written on update — manual metadata (notes, tags,
// serials, locations) is preserved (spec §4: no silent overwrite).
func (s *Integrations) UpsertDiscoveredAsset(ctx context.Context, in DiscoveredAsset) (id string, created bool, err error) {
	if in.Status == "" {
		in.Status = "unknown" // discovered assets never assume operator-declared states
	}
	if in.Environment == "" {
		in.Environment = "production"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)

	existingID, found, err := s.FindAssetByExternalRef(ctx, in.Source, in.ExternalKey, in.ExternalValue, in.TypeSlug, in.Name)
	if err != nil {
		return "", false, err
	}

	mergedMeta, err := json.Marshal(in.Metadata)
	if err != nil {
		return "", false, err
	}

	if found {
		// The name follows the best-known identity only while it still holds
		// the default (external value): operator renames always survive, while
		// default-named assets pick up richer names (e.g. reverse DNS).
		_, err = tx.Exec(ctx, `
			UPDATE assets SET
				name = (CASE WHEN name = $8 THEN $9 ELSE name END),
				hostname = $2, os_name = $3, os_version = $4,
				cpu_cores = $5, ram_gb = $6,
				metadata = $7,
				updated_at = now()
			WHERE id = $1`,
			existingID, in.Hostname, in.OSName, in.OSVersion, in.CPUCores, in.RamGB, mergedMeta,
			in.ExternalValue, in.Name)
		if err != nil {
			return "", false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", false, err
		}
		return existingID, false, nil
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO assets (name, hostname, type_id, status, environment, os_name, os_version,
		                    cpu_cores, ram_gb, source, metadata)
		SELECT $1, $2, t.id, $3, $4, $5, $6, $7, $8, $9, $10
		FROM asset_types t WHERE t.slug = $11
		RETURNING id`,
		in.Name, in.Hostname, in.Status, in.Environment, in.OSName, in.OSVersion,
		in.CPUCores, in.RamGB, in.Source, mergedMeta, in.TypeSlug,
	).Scan(&id)
	if err != nil {
		return "", false, mapConstraint(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return id, true, nil
}

// DiscoveredAsset is a normalized guest/container/device from any integration.
type DiscoveredAsset struct {
	Name          string
	Hostname      string
	TypeSlug      string
	Status        string // declared inventory status (kept coarse: online/unknown)
	Environment   string
	OSName        string
	OSVersion     string
	CPUCores      *int
	RamGB         *float64
	Source        string // proxmox | docker | ...
	ExternalKey   string // e.g. "vmid" / "container_id"
	ExternalValue string
	Metadata      map[string]any
}

// EnsureParent sets parent_id only when the guest has no parent yet — a manually
// placed guest is never re-parented by a sync.
func (s *Integrations) EnsureParent(ctx context.Context, assetID, parentID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE assets SET parent_id = $2 WHERE id = $1 AND parent_id IS NULL`, assetID, parentID)
	_ = tag
	return err
}

// SetObserved records a reachability observation without touching declared state.
func (s *Integrations) SetObserved(ctx context.Context, assetID string, observed map[string]any) error {
	var meta json.RawMessage
	if err := s.pool.QueryRow(ctx, `SELECT metadata FROM assets WHERE id=$1`, assetID).Scan(&meta); err != nil {
		return err
	}
	base := map[string]any{}
	_ = json.Unmarshal(meta, &base)
	base["observed"] = observed
	merged, err := json.Marshal(base)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE assets SET metadata=$2 WHERE id=$1`, assetID, merged)
	return err
}

// SetObservedSub merges one named observation under metadata.observed.<key>,
// leaving other observation keys (e.g. icmp reachability) untouched.
func (s *Integrations) SetObservedSub(ctx context.Context, assetID, key string, observed map[string]any) error {
	var meta json.RawMessage
	if err := s.pool.QueryRow(ctx, `SELECT metadata FROM assets WHERE id=$1`, assetID).Scan(&meta); err != nil {
		return err
	}
	base := map[string]any{}
	_ = json.Unmarshal(meta, &base)
	obs, _ := base["observed"].(map[string]any)
	if obs == nil {
		obs = map[string]any{}
	}
	obs[key] = observed
	base["observed"] = obs
	merged, err := json.Marshal(base)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE assets SET metadata=$2 WHERE id=$1`, assetID, merged)
	return err
}
