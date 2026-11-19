package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/labvory/internal/ipam"
)

// ---------- network contexts ----------

type NetworkContext struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	SubnetCount int       `json:"subnet_count"`
	VlanCount   int       `json:"vlan_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type NetworkContexts struct{ pool *pgxpool.Pool }

func NewNetworkContexts(pool *pgxpool.Pool) *NetworkContexts { return &NetworkContexts{pool: pool} }

func (s *NetworkContexts) List(ctx context.Context) ([]NetworkContext, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.slug, c.description, c.created_at,
		       (SELECT count(*) FROM subnets n WHERE n.context_id = c.id),
		       (SELECT count(*) FROM vlan_definitions v WHERE v.context_id = c.id)
		FROM network_contexts c ORDER BY c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NetworkContext{}
	for rows.Next() {
		var c NetworkContext
		if err := rows.Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.CreatedAt,
			&c.SubnetCount, &c.VlanCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *NetworkContexts) Create(ctx context.Context, name, slug, description string) (NetworkContext, error) {
	var c NetworkContext
	err := s.pool.QueryRow(ctx, `
		INSERT INTO network_contexts (name, slug, description) VALUES ($1,$2,$3)
		RETURNING id, created_at`, name, slug, description,
	).Scan(&c.ID, &c.CreatedAt)
	if err != nil {
		return NetworkContext{}, mapConstraint(err)
	}
	c.Name, c.Slug, c.Description = name, slug, description
	return c, nil
}

func (s *NetworkContexts) Update(ctx context.Context, id, name, description string) (NetworkContext, error) {
	var c NetworkContext
	err := s.pool.QueryRow(ctx, `
		UPDATE network_contexts SET name=$2, description=$3 WHERE id=$1
		RETURNING id, name, slug, description, created_at`, id, name, description,
	).Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NetworkContext{}, ErrNotFound
	}
	return c, mapConstraint(err)
}

func (s *NetworkContexts) Get(ctx context.Context, id string) (NetworkContext, error) {
	var c NetworkContext
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, slug, description, created_at FROM network_contexts WHERE id=$1`, id,
	).Scan(&c.ID, &c.Name, &c.Slug, &c.Description, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NetworkContext{}, ErrNotFound
	}
	return c, err
}

// ---------- VLANs ----------

type Vlan struct {
	ID          string    `json:"id"`
	ContextID   string    `json:"context_id"`
	ContextName string    `json:"context_name"`
	VlanID      int       `json:"vlan_id"`
	Name        string    `json:"name"`
	Purpose     string    `json:"purpose"`
	Description string    `json:"description"`
	SubnetCount int       `json:"subnet_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type Vlans struct{ pool *pgxpool.Pool }

func NewVlans(pool *pgxpool.Pool) *Vlans { return &Vlans{pool: pool} }

func (s *Vlans) List(ctx context.Context) ([]Vlan, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT v.id, v.context_id, c.name, v.vlan_id, v.name, v.purpose, v.description,
		       v.created_at,
		       (SELECT count(*) FROM subnets n WHERE n.vlan_id = v.id)
		FROM vlan_definitions v
		JOIN network_contexts c ON c.id = v.context_id
		ORDER BY c.name, v.vlan_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Vlan{}
	for rows.Next() {
		var v Vlan
		if err := rows.Scan(&v.ID, &v.ContextID, &v.ContextName, &v.VlanID, &v.Name,
			&v.Purpose, &v.Description, &v.CreatedAt, &v.SubnetCount); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Vlans) Get(ctx context.Context, id string) (Vlan, error) {
	var v Vlan
	err := s.pool.QueryRow(ctx, `
		SELECT v.id, v.context_id, c.name, v.vlan_id, v.name, v.purpose, v.description, v.created_at
		FROM vlan_definitions v JOIN network_contexts c ON c.id = v.context_id
		WHERE v.id=$1`, id,
	).Scan(&v.ID, &v.ContextID, &v.ContextName, &v.VlanID, &v.Name, &v.Purpose, &v.Description, &v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Vlan{}, ErrNotFound
	}
	return v, err
}

func (s *Vlans) Create(ctx context.Context, v *Vlan) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO vlan_definitions (context_id, vlan_id, name, purpose, description)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, created_at`,
		v.ContextID, v.VlanID, v.Name, v.Purpose, v.Description,
	).Scan(&v.ID, &v.CreatedAt)
	return mapConstraint(err)
}

