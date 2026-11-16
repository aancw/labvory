package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrHasChildren is returned when a referenced asset/location cannot be removed
// because dependent rows exist.
var ErrHasChildren = errors.New("has dependents")

// Tag is a user-defined label attachable to entities.
type Tag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Usage int    `json:"usage"`
}

// Asset is a physical or virtual infrastructure asset.
// Nullable numeric/date fields use pointers to distinguish "unset" from zero.
type Asset struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Hostname        string          `json:"hostname"`
	TypeID          string          `json:"type_id"`
	TypeLabel       string          `json:"type_label"`
	TypeSlug        string          `json:"type_slug"`
	Category        string          `json:"category"`
	Icon            string          `json:"icon"`
	Status          string          `json:"status"`
	Lifecycle       string          `json:"lifecycle"`
	Environment     string          `json:"environment"`
	LocationID      *string         `json:"location_id,omitempty"`
	LocationName    *string         `json:"location_name,omitempty"`
	ParentID        *string         `json:"parent_id,omitempty"`
	ParentName      *string         `json:"parent_name,omitempty"`
	Manufacturer    string          `json:"manufacturer"`
	Model           string          `json:"model"`
	SerialNumber    string          `json:"serial_number"`
	OSName          string          `json:"os_name"`
	OSVersion       string          `json:"os_version"`
	CPUModel        string          `json:"cpu_model"`
	CPUCores        *int            `json:"cpu_cores,omitempty"`
	RamGB           *float64        `json:"ram_gb,omitempty"`
	StorageGB       *float64        `json:"storage_gb,omitempty"`
	PurchaseDate    *string         `json:"purchase_date,omitempty"`    // YYYY-MM-DD
	PurchaseCost    *float64        `json:"purchase_cost,omitempty"`    // numeric
	WarrantyExpires *string         `json:"warranty_expires,omitempty"` // YYYY-MM-DD
	Notes           string          `json:"notes"`
	Source          string          `json:"source"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
	PrimaryIP       *string         `json:"primary_ip,omitempty"`
	ChildCount      int             `json:"child_count"`
	Tags            []Tag           `json:"tags"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// AssetFilter parametrizes the inventory query. All fields optional.
type AssetFilter struct {
	Q           string
	Category    string   // physical | virtual
	TypeSlugs   []string // e.g. vm, lxc
	TypeID      string
	Status      string
	Environment string
	LocationID  string
	ParentID    string
	Sort        string // updated_at | created_at | name | status
	Order       string // asc | desc
	Limit       int
	Offset      int
}

var assetSortColumns = map[string]string{
	"updated_at": "a.updated_at",
	"created_at": "a.created_at",
	"name":       "a.name",
	"status":     "a.status",
	"hostname":   "a.hostname",
}

const assetColumns = `
	a.id, a.name, a.hostname, a.type_id, t.label, t.slug, t.category, t.icon,
	a.status, a.lifecycle, a.environment,
	TO_JSONB(l.id) #>> '{}' AS location_id, l.name AS location_name,
	TO_JSONB(p.id) #>> '{}' AS parent_id, p.name AS parent_name,
	a.manufacturer, a.model, a.serial_number, a.os_name, a.os_version, a.cpu_model,
	a.cpu_cores, a.ram_gb, a.storage_gb,
	TO_CHAR(a.purchase_date, 'YYYY-MM-DD'), a.purchase_cost, TO_CHAR(a.warranty_expires, 'YYYY-MM-DD'),
	a.notes, a.source, a.metadata,
	a.created_at, a.updated_at`

const assetJoins = `
	FROM assets a
	JOIN asset_types t ON t.id = a.type_id
	LEFT JOIN locations l ON l.id = a.location_id
	LEFT JOIN assets p ON p.id = a.parent_id`

type Assets struct{ pool *pgxpool.Pool }

func NewAssets(pool *pgxpool.Pool) *Assets { return &Assets{pool: pool} }

