package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/lanvory/internal/ipam"
)

// AssetSummary aggregates inventory counts for the dashboard.
type AssetSummary struct {
	Total       int `json:"total"`
	Physical    int `json:"physical"`
	Virtual     int `json:"virtual"`
	VMs         int `json:"vms"`
	Containers  int `json:"containers"`
	DockerHosts int `json:"docker_hosts"`
	Online      int `json:"online"`
	Offline     int `json:"offline"`
	Maintenance int `json:"maintenance"`
	Retired     int `json:"retired"`
	Unknown     int `json:"unknown"`
}

// NetworkSummary aggregates network counts.
type NetworkSummary struct {
	Subnets     int `json:"subnets"`
	VLANs       int `json:"vlans"`
	IPTracked   int `json:"ip_tracked"`
	IPAssigned  int `json:"ip_assigned"`
	IPReserved  int `json:"ip_reserved"`
	IPAvailable int `json:"ip_available"`
	IPConflicts int `json:"ip_conflicts"`
}

// DashboardStats is the full dashboard payload.
type DashboardStats struct {
	Assets            AssetSummary     `json:"assets"`
	Network           NetworkSummary   `json:"network"`
	Services          int              `json:"services"`
	Locations         int              `json:"locations"`
	Tags              int              `json:"tags"`
	Recent            []AuditEvent     `json:"recent_activity"`
	UpdatedRecently   []AssetStub      `json:"recently_updated_assets"`
	ByOS              []map[string]any `json:"by_os"`
	ServicesByHost    []map[string]any `json:"services_by_host"`
	SubnetUtilization []map[string]any `json:"subnet_utilization"`
	Alerts            AlertsSummary    `json:"alerts"`
}

// AlertsSummary surfaces actionable infrastructure conditions.
type AlertsSummary struct {
	IPConflicts         int `json:"ip_conflicts"`
	WarrantyExpiring    int `json:"warranty_expiring"`
	ServicesWithoutHost int `json:"services_without_host"`
}

// AssetStub is a lightweight asset reference for lists.
type AssetStub struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Hostname  string    `json:"hostname"`
	TypeLabel string    `json:"type_label"`
	Category  string    `json:"category"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Stats struct{ pool *pgxpool.Pool }

func NewStats(pool *pgxpool.Pool) *Stats { return &Stats{pool: pool} }

// Summary computes dashboard counters in a handful of aggregate queries.
func (s *Stats) Summary(ctx context.Context) (DashboardStats, error) {
	var out DashboardStats

	err := s.pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE t.category = 'physical'),
			count(*) FILTER (WHERE t.category = 'virtual'),
			count(*) FILTER (WHERE t.slug = 'vm'),
			count(*) FILTER (WHERE t.slug IN ('lxc', 'k8s-node')),
			count(*) FILTER (WHERE t.slug = 'docker'),
			count(*) FILTER (WHERE a.status = 'online'),
			count(*) FILTER (WHERE a.status = 'offline'),
			count(*) FILTER (WHERE a.status = 'maintenance'),
			count(*) FILTER (WHERE a.status = 'retired'),
			count(*) FILTER (WHERE a.status = 'unknown')
		FROM assets a
		JOIN asset_types t ON t.id = a.type_id`,
	).Scan(&out.Assets.Total, &out.Assets.Physical, &out.Assets.Virtual, &out.Assets.VMs,
		&out.Assets.Containers, &out.Assets.DockerHosts, &out.Assets.Online, &out.Assets.Offline,
		&out.Assets.Maintenance, &out.Assets.Retired, &out.Assets.Unknown)
	if err != nil {
		return out, err
	}

	err = s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM subnets),
			(SELECT count(*) FROM vlan_definitions),
			(SELECT count(*) FROM ip_addresses),
			(SELECT count(*) FROM ip_addresses WHERE status = 'assigned'),
			(SELECT count(*) FROM ip_addresses WHERE status = 'reserved'),
			(SELECT count(*) FROM ip_addresses WHERE status = 'available'),
			(SELECT count(*) FROM ip_addresses WHERE status = 'conflict')`,
	).Scan(&out.Network.Subnets, &out.Network.VLANs, &out.Network.IPTracked, &out.Network.IPAssigned,
		&out.Network.IPReserved, &out.Network.IPAvailable, &out.Network.IPConflicts)
	if err != nil {
		return out, err
	}

	err = s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM services),
			(SELECT count(*) FROM locations),
			(SELECT count(*) FROM tags)`,
	).Scan(&out.Services, &out.Locations, &out.Tags)
	if err != nil {
		return out, err
	}

	if out.Recent, err = s.recentActivity(ctx, 8); err != nil {
		return out, err
	}
	if out.UpdatedRecently, err = s.recentlyUpdatedAssets(ctx, 8); err != nil {
		return out, err
	}

	svc := NewServices(s.pool)
	if out.ByOS, err = svc.ByOS(ctx, 6); err != nil {
		return out, err
	}
	if out.ServicesByHost, err = svc.ByHost(ctx, 6); err != nil {
		return out, err
	}
	if out.SubnetUtilization, err = s.subnetUtilization(ctx, 6); err != nil {
		return out, err
	}

	var warranty int
	if err = s.pool.QueryRow(ctx, `
		SELECT count(*) FROM assets
		WHERE warranty_expires IS NOT NULL
		  AND warranty_expires BETWEEN CURRENT_DATE AND CURRENT_DATE + INTERVAL '60 days'`).Scan(&warranty); err != nil {
		return out, err
	}
	var noHost int
	if noHost, err = svc.CountWithoutDeployment(ctx); err != nil {
		return out, err
	}
	out.Alerts = AlertsSummary{
		IPConflicts:         out.Network.IPConflicts,
		WarrantyExpiring:    warranty,
		ServicesWithoutHost: noHost,
	}
	return out, nil
}

