package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NetworkInterface is a physical or logical interface of an asset.
type NetworkInterface struct {
	ID         string    `json:"id"`
	AssetID    string    `json:"asset_id"`
	AssetName  string    `json:"asset_name,omitempty"`
	Name       string    `json:"name"`
	Kind       string    `json:"kind"`
	MACAddress *string   `json:"mac_address,omitempty"`
	MTU        *int      `json:"mtu,omitempty"`
	SpeedMbps  *int      `json:"speed_mbps,omitempty"`
	Notes      string    `json:"notes"`
	IPCount    int       `json:"ip_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Interfaces struct{ pool *pgxpool.Pool }

func NewInterfaces(pool *pgxpool.Pool) *Interfaces { return &Interfaces{pool: pool} }

// MACByID returns the MAC address recorded on one interface ("" when the
// interface exists but has no MAC).
func (s *Interfaces) MACByID(ctx context.Context, id string) (string, error) {
	var mac *string
	err := s.pool.QueryRow(ctx, `SELECT mac_address FROM network_interfaces WHERE id = $1`, id).Scan(&mac)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil || mac == nil {
		return "", err
	}
	return *mac, nil
}

// Create adds an interface to an asset.
func (s *Interfaces) Create(ctx context.Context, ni *NetworkInterface) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO network_interfaces (asset_id, name, kind, mac_address, mtu, speed_mbps, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, created_at, updated_at`,
		ni.AssetID, ni.Name, ni.Kind, ni.MACAddress, ni.MTU, ni.SpeedMbps, ni.Notes,
	).Scan(&ni.ID, &ni.CreatedAt, &ni.UpdatedAt)
	if err != nil {
		return mapConstraint(err)
	}
	return nil
}

// Update replaces editable fields.
func (s *Interfaces) Update(ctx context.Context, ni *NetworkInterface) error {
	err := s.pool.QueryRow(ctx, `
		UPDATE network_interfaces SET name=$2, kind=$3, mac_address=$4, mtu=$5, speed_mbps=$6,
		       notes=$7, updated_at=now()
		WHERE id=$1
		RETURNING updated_at`,
		ni.ID, ni.Name, ni.Kind, ni.MACAddress, ni.MTU, ni.SpeedMbps, ni.Notes,
	).Scan(&ni.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return mapConstraint(err)
}

// Delete removes an interface; assigned IP rows keep existing (interface_id set NULL).
func (s *Interfaces) Delete(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM network_interfaces WHERE id = $1`, id)
	return err
}

// ListByAsset returns the interfaces of one asset.
func (s *Interfaces) ListByAsset(ctx context.Context, assetID string) ([]NetworkInterface, error) {
	return s.list(ctx, ` WHERE ni.asset_id = $1`, assetID)
}

// ListAll returns interfaces across the inventory (global table view).
func (s *Interfaces) ListAll(ctx context.Context, limit, offset int) ([]NetworkInterface, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM network_interfaces`).Scan(&total); err != nil {
		return nil, 0, err
	}
	items, err := s.list(ctx, ` ORDER BY ni.asset_id, ni.name LIMIT $1 OFFSET $2`, limit, offset)
	return items, total, err
}

// MACIndexEntry is one (asset, MAC) pair for L2 matching by integrations.
type MACIndexEntry struct {
	AssetID string
	MAC     string
}

// MACIndex returns every interface MAC with its owning asset. Homelab-scale:
// callers normalize and match in memory.
func (s *Interfaces) MACIndex(ctx context.Context) ([]MACIndexEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT asset_id, mac_address FROM network_interfaces
		WHERE mac_address IS NOT NULL AND mac_address <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MACIndexEntry{}
	for rows.Next() {
		var e MACIndexEntry
		if err := rows.Scan(&e.AssetID, &e.MAC); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Interfaces) list(ctx context.Context, suffix string, args ...any) ([]NetworkInterface, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ni.id, ni.asset_id, a.name, ni.name, ni.kind, ni.mac_address, ni.mtu, ni.speed_mbps,
		       ni.notes,
		       (SELECT count(*) FROM ip_addresses i WHERE i.interface_id = ni.id) AS ip_count,
		       ni.created_at, ni.updated_at
		FROM network_interfaces ni
		JOIN assets a ON a.id = ni.asset_id`+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []NetworkInterface{}
	for rows.Next() {
		var ni NetworkInterface
		if err := rows.Scan(&ni.ID, &ni.AssetID, &ni.AssetName, &ni.Name, &ni.Kind, &ni.MACAddress,
			&ni.MTU, &ni.SpeedMbps, &ni.Notes, &ni.IPCount, &ni.CreatedAt, &ni.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, ni)
	}
	return items, rows.Err()
}