func (s *Vlans) Update(ctx context.Context, v *Vlan) error {
	err := s.pool.QueryRow(ctx, `
		UPDATE vlan_definitions SET vlan_id=$2, name=$3, purpose=$4, description=$5
		WHERE id=$1
		RETURNING updated_at`, v.ID, v.VlanID, v.Name, v.Purpose, v.Description,
	).Scan(&v.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return mapConstraint(err)
}

func (s *Vlans) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM vlan_definitions WHERE id=$1`, id)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ---------- subnets ----------

type Subnet struct {
	ID           string   `json:"id"`
	CIDR         string   `json:"cidr"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ContextID    string   `json:"context_id"`
	ContextName  string   `json:"context_name"`
	VlanID       *string  `json:"vlan_id,omitempty"`
	VlanName     *string  `json:"vlan_name,omitempty"`
	VlanNumber   *int     `json:"vlan_number,omitempty"`
	Gateway      *string  `json:"gateway,omitempty"`
	DNSServers   []string `json:"dns_servers"`
	DHCPStart    *string  `json:"dhcp_start,omitempty"`
	DHCPEnd      *string  `json:"dhcp_end,omitempty"`
	LocationID   *string  `json:"location_id,omitempty"`
	LocationName *string  `json:"location_name,omitempty"`
	Environment  string   `json:"environment"`
	// Computed utilization.
	UsableCount  uint64    `json:"usable_count"`
	Assigned     int       `json:"assigned"`
	Reserved     int       `json:"reserved"`
	Excluded     int       `json:"excluded"`
	Conflicts    int       `json:"conflicts"`
	Available    int       `json:"available"` // tracked rows still marked available
	Utilization  int       `json:"utilization"`
	SubnetUsable bool      `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Subnets struct{ pool *pgxpool.Pool }

func NewSubnets(pool *pgxpool.Pool) *Subnets { return &Subnets{pool: pool} }

const subnetColumns = `
	SELECT s.id, text(s.cidr), s.name, s.description, s.context_id, c.name,
	       TO_JSONB(s.vlan_id) #>> '{}', v.name, v.vlan_id, host(s.gateway), s.dns_servers,
	       host(s.dhcp_start), host(s.dhcp_end),
	       TO_JSONB(s.location_id) #>> '{}', l.name, s.environment, s.created_at, s.updated_at
	FROM subnets s
	JOIN network_contexts c ON c.id = s.context_id
	LEFT JOIN vlan_definitions v ON v.id = s.vlan_id
	LEFT JOIN locations l ON l.id = s.location_id`

func scanSubnet(row pgx.Row) (Subnet, error) {
	var n Subnet
	err := row.Scan(&n.ID, &n.CIDR, &n.Name, &n.Description, &n.ContextID, &n.ContextName,
		&n.VlanID, &n.VlanName, &n.VlanNumber, &n.Gateway, &n.DNSServers,
		&n.DHCPStart, &n.DHCPEnd, &n.LocationID, &n.LocationName, &n.Environment,
		&n.CreatedAt, &n.UpdatedAt)
	return n, err
}

// List returns subnets matching the filter with live usage counters attached.
func (s *Subnets) List(ctx context.Context, q, contextID, vlanID string) ([]Subnet, error) {
	where := ""
	args := []any{}
	if q != "" {
		args = append(args, "%"+q+"%", "%"+q+"%")
		where = " WHERE (s.name ILIKE $1 OR text(s.cidr) ILIKE $2)"
	}
	if contextID != "" {
		args = append(args, contextID)
		where += whereStart(where) + " s.context_id = $" + itoa(len(args))
	}
	if vlanID != "" {
		args = append(args, vlanID)
		where += whereStart(where) + " s.vlan_id = $" + itoa(len(args))
	}

	rows, err := s.pool.Query(ctx, subnetColumns+where+" ORDER BY s.cidr", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Subnet{}
	for rows.Next() {
		n, err := scanSubnet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachUsage(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func whereStart(where string) string {
	if where == "" {
		return " WHERE"
	}
	return " AND"
}

func itoa(n int) string { return strconv.Itoa(n) }

// Get fetches one subnet.
func (s *Subnets) Get(ctx context.Context, id string) (Subnet, error) {
	n, err := scanSubnet(s.pool.QueryRow(ctx, subnetColumns+" WHERE s.id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Subnet{}, ErrNotFound
	}
	if err != nil {
		return Subnet{}, err
	}
	one := []Subnet{n}
	if err := s.attachUsage(ctx, one); err != nil {
		return Subnet{}, err
	}
	return one[0], nil
}

// GetByCIDR finds a subnet row by exact CIDR within a context (for imports/sync).
func (s *Subnets) GetByCIDR(ctx context.Context, contextID, cidr string) (Subnet, error) {
	n, err := scanSubnet(s.pool.QueryRow(ctx, subnetColumns+" WHERE s.context_id=$1 AND text(s.cidr)=$2", contextID, cidr))
	if errors.Is(err, pgx.ErrNoRows) {
		return Subnet{}, ErrNotFound
	}
	if err != nil {
		return Subnet{}, err
	}
	return n, nil
}

// attachUsage fills per-status counters and utilization for each subnet.
func (s *Subnets) attachUsage(ctx context.Context, subnets []Subnet) error {
	for i := range subnets {
		var assigned, reserved, excluded, conflicts, available int
		err := s.pool.QueryRow(ctx, `
			SELECT
				count(*) FILTER (WHERE status='assigned'),
				count(*) FILTER (WHERE status='reserved'),
				count(*) FILTER (WHERE status='excluded'),
				count(*) FILTER (WHERE status='conflict'),
				count(*) FILTER (WHERE status='available')
			FROM ip_addresses WHERE subnet_id=$1`, subnets[i].ID,
		).Scan(&assigned, &reserved, &excluded, &conflicts, &available)
		if err != nil {
			return err
		}
		subnets[i].Assigned = assigned
		subnets[i].Reserved = reserved
		subnets[i].Excluded = excluded
		subnets[i].Conflicts = conflicts
		subnets[i].Available = available

		if prefix, perr := ipam.ParseCIDR(subnets[i].CIDR); perr == nil {
			usable := ipam.UsableCount(prefix)
			subnets[i].UsableCount = usable
			subnets[i].Utilization = ipam.UtilizationPct(uint64(assigned+reserved), usable)
		}
	}
	return nil
}

func (s *Subnets) Create(ctx context.Context, n *Subnet) error {
	if n.DNSServers == nil {
		n.DNSServers = []string{}
	}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO subnets (cidr, name, description, context_id, vlan_id, gateway, dns_servers,
		                     dhcp_start, dhcp_end, location_id, environment)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id, created_at, updated_at`,
		n.CIDR, n.Name, n.Description, n.ContextID, nullableID(deref(n.VlanID)),
		nullableID(deref(n.Gateway)), n.DNSServers, nullableID(deref(n.DHCPStart)),
		nullableID(deref(n.DHCPEnd)), nullableID(deref(n.LocationID)), n.Environment,
	).Scan(&n.ID, &n.CreatedAt, &n.UpdatedAt)
	return mapConstraint(err)
}

