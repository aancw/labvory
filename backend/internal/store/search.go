package store

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SearchResults groups matches by entity kind for the command palette.
type SearchResults struct {
	Assets    []SearchHit `json:"assets"`
	IPs       []SearchHit `json:"ips"`
	Subnets   []SearchHit `json:"subnets"`
	VLANs     []SearchHit `json:"vlans"`
	Services  []SearchHit `json:"services"`
	Locations []SearchHit `json:"locations"`
}

// SearchHit is one result row.
type SearchHit struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle"`
	EntityType  string `json:"entity_type"`
	Icon        string `json:"icon"`
}

type Search struct{ pool *pgxpool.Pool }

func NewSearch(pool *pgxpool.Pool) *Search { return &Search{pool: pool} }

// Query runs the global search. `q` is matched against names, hostnames, serials, IPs and MACs.
func (s *Search) Query(ctx context.Context, q string) (SearchResults, error) {
	var out SearchResults
	pattern := "%" + q + "%"

	assets, err := s.assets(ctx, pattern)
	if err != nil {
		return out, err
	}
	out.Assets = assets

	if out.IPs, err = s.ips(ctx, pattern); err != nil {
		return out, err
	}
	if out.Subnets, err = s.subnets(ctx, pattern); err != nil {
		return out, err
	}
	if out.VLANs, err = s.vlans(ctx, pattern); err != nil {
		return out, err
	}
	if out.Services, err = s.services(ctx, pattern); err != nil {
		return out, err
	}
	out.Locations, err = s.locations(ctx, pattern)
	return out, err
}

func (s *Search) assets(ctx context.Context, pattern string) ([]SearchHit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.name, a.hostname, t.label, t.category
		FROM assets a
		JOIN asset_types t ON t.id = a.type_id
		WHERE a.name ILIKE $1 OR a.hostname ILIKE $1 OR a.serial_number ILIKE $1
		   OR a.manufacturer ILIKE $1 OR a.model ILIKE $1
		ORDER BY a.updated_at DESC
		LIMIT 8`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var id, name, hostname, label, category string
		if err := rows.Scan(&id, &name, &hostname, &label, &category); err != nil {
			return nil, err
		}
		subtitle := label
		if hostname != "" {
			subtitle = label + " · " + hostname
		}
		hits = append(hits, SearchHit{ID: id, Title: name, Subtitle: subtitle, EntityType: "asset", Icon: iconForCategory(category)})
	}
	return hits, rows.Err()
}

func (s *Search) ips(ctx context.Context, pattern string) ([]SearchHit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, host(i.address), i.hostname, i.status
		FROM ip_addresses i
		WHERE host(i.address) ILIKE $1 OR i.hostname ILIKE $1
		ORDER BY i.address
		LIMIT 8`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var id, address, hostname, status string
		if err := rows.Scan(&id, &address, &hostname, &status); err != nil {
			return nil, err
		}
		title := address
		if hostname != "" {
			title = address + " · " + hostname
		}
		hits = append(hits, SearchHit{ID: id, Title: title, Subtitle: "IP address · " + status, EntityType: "ip", Icon: "network"})
	}
	return hits, rows.Err()
}

func (s *Search) subnets(ctx context.Context, pattern string) ([]SearchHit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, text(cidr), name FROM subnets
		WHERE text(cidr) ILIKE $1 OR name ILIKE $1
		ORDER BY cidr
		LIMIT 8`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var id, cidr, name string
		if err := rows.Scan(&id, &cidr, &name); err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{ID: id, Title: cidr, Subtitle: "Subnet · " + name, EntityType: "subnet", Icon: "network"})
	}
	return hits, rows.Err()
}

func (s *Search) vlans(ctx context.Context, pattern string) ([]SearchHit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, vlan_id, name, purpose FROM vlan_definitions
		WHERE name ILIKE $1 OR vlan_id::text ILIKE $1 OR purpose ILIKE $1
		ORDER BY vlan_id
		LIMIT 8`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var id string
		var vlanID int
		var name, purpose string
		if err := rows.Scan(&id, &vlanID, &name, &purpose); err != nil {
			return nil, err
		}
		subtitle := "VLAN · ID " + strconv.Itoa(vlanID)
		if purpose != "" {
			subtitle += " · " + purpose
		}
		hits = append(hits, SearchHit{
			ID:         id,
			Title:      name,
			Subtitle:   subtitle,
			EntityType: "vlan",
			Icon:       "layers",
		})
	}
	return hits, rows.Err()
}

func (s *Search) services(ctx context.Context, pattern string) ([]SearchHit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, category FROM services
		WHERE name ILIKE $1 OR description ILIKE $1 OR category ILIKE $1
		ORDER BY name
		LIMIT 8`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var id, name, category string
		if err := rows.Scan(&id, &name, &category); err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{ID: id, Title: name, Subtitle: "Service · " + category, EntityType: "service", Icon: "app-window"})
	}
	return hits, rows.Err()
}

func (s *Search) locations(ctx context.Context, pattern string) ([]SearchHit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description FROM locations
		WHERE name ILIKE $1 OR description ILIKE $1
		ORDER BY name
		LIMIT 5`, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []SearchHit{}
	for rows.Next() {
		var id, name, description string
		if err := rows.Scan(&id, &name, &description); err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{ID: id, Title: name, Subtitle: "Location" + locationDesc(description), EntityType: "location", Icon: "map-pin"})
	}
	return hits, rows.Err()
}

func iconForCategory(category string) string {
	if category == "virtual" {
		return "monitor-dot"
	}
	return "server"
}

func locationDesc(d string) string {
	if d == "" {
		return ""
	}
	return " · " + d
}