// subnetUtilization returns the most-utilized subnets with live percentages.
func (s *Stats) subnetUtilization(ctx context.Context, limit int) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, text(s.cidr), s.name,
		       count(*) FILTER (WHERE i.status = 'assigned'),
		       count(*) FILTER (WHERE i.status = 'reserved')
		FROM subnets s
		LEFT JOIN ip_addresses i ON i.subnet_id = s.id
		GROUP BY s.id, s.cidr, s.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type entry struct {
		id, cidr, name string
		used           int
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		var assigned, reserved int
		if err := rows.Scan(&e.id, &e.cidr, &e.name, &assigned, &reserved); err != nil {
			return nil, err
		}
		e.used = assigned + reserved
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		prefix, perr := ipam.ParseCIDR(e.cidr)
		if perr != nil {
			continue
		}
		usable := ipam.UsableCount(prefix)
		out = append(out, map[string]any{
			"id": e.id, "cidr": e.cidr, "name": e.name,
			"used": e.used, "usable": usable,
			"utilization": ipam.UtilizationPct(uint64(e.used), usable),
		})
	}
	// Simple insertion sort by utilization desc (small N).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j]["utilization"].(int) > out[j-1]["utilization"].(int); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Stats) recentActivity(ctx context.Context, limit int) ([]AuditEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, occurred_at, COALESCE(actor_user_id::text, ''), actor_label,
		       entity_type, COALESCE(entity_id::text, ''), entity_label, action, changes,
		       COALESCE(source_ip::text, '')
		FROM audit_events
		ORDER BY occurred_at DESC
		LIMIT $1`, limit)
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

func (s *Stats) recentlyUpdatedAssets(ctx context.Context, limit int) ([]AssetStub, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.name, a.hostname, t.label, t.category, a.status, a.updated_at
		FROM assets a
		JOIN asset_types t ON t.id = a.type_id
		ORDER BY a.updated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assets := []AssetStub{}
	for rows.Next() {
		var a AssetStub
		if err := rows.Scan(&a.ID, &a.Name, &a.Hostname, &a.TypeLabel, &a.Category, &a.Status, &a.UpdatedAt); err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}