func (s *Subnets) Update(ctx context.Context, n *Subnet) error {
	if n.DNSServers == nil {
		n.DNSServers = []string{}
	}
	err := s.pool.QueryRow(ctx, `
		UPDATE subnets SET cidr=$2, name=$3, description=$4, context_id=$5, vlan_id=$6,
		       gateway=$7, dns_servers=$8, dhcp_start=$9, dhcp_end=$10, location_id=$11,
		       environment=$12, updated_at=now()
		WHERE id=$1
		RETURNING updated_at`,
		n.ID, n.CIDR, n.Name, n.Description, n.ContextID, nullableID(deref(n.VlanID)),
		nullableID(deref(n.Gateway)), n.DNSServers, nullableID(deref(n.DHCPStart)),
		nullableID(deref(n.DHCPEnd)), nullableID(deref(n.LocationID)), n.Environment,
	).Scan(&n.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return mapConstraint(err)
}

// Delete removes a subnet. Rows marked available are auto-tracked placeholders and
// are removed with the subnet; real allocations (assigned/reserved/excluded/conflict)
// block deletion so nothing operator-declared is lost silently.
func (s *Subnets) Delete(ctx context.Context, id string) error {
	var real int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM ip_addresses WHERE subnet_id=$1 AND status <> 'available'`, id).Scan(&real); err != nil {
		return err
	}
	if real > 0 {
		return ErrHasChildren
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM subnets WHERE id=$1`, id)
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// AllCIDRsInContext returns parsed CIDR strings for overlap checks.
func (s *Subnets) AllCIDRsInContext(ctx context.Context, contextID string, excludeID string) ([]string, error) {
	var rows pgx.Rows
	var err error
	if excludeID == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT text(cidr) FROM subnets WHERE context_id=$1`, contextID)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT text(cidr) FROM subnets WHERE context_id=$1 AND id <> $2`, contextID, excludeID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------- IP addresses ----------

type IPAddress struct {
	ID             string     `json:"id"`
	SubnetID       string     `json:"subnet_id"`
	SubnetCIDR     string     `json:"subnet_cidr,omitempty"`
	Address        string     `json:"address"`
	Status         string     `json:"status"`
	Method         *string    `json:"assignment_method,omitempty"`
	Hostname       string     `json:"hostname"`
	Description    string     `json:"description"`
	InterfaceID    *string    `json:"interface_id,omitempty"`
	AssetID        *string    `json:"asset_id,omitempty"`
	AssetName      *string    `json:"asset_name,omitempty"`
	LastObservedAt *time.Time `json:"last_observed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	// Metadata holds observed facts under metadata.observed.* (e.g. the MAC
	// the ARP sweep last saw answering at this address). Declared fields stay
	// out of it.
	Metadata map[string]any `json:"metadata,omitempty"`
	// ObservedConflict is computed on list reads: the last ARP observation
	// within the window saw a different MAC than the assigned interface.
	ObservedConflict bool `json:"observed_conflict"`
}

type IPAddresses struct{ pool *pgxpool.Pool }

func NewIPAddresses(pool *pgxpool.Pool) *IPAddresses { return &IPAddresses{pool: pool} }

const ipColumns = `
	SELECT i.id, i.subnet_id, text(s.cidr), host(i.address), i.status, i.assignment_method,
	       i.hostname, i.description, TO_JSONB(i.interface_id) #>> '{}',
	       TO_JSONB(a.id) #>> '{}', a.name, i.last_observed_at, i.created_at, i.updated_at, i.metadata
	FROM ip_addresses i
	JOIN subnets s ON s.id = i.subnet_id
	LEFT JOIN network_interfaces ni ON ni.id = i.interface_id
	LEFT JOIN assets a ON a.id = COALESCE(ni.asset_id, (SELECT ia.asset_id FROM ip_assignments ia WHERE ia.ip_address_id = i.id AND ia.released_at IS NULL LIMIT 1))`

func scanIP(row pgx.Row) (IPAddress, error) {
	var ip IPAddress
	err := row.Scan(&ip.ID, &ip.SubnetID, &ip.SubnetCIDR, &ip.Address, &ip.Status, &ip.Method,
		&ip.Hostname, &ip.Description, &ip.InterfaceID, &ip.AssetID, &ip.AssetName,
		&ip.LastObservedAt, &ip.CreatedAt, &ip.UpdatedAt, &ip.Metadata)
	return ip, err
}

// observedConflictWindow bounds how recent an ARP mismatch observation must be
// to count as a live conflict.
const observedConflictWindow = 24 * time.Hour

// ipHasObservedConflict reports whether the row's metadata carries a recent
// ARP observation that saw a different MAC than the assigned interface.
func ipHasObservedConflict(m map[string]any, now time.Time) bool {
	if m == nil {
		return false
	}
	obs, _ := m["observed"].(map[string]any)
	arp, _ := obs["arp"].(map[string]any)
	if arp == nil || arp["matches"] != false {
		return false
	}
	ts, _ := arp["checked_at"].(string)
	t, err := time.Parse(time.RFC3339, ts)
	return err == nil && now.Sub(t) <= observedConflictWindow
}

// markObservedConflicts sets the computed flag on listed rows.
func markObservedConflicts(rows []IPAddress, now time.Time) {
	for i := range rows {
		rows[i].ObservedConflict = ipHasObservedConflict(rows[i].Metadata, now)
	}
}

// ListBySubnet returns tracked rows for one subnet, optionally filtered.
func (s *IPAddresses) ListBySubnet(ctx context.Context, subnetID, status, q string) ([]IPAddress, error) {
	where := " WHERE i.subnet_id = $1"
	args := []any{subnetID}
	if status != "" {
		args = append(args, status)
		where += " AND i.status = $" + itoa(len(args))
	}
	if q != "" {
		args = append(args, "%"+q+"%", "%"+q+"%")
		where += " AND (host(i.address) ILIKE $" + itoa(len(args)-1) + " OR i.hostname ILIKE $" + itoa(len(args)) + ")"
	}
	rows, err := s.pool.Query(ctx, ipColumns+where+" ORDER BY i.address", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IPAddress{}
	for rows.Next() {
		ip, err := scanIP(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ip)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	markObservedConflicts(out, time.Now())
	return out, nil
}

// ListGlobal returns tracked addresses across all subnets (global IP view).
// observedConflict filters to rows whose latest ARP observation disagrees
// with their assigned interface MAC.
func (s *IPAddresses) ListGlobal(ctx context.Context, status, q string, observedConflict bool, limit, offset int) ([]IPAddress, int, error) {
	where := ""
	args := []any{}
	if status != "" {
		args = append(args, status)
		where += whereStart(where) + " i.status = $" + itoa(len(args))
	}
	if observedConflict {
		where += whereStart(where) + ` (i.metadata->'observed'->'arp'->>'matches') = 'false'
		  AND (i.metadata->'observed'->'arp'->>'checked_at')::timestamptz > now() - make_interval(secs => $` + itoa(len(args)+1) + `)`
		args = append(args, int(observedConflictWindow.Seconds()))
	}
	if q != "" {
		args = append(args, "%"+q+"%", "%"+q+"%")
		where += whereStart(where) + " (host(i.address) ILIKE $" + itoa(len(args)-1) + " OR i.hostname ILIKE $" + itoa(len(args)) + ")"
	}

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM ip_addresses i`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := ipColumns + where + " ORDER BY i.address LIMIT $" + itoa(len(args)+1) + " OFFSET $" + itoa(len(args)+2)
	args = append(args, limit, offset)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []IPAddress{}
	for rows.Next() {
		ip, err := scanIP(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, ip)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	markObservedConflicts(out, time.Now())
	return out, total, nil
}

// SetObservedSub merges one observation under metadata.observed.<key> on an
// allocation, leaving other observation keys and declared fields untouched.
func (s *IPAddresses) SetObservedSub(ctx context.Context, ipID, key string, observed map[string]any) error {
	var meta map[string]any
	if err := s.pool.QueryRow(ctx, `SELECT metadata FROM ip_addresses WHERE id=$1`, ipID).Scan(&meta); err != nil {
		return err
	}
	if meta == nil {
		meta = map[string]any{}
	}
	obs, _ := meta["observed"].(map[string]any)
	if obs == nil {
		obs = map[string]any{}
	}
	obs[key] = observed
	meta["observed"] = obs
	merged, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE ip_addresses SET metadata=$2, updated_at=now() WHERE id=$1`, ipID, merged)
	return err
}

// CountObservedConflicts counts allocations whose latest ARP observation
// (within the window) saw a different MAC than their assigned interface.
func (s *IPAddresses) CountObservedConflicts(ctx context.Context, within time.Duration) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM ip_addresses
		WHERE (metadata->'observed'->'arp'->>'matches') = 'false'
		  AND (metadata->'observed'->'arp'->>'checked_at')::timestamptz > now() - make_interval(secs => $1)`,
		int(within.Seconds())).Scan(&n)
	return n, err
}

// Get fetches one tracked address.
func (s *IPAddresses) Get(ctx context.Context, id string) (IPAddress, error) {
	ip, err := scanIP(s.pool.QueryRow(ctx, ipColumns+" WHERE i.id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return IPAddress{}, ErrNotFound
	}
	return ip, err
}

// GetByAddress finds a tracked row inside a subnet.
func (s *IPAddresses) GetByAddress(ctx context.Context, subnetID, address string) (IPAddress, error) {
	ip, err := scanIP(s.pool.QueryRow(ctx, ipColumns+" WHERE i.subnet_id=$1 AND i.address=$2", subnetID, address))
	if errors.Is(err, pgx.ErrNoRows) {
		return IPAddress{}, ErrNotFound
	}
	return ip, err
}

// Allocate creates or claims a tracked address row and records the assignment
// history atomically. An existing row is only claimable while `available`.
func (s *IPAddresses) Allocate(ctx context.Context, ip *IPAddress, assetID string, assignedBy string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var existingID string
	err = tx.QueryRow(ctx,
		`SELECT id FROM ip_addresses WHERE subnet_id=$1 AND address=$2 FOR UPDATE`,
		ip.SubnetID, ip.Address).Scan(&existingID)
	if err == nil {
		// Row exists: only an available row can be claimed.
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM ip_addresses WHERE id=$1`, existingID).Scan(&status); err != nil {
			return err
		}
		if status != "available" {
			return ErrConflict
		}
		_, err = tx.Exec(ctx, `
			UPDATE ip_addresses SET status=$2, assignment_method=$3, hostname=$4, description=$5,
			       interface_id=$6, updated_at=now() WHERE id=$1`,
			existingID, ip.Status, ip.Method, ip.Hostname, ip.Description, nullableID(deref(ip.InterfaceID)))
		if err != nil {
			return err
		}
		ip.ID = existingID
	} else {
		err = tx.QueryRow(ctx, `
			INSERT INTO ip_addresses (subnet_id, address, status, assignment_method, hostname,
			                          description, interface_id)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			RETURNING id, created_at, updated_at`,
			ip.SubnetID, ip.Address, ip.Status, ip.Method, ip.Hostname, ip.Description,
			nullableID(deref(ip.InterfaceID)),
		).Scan(&ip.ID, &ip.CreatedAt, &ip.UpdatedAt)
		if err != nil {
			return mapConstraint(err)
		}
	}

	// History row for assigned/reserved states.
	if ip.Status == "assigned" || ip.Status == "reserved" {
		_, err = tx.Exec(ctx, `
			INSERT INTO ip_assignments (ip_address_id, asset_id, interface_id, method, assigned_by, note)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			ip.ID, nullableID(assetID), nullableID(deref(ip.InterfaceID)), ip.Method, nullableID(assignedBy), ip.Description)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Update modifies a tracked address; releasing an assignment closes its history row.
func (s *IPAddresses) Update(ctx context.Context, ip *IPAddress, assignedBy string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var oldStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM ip_addresses WHERE id=$1 FOR UPDATE`, ip.ID).Scan(&oldStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	_, err = tx.Exec(ctx, `
		UPDATE ip_addresses SET status=$2, assignment_method=$3, hostname=$4, description=$5,
		       interface_id=$6, updated_at=now() WHERE id=$1`,
		ip.ID, ip.Status, ip.Method, ip.Hostname, ip.Description, nullableID(deref(ip.InterfaceID)))
	if err != nil {
		return mapConstraint(err)
	}

	if oldStatus != ip.Status {
		if ip.Status == "available" || ip.Status == "excluded" {
			// Release any active assignment.
			_, _ = tx.Exec(ctx, `
				UPDATE ip_assignments SET released_at = now()
				WHERE ip_address_id = $1 AND released_at IS NULL`, ip.ID)
		}
		if ip.Status == "assigned" || ip.Status == "reserved" {
			_, err = tx.Exec(ctx, `
				INSERT INTO ip_assignments (ip_address_id, asset_id, interface_id, method, assigned_by, note)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				ip.ID, nullableID(deref(ip.AssetID)), nullableID(deref(ip.InterfaceID)), ip.Method,
				nullableID(assignedBy), ip.Description)
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}

// Delete releases and removes a tracked address.
func (s *IPAddresses) Delete(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, _ = tx.Exec(ctx, `
		UPDATE ip_assignments SET released_at = now()
		WHERE ip_address_id = $1 AND released_at IS NULL`, id)
	tag, err := tx.Exec(ctx, `DELETE FROM ip_addresses WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// TakenAddresses returns the allocated address strings of a subnet.
func (s *IPAddresses) TakenAddresses(ctx context.Context, subnetID string) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT host(address) FROM ip_addresses WHERE subnet_id=$1`, subnetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	taken := map[string]bool{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		taken[a] = true
	}
	return taken, rows.Err()
}

// MarkObserved records a discovery observation without touching declared state.
func (s *IPAddresses) MarkObserved(ctx context.Context, address string, observedAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE ip_addresses SET last_observed_at=$2 WHERE address=$1`, address, observedAt)
	return err
}

// ObserveLease marks an address as last seen by an integration and merges one
// observation under metadata.observed.<key> in a single statement. Declared
// fields (status, hostname, description, assignment) are never touched, and
// updated_at is deliberately left alone so observations don't pollute
// "recently updated" views. Returns whether a tracked row matched.
func (s *IPAddresses) ObserveLease(ctx context.Context, address string, observedAt time.Time, key string, observed map[string]any) (bool, error) {
	payload, err := json.Marshal(observed)
	if err != nil {
		return false, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE ip_addresses
		SET last_observed_at=$2,
		    metadata=jsonb_set(metadata, '{observed}',
		                       COALESCE(metadata->'observed', '{}'::jsonb) || jsonb_build_object($3::text, $4::jsonb))
		WHERE address=$1`,
		address, observedAt, key, string(payload))
	return tag.RowsAffected() > 0, err
}

// History returns assignment history for one address.
func (s *IPAddresses) History(ctx context.Context, ipID string) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT h.id, h.assigned_at, h.released_at, h.method, h.note,
		       COALESCE(TO_JSONB(h.asset_id) #>> '{}', ''), COALESCE(a.name, ''),
		       COALESCE(u.username, '')
		FROM ip_assignments h
		LEFT JOIN assets a ON a.id = h.asset_id
		LEFT JOIN users u ON u.id = h.assigned_by
		WHERE h.ip_address_id = $1
		ORDER BY h.assigned_at DESC`, ipID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id string
		var assignedAt time.Time
		var releasedAny any
		var method, note any
		var assetID, assetName, username string
		if err := rows.Scan(&id, &assignedAt, &releasedAny, &method, &note, &assetID, &assetName, &username); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "assigned_at": assignedAt, "released_at": releasedAny,
			"method": method, "note": note, "asset_id": assetID, "asset_name": assetName,
			"assigned_by": username,
		})
	}
	return out, rows.Err()
}

// ReleaseMany marks the given addresses available (any non-available status)
// and closes their active assignment history rows, mirroring the single-row
// release. Already-available ids are ignored; returns the released count.
func (s *IPAddresses) ReleaseMany(ctx context.Context, ids []string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE ip_assignments SET released_at = now()
		WHERE released_at IS NULL
		  AND ip_address_id IN (SELECT id FROM ip_addresses
		                         WHERE id = ANY($1::uuid[]) AND status <> 'available')`, ids)
	if err != nil {
		return 0, err
	}
	tag, err = s.pool.Exec(ctx, `
		UPDATE ip_addresses SET status = 'available', assignment_method = NULL, updated_at = now()
		WHERE id = ANY($1::uuid[]) AND status <> 'available'`, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteMany removes the given tracked addresses; assignment history cascades
// with the rows.
func (s *IPAddresses) DeleteMany(ctx context.Context, ids []string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ip_addresses WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteAllForSubnet removes every tracked address of the subnet; assignment
// history cascades with the rows.
func (s *IPAddresses) DeleteAllForSubnet(ctx context.Context, subnetID string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ip_addresses WHERE subnet_id = $1`, subnetID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