// Create inserts an asset and links its tags in one transaction.
func (s *Assets) Create(ctx context.Context, a *Asset, tagIDs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO assets
			(name, hostname, type_id, status, lifecycle, environment, location_id, parent_id,
			 manufacturer, model, serial_number, os_name, os_version, cpu_model,
			 cpu_cores, ram_gb, storage_gb, purchase_date, purchase_cost, warranty_expires,
			 notes, source, metadata)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		RETURNING id, created_at, updated_at`,
		a.Name, a.Hostname, a.TypeID, a.Status, a.Lifecycle, a.Environment,
		nullableID(deref(a.LocationID)), nullableID(deref(a.ParentID)),
		a.Manufacturer, a.Model, a.SerialNumber, a.OSName, a.OSVersion, a.CPUModel,
		a.CPUCores, a.RamGB, a.StorageGB, nilPtr(a.PurchaseDate), a.PurchaseCost,
		nilPtr(a.WarrantyExpires), a.Notes, a.Source, jsonOrEmpty(a.Metadata),
	).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return mapConstraint(err)
	}
	if err := setEntityTagsTx(ctx, tx, "asset", a.ID, tagIDs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetByID loads one asset with its tags.
func (s *Assets) GetByID(ctx context.Context, id string) (Asset, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT `+assetColumns+`,
			(
				SELECT host(i.address)
				FROM ip_addresses i
				JOIN network_interfaces ni ON ni.id = i.interface_id
				WHERE ni.asset_id = a.id
				ORDER BY (i.status = 'assigned') DESC, i.address
				LIMIT 1
			) AS primary_ip,
			(
				SELECT count(*) FROM assets c WHERE c.parent_id = a.id
			) AS child_count
		`+assetJoins+` WHERE a.id = $1`, id)

	var a Asset
	err := row.Scan(append(assetScanTargets(&a), &a.PrimaryIP, &a.ChildCount)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrNotFound
	}
	if err != nil {
		return Asset{}, err
	}
	if a.Tags, err = s.entityTags(ctx, "asset", a.ID); err != nil {
		return Asset{}, err
	}
	return a, nil
}

// List returns a filtered, sorted page of assets plus the total matching count.
func (s *Assets) List(ctx context.Context, f AssetFilter) ([]Asset, int, error) {
	where, args := buildAssetWhere(f)

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM assets a JOIN asset_types t ON t.id = a.type_id`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	sortCol, ok := assetSortColumns[f.Sort]
	if !ok {
		sortCol = "a.updated_at"
	}
	order := "DESC"
	if strings.EqualFold(f.Order, "asc") {
		order = "ASC"
	}

	query := `
		SELECT ` + assetColumns + `,
			(
				SELECT host(i.address)
				FROM ip_addresses i
				JOIN network_interfaces ni ON ni.id = i.interface_id
				WHERE ni.asset_id = a.id
				ORDER BY (i.status = 'assigned') DESC, i.address
				LIMIT 1
			) AS primary_ip,
			(
				SELECT count(*) FROM assets c WHERE c.parent_id = a.id
			) AS child_count
		` + assetJoins + where + fmt.Sprintf(" ORDER BY %s %s, a.id LIMIT $%d OFFSET $%d",
		sortCol, order, len(args)+1, len(args)+2)
	args = append(args, clampLimit(f.Limit, 100), f.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	assets := []Asset{}
	for rows.Next() {
		var a Asset
		if err := rows.Scan(append(assetScanTargets(&a), &a.PrimaryIP, &a.ChildCount)...); err != nil {
			return nil, 0, err
		}
		assets = append(assets, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return assets, total, nil
}

func assetScanTargets(a *Asset) []any {
	return []any{
		&a.ID, &a.Name, &a.Hostname, &a.TypeID, &a.TypeLabel, &a.TypeSlug, &a.Category, &a.Icon,
		&a.Status, &a.Lifecycle, &a.Environment,
		&a.LocationID, &a.LocationName,
		&a.ParentID, &a.ParentName,
		&a.Manufacturer, &a.Model, &a.SerialNumber, &a.OSName, &a.OSVersion, &a.CPUModel,
		&a.CPUCores, &a.RamGB, &a.StorageGB,
		&a.PurchaseDate, &a.PurchaseCost, &a.WarrantyExpires,
		&a.Notes, &a.Source, &a.Metadata,
		&a.CreatedAt, &a.UpdatedAt,
	}
}

func buildAssetWhere(f AssetFilter) (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, values ...any) {
		for _, v := range values {
			args = append(args, v)
			cond = strings.Replace(cond, "?", fmt.Sprintf("$%d", len(args)), 1)
		}
		conds = append(conds, cond)
	}

	if f.Q != "" {
		p := "%" + f.Q + "%"
		add(`(a.name ILIKE ? OR a.hostname ILIKE ? OR a.serial_number ILIKE ? OR a.manufacturer ILIKE ? OR a.model ILIKE ?)`,
			p, p, p, p, p)
	}
	if f.Category != "" {
		add(`t.category = ?`, f.Category)
	}
	if len(f.TypeSlugs) > 0 {
		add(`t.slug = ANY(?)`, f.TypeSlugs)
	}
	if f.TypeID != "" {
		add(`a.type_id = ?`, f.TypeID)
	}
	if f.Status != "" {
		add(`a.status = ?`, f.Status)
	}
	if f.Environment != "" {
		add(`a.environment = ?`, f.Environment)
	}
	if f.LocationID != "" {
		add(`a.location_id = ?`, f.LocationID)
	}
	if f.ParentID != "" {
		add(`a.parent_id = ?`, f.ParentID)
	}

	if len(conds) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// Update replaces all editable fields and rewrites the tag set in one transaction.
func (s *Assets) Update(ctx context.Context, a *Asset, tagIDs []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		UPDATE assets SET
			name=$2, hostname=$3, type_id=$4, status=$5, lifecycle=$6, environment=$7,
			location_id=$8, parent_id=$9, manufacturer=$10, model=$11, serial_number=$12,
			os_name=$13, os_version=$14, cpu_model=$15, cpu_cores=$16, ram_gb=$17,
			storage_gb=$18, purchase_date=$19, purchase_cost=$20, warranty_expires=$21,
			notes=$22, metadata=$23, updated_at=now()
		WHERE id=$1
		RETURNING updated_at`,
		a.ID, a.Name, a.Hostname, a.TypeID, a.Status, a.Lifecycle, a.Environment,
		nullableID(deref(a.LocationID)), nullableID(deref(a.ParentID)),
		a.Manufacturer, a.Model, a.SerialNumber, a.OSName, a.OSVersion, a.CPUModel,
		a.CPUCores, a.RamGB, a.StorageGB, nilPtr(a.PurchaseDate), a.PurchaseCost,
		nilPtr(a.WarrantyExpires), a.Notes, jsonOrEmpty(a.Metadata),
	).Scan(&a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return mapConstraint(err)
	}
	if err := setEntityTagsTx(ctx, tx, "asset", a.ID, tagIDs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Delete removes an asset. Parent assets with guests are rejected (FK RESTRICT).
func (s *Assets) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM assets WHERE id = $1`, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrHasChildren
	}
	return err
}

// CountChildren returns how many assets reference the given parent.
func (s *Assets) CountChildren(ctx context.Context, ids []string) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT parent_id, count(*) FROM assets
		WHERE parent_id = ANY($1) GROUP BY parent_id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// DeleteMany removes several assets (children are rejected by FK as in Delete).
func (s *Assets) DeleteMany(ctx context.Context, ids []string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM assets WHERE id = ANY($1)`, ids)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrHasChildren
	}
	return err
}

// BulkUpdateStatus / BulkUpdateEnvironment perform set-based updates.
func (s *Assets) BulkUpdateStatus(ctx context.Context, status string, ids []string) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE assets SET status=$2, updated_at=now() WHERE id = ANY($1)`, ids, status)
	return int(tag.RowsAffected()), err
}

func (s *Assets) BulkUpdateEnvironment(ctx context.Context, environment string, ids []string) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE assets SET environment=$2, updated_at=now() WHERE id = ANY($1)`, ids, environment)
	return int(tag.RowsAffected()), err
}

func (s *Assets) entityTags(ctx context.Context, entityType, entityID string) ([]Tag, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.name, t.color
		FROM entity_tags e JOIN tags t ON t.id = e.tag_id
		WHERE e.entity_type = $1 AND e.entity_id = $2
		ORDER BY t.name`, entityType, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// deref returns the string value of a nullable pointer or "".
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func jsonOrEmpty(raw json.RawMessage) any {
	if len(raw) == 0 {
		return "{}"
	}
	return raw
}

// nilPtr maps a nil string pointer to SQL NULL, otherwise passes the value through.
func nilPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// clampLimit bounds a page size request to sane values.
func clampLimit(n, max int) int {
	if n <= 0 {
		return 50
	}
	if n > max {
		return max
	}
	return n
}
