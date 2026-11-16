package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ImportRowResult is the per-row verdict for import previews.
type ImportRowResult struct {
	Index  int            `json:"index"`
	Status string         `json:"status"` // ok | error | duplicate
	Detail string         `json:"detail,omitempty"`
	Data   map[string]any `json:"data"`
}

// ImportSummary is the outcome of a committed import.
type ImportSummary struct {
	Created int      `json:"created"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors,omitempty"`
}

// Importer validates and writes bulk imports. CSV parsing and column mapping
// happen client-side; the importer only sees mapped JSON rows.
type Importer struct {
	pool *pgxpool.Pool
}

func NewImporter(pool *pgxpool.Pool) *Importer { return &Importer{pool: pool} }

func NewImporterTx(tx pgx.Tx) *importerTx { return &importerTx{tx: tx} }

type importerTx struct{ tx pgx.Tx }

// ---------- assets ----------

func (s *Importer) PreviewAssets(ctx context.Context, rows []map[string]any) ([]ImportRowResult, error) {
	results := make([]ImportRowResult, len(rows))
	for i, row := range rows {
		res := ImportRowResult{Index: i, Data: row}
		name, _ := row["name"].(string)
		typeSlug, _ := row["type_slug"].(string)
		serial, _ := row["serial_number"].(string)
		if name == "" || typeSlug == "" {
			res.Status = "error"
			res.Detail = "name and type_slug are required"
			results[i] = res
			continue
		}
		var dup int
		err := s.pool.QueryRow(ctx, `
			SELECT count(*) FROM assets a JOIN asset_types t ON t.id = a.type_id
			WHERE (t.slug = $2 AND lower(a.name) = lower($1))
			   OR ($3 <> '' AND a.serial_number = $3)`,
			name, typeSlug, serial).Scan(&dup)
		if err != nil {
			res.Status = "error"
			res.Detail = err.Error()
		} else if dup > 0 {
			res.Status = "duplicate"
			res.Detail = "an asset with this name/type or serial already exists"
		} else {
			res.Status = "ok"
		}
		results[i] = res
	}
	return results, nil
}

// CommitAssets inserts valid rows transactionally. Duplicates are skipped, never overwritten.
func (s *Importer) CommitAssets(ctx context.Context, rows []map[string]any, skipDuplicates bool) (ImportSummary, error) {
	summary := ImportSummary{}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return summary, err
	}
	defer tx.Rollback(ctx)
	it := NewImporterTx(tx)
	for i, row := range rows {
		created, status, detail, err := it.insertAsset(ctx, row, skipDuplicates)
		if err != nil {
			return summary, fmt.Errorf("row %d: %w", i, err)
		}
		switch status {
		case "created":
			summary.Created++
			_ = created
		case "duplicate":
			summary.Skipped++
		case "error":
			summary.Errors = append(summary.Errors, fmt.Sprintf("row %d: %s", i, detail))
		}
	}
	if len(summary.Errors) > 0 {
		return summary, fmt.Errorf("%d rows failed validation", len(summary.Errors))
	}
	return summary, tx.Commit(ctx)
}

func (it *importerTx) insertAsset(ctx context.Context, row map[string]any, skipDuplicates bool) (created bool, status, detail string, err error) {
	name, _ := row["name"].(string)
	typeSlug, _ := row["type_slug"].(string)
	serial, _ := row["serial_number"].(string)
	hostname, _ := row["hostname"].(string)
	manufacturer, _ := row["manufacturer"].(string)
	model, _ := row["model"].(string)
	osName, _ := row["os_name"].(string)
	osVersion, _ := row["os_version"].(string)
	environment, _ := row["environment"].(string)
	statusField, _ := row["status"].(string)

	if name == "" || typeSlug == "" {
		return false, "error", "name and type_slug are required", nil
	}

	var exists int
	err = it.tx.QueryRow(ctx, `
		SELECT count(*) FROM assets a JOIN asset_types t ON t.id = a.type_id
		WHERE (t.slug = $2 AND lower(a.name) = lower($1))
		   OR ($3 <> '' AND a.serial_number = $3)`,
		name, typeSlug, serial).Scan(&exists)
	if err != nil {
		return false, "error", err.Error(), err
	}
	if exists > 0 {
		if skipDuplicates {
			return false, "duplicate", "", nil
		}
		return false, "error", "duplicate asset", nil
	}

	if environment == "" {
		environment = "production"
	}
	if statusField == "" {
		statusField = "unknown"
	}

	tag, err := it.tx.Exec(ctx, `
		INSERT INTO assets (name, hostname, type_id, status, environment, manufacturer, model, serial_number, os_name, os_version, source)
		SELECT $1, $2, t.id, $3, $4, $5, $6, $7, $8, $9, 'import'
		FROM asset_types t WHERE t.slug = $10`,
		name, hostname, statusField, environment, manufacturer, model, serial, osName, osVersion, typeSlug)
	if err != nil {
		return false, "error", err.Error(), err
	}
	if tag.RowsAffected() == 0 {
		return false, "error", "unknown type_slug: " + typeSlug, nil
	}
	return true, "created", "", nil
}

// ---------- subnets ----------

func (s *Importer) PreviewSubnets(ctx context.Context, rows []map[string]any, contextID string) ([]ImportRowResult, error) {
	results := make([]ImportRowResult, len(rows))
	for i, row := range rows {
		res := ImportRowResult{Index: i, Data: row}
		cidr, _ := row["cidr"].(string)
		name, _ := row["name"].(string)
		if cidr == "" || name == "" {
			res.Status = "error"
			res.Detail = "cidr and name are required"
			results[i] = res
			continue
		}
		var dup int
		err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM subnets WHERE context_id=$1 AND text(cidr)=$2`, contextID, cidr).Scan(&dup)
		if err != nil {
			res.Status = "error"
			res.Detail = err.Error()
		} else if dup > 0 {
			res.Status = "duplicate"
			res.Detail = "subnet exists in this context"
		} else {
			res.Status = "ok"
		}
		results[i] = res
	}
	return results, nil
}

func (s *Importer) CommitSubnets(ctx context.Context, rows []map[string]any, contextID string, skipDuplicates bool) (ImportSummary, error) {
	summary := ImportSummary{}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return summary, err
	}
	defer tx.Rollback(ctx)

	for i, row := range rows {
		cidr, _ := row["cidr"].(string)
		name, _ := row["name"].(string)
		description, _ := row["description"].(string)
		gateway, _ := row["gateway"].(string)
		environment, _ := row["environment"].(string)
		if environment == "" {
			environment = "production"
		}
		if cidr == "" || name == "" {
			summary.Errors = append(summary.Errors, fmt.Sprintf("row %d: cidr and name are required", i))
			continue
		}
		var dup int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM subnets WHERE context_id=$1 AND text(cidr)=$2`, contextID, cidr).Scan(&dup); err != nil {
			return summary, err
		}
		if dup > 0 {
			if skipDuplicates {
				summary.Skipped++
				continue
			}
			summary.Errors = append(summary.Errors, fmt.Sprintf("row %d: duplicate subnet", i))
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO subnets (cidr, name, description, context_id, gateway, environment)
			VALUES ($1,$2,$3,$4, NULLIF($5,'')::inet, $6)`,
			cidr, name, description, contextID, gateway, environment); err != nil {
			return summary, fmt.Errorf("row %d: %w", i, err)
		}
		summary.Created++
	}
	if len(summary.Errors) > 0 {
		return summary, fmt.Errorf("%d rows failed", len(summary.Errors))
	}
	return summary, tx.Commit(ctx)
}

// ---------- ip addresses ----------

func (s *Importer) PreviewIPs(ctx context.Context, rows []map[string]any, subnetID string) ([]ImportRowResult, error) {
	results := make([]ImportRowResult, len(rows))
	for i, row := range rows {
		res := ImportRowResult{Index: i, Data: row}
		address, _ := row["address"].(string)
		if address == "" {
			res.Status = "error"
			res.Detail = "address is required"
			results[i] = res
			continue
		}
		var dup int
		err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM ip_addresses WHERE subnet_id=$1 AND address=$2`, subnetID, address).Scan(&dup)
		if err != nil {
			res.Status = "error"
			res.Detail = err.Error()
		} else if dup > 0 {
			res.Status = "duplicate"
			res.Detail = "address already tracked in this subnet"
		} else {
			res.Status = "ok"
		}
		results[i] = res
	}
	return results, nil
}

func (s *Importer) CommitIPs(ctx context.Context, rows []map[string]any, subnetID string, skipDuplicates bool) (ImportSummary, error) {
	summary := ImportSummary{}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return summary, err
	}
	defer tx.Rollback(ctx)

	for i, row := range rows {
		address, _ := row["address"].(string)
		hostname, _ := row["hostname"].(string)
		status, _ := row["status"].(string)
		description, _ := row["description"].(string)
		if status == "" {
			status = "available"
		}
		if address == "" {
			summary.Errors = append(summary.Errors, fmt.Sprintf("row %d: address is required", i))
			continue
		}
		var dup int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM ip_addresses WHERE subnet_id=$1 AND address=$2`, subnetID, address).Scan(&dup); err != nil {
			return summary, err
		}
		if dup > 0 {
			if skipDuplicates {
				summary.Skipped++
				continue
			}
			summary.Errors = append(summary.Errors, fmt.Sprintf("row %d: duplicate address", i))
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO ip_addresses (subnet_id, address, status, hostname, description)
			VALUES ($1, $2::inet, $3, $4, $5)`,
			subnetID, address, status, hostname, description); err != nil {
			return summary, fmt.Errorf("row %d: %w", i, err)
		}
		summary.Created++
	}
	if len(summary.Errors) > 0 {
		return summary, fmt.Errorf("%d rows failed", len(summary.Errors))
	}
	return summary, tx.Commit(ctx)
}

// ---------- full inventory export / restore ----------

// ExportInventory dumps the whole inventory with stable natural keys.
func (s *Importer) ExportInventory(ctx context.Context) (map[string]any, error) {
	out := map[string]any{}

	// Locations.
	locRows, err := s.pool.Query(ctx, `SELECT l.name, l.slug, l.description, COALESCE(p.slug, '') FROM locations l LEFT JOIN locations p ON p.id = l.parent_id`)
	if err != nil {
		return nil, err
	}
	locations := []map[string]any{}
	for locRows.Next() {
		var name, slug, description, parent string
		if err := locRows.Scan(&name, &slug, &description, &parent); err != nil {
			locRows.Close()
			return nil, err
		}
		locations = append(locations, map[string]any{"name": name, "slug": slug, "description": description, "parent_slug": parent})
	}
	locRows.Close()
	out["locations"] = locations

	// Tags.
	tagRows, err := s.pool.Query(ctx, `SELECT name, color FROM tags`)
	if err != nil {
		return nil, err
	}
	tags := []map[string]any{}
	for tagRows.Next() {
		var name, color string
		if err := tagRows.Scan(&name, &color); err != nil {
			tagRows.Close()
			return nil, err
		}
		tags = append(tags, map[string]any{"name": name, "color": color})
	}
	tagRows.Close()
	out["tags"] = tags

	// Contexts + VLANs.
	ctxRows, err := s.pool.Query(ctx, `SELECT name, slug, description FROM network_contexts WHERE slug <> 'default'`)
	if err != nil {
		return nil, err
	}
	contexts := []map[string]any{}
	for ctxRows.Next() {
		var name, slug, description string
		if err := ctxRows.Scan(&name, &slug, &description); err != nil {
			ctxRows.Close()
			return nil, err
		}
		contexts = append(contexts, map[string]any{"name": name, "slug": slug, "description": description})
	}
	ctxRows.Close()
	out["network_contexts"] = contexts

	vlanRows, err := s.pool.Query(ctx, `
		SELECT v.vlan_id, v.name, v.purpose, v.description, c.slug
		FROM vlan_definitions v JOIN network_contexts c ON c.id = v.context_id`)
	if err != nil {
		return nil, err
	}
	vlans := []map[string]any{}
	for vlanRows.Next() {
		var id int
		var name, purpose, description, ctxSlug string
		if err := vlanRows.Scan(&id, &name, &purpose, &description, &ctxSlug); err != nil {
			vlanRows.Close()
			return nil, err
		}
		vlans = append(vlans, map[string]any{"vlan_id": id, "name": name, "purpose": purpose, "description": description, "context_slug": ctxSlug})
	}
	vlanRows.Close()
	out["vlans"] = vlans

	// Assets (natural key: type slug + name; parent by name).
	assetRows, err := s.pool.Query(ctx, `
		SELECT a.name, a.hostname, t.slug, a.status, a.lifecycle, a.environment,
		       COALESCE(l.slug, ''), COALESCE(p.name, ''),
		       a.manufacturer, a.model, a.serial_number, a.os_name, a.os_version,
		       a.cpu_model, a.cpu_cores, a.ram_gb, a.storage_gb, a.notes, a.source
		FROM assets a
		JOIN asset_types t ON t.id = a.type_id
		LEFT JOIN locations l ON l.id = a.location_id
		LEFT JOIN assets p ON p.id = a.parent_id
		ORDER BY (CASE WHEN EXISTS (SELECT 1 FROM assets c WHERE c.parent_id = a.id) THEN 0 ELSE 1 END), a.name`)
	if err != nil {
		return nil, err
	}
	assets := []map[string]any{}
	for assetRows.Next() {
		var name, typeSlug, status, lifecycle, environment, location, parent string
		var hostname, manufacturer, model, serial, osName, osVersion, cpuModel, notes, source string
		var cores *int
		var ram, storage *float64
		if err := assetRows.Scan(&name, &hostname, &typeSlug, &status, &lifecycle, &environment,
			&location, &parent, &manufacturer, &model, &serial, &osName, &osVersion,
			&cpuModel, &cores, &ram, &storage, &notes, &source); err != nil {
			assetRows.Close()
			return nil, err
		}
		entry := map[string]any{
			"name": name, "hostname": hostname, "type_slug": typeSlug, "status": status,
			"lifecycle": lifecycle, "environment": environment, "location_slug": location,
			"parent_name": parent, "manufacturer": manufacturer, "model": model,
			"serial_number": serial, "os_name": osName, "os_version": osVersion,
			"cpu_model": cpuModel, "notes": notes, "source": source,
		}
		if cores != nil {
			entry["cpu_cores"] = *cores
		}
		if ram != nil {
			entry["ram_gb"] = *ram
		}
		if storage != nil {
			entry["storage_gb"] = *storage
		}
		assets = append(assets, entry)
	}
	assetRows.Close()
	out["assets"] = assets

	// Subnets + IPs.
	subRows, err := s.pool.Query(ctx, `
		SELECT text(s.cidr), s.name, s.description, c.slug, v.vlan_id, host(s.gateway),
		       s.dns_servers, s.environment
		FROM subnets s
		JOIN network_contexts c ON c.id = s.context_id
		LEFT JOIN vlan_definitions v ON v.id = s.vlan_id
		ORDER BY s.cidr`)
	if err != nil {
		return nil, err
	}
	subnets := []map[string]any{}
	for subRows.Next() {
		var cidr, name, description, ctxSlug, environment string
		var vlanID *int
		var gateway *string
		var dns []string
		if err := subRows.Scan(&cidr, &name, &description, &ctxSlug, &vlanID, &gateway, &dns, &environment); err != nil {
			subRows.Close()
			return nil, err
		}
		subnets = append(subnets, map[string]any{
			"cidr": cidr, "name": name, "description": description,
			"context_slug": ctxSlug, "vlan_id": vlanID, "gateway": gateway,
			"dns_servers": dns, "environment": environment,
		})
	}
	subRows.Close()
	out["subnets"] = subnets

	ipRows, err := s.pool.Query(ctx, `
		SELECT host(i.address), i.status, i.assignment_method, i.hostname, i.description, text(s.cidr)
		FROM ip_addresses i JOIN subnets s ON s.id = i.subnet_id
		WHERE i.status <> 'available'
		ORDER BY i.address`)
	if err != nil {
		return nil, err
	}
	ips := []map[string]any{}
	for ipRows.Next() {
		var address, status, subnetCIDR string
		var method, hostname, description *string
		if err := ipRows.Scan(&address, &status, &method, &hostname, &description, &subnetCIDR); err != nil {
			ipRows.Close()
			return nil, err
		}
		ips = append(ips, map[string]any{
			"address": address, "status": status, "assignment_method": method,
			"hostname": hostname, "description": description, "subnet_cidr": subnetCIDR,
		})
	}
	ipRows.Close()
	out["ip_addresses"] = ips

	// Services + endpoints + deployments.
	svcRows, err := s.pool.Query(ctx, `
		SELECT s.name, s.description, s.category, s.deployment_type, s.environment, s.status,
		       s.repo_url, s.docs_url, s.notes
		FROM services s ORDER BY s.name`)
	if err != nil {
		return nil, err
	}
	services := []map[string]any{}
	for svcRows.Next() {
		var name, description, category, deploymentType, environment, status, repo, docs, notes string
		if err := svcRows.Scan(&name, &description, &category, &deploymentType, &environment, &status, &repo, &docs, &notes); err != nil {
			svcRows.Close()
			return nil, err
		}
		services = append(services, map[string]any{
			"name": name, "description": description, "category": category,
			"deployment_type": deploymentType, "environment": environment, "status": status,
			"repo_url": repo, "docs_url": docs, "notes": notes,
		})
	}
	svcRows.Close()
	out["services"] = services

	out["version"] = 1
	return out, nil
}

// RestoreInventory re-imports an exported inventory idempotently. Existing
// entities (matched by natural key) are left untouched — never overwritten.
func (s *Importer) RestoreInventory(ctx context.Context, doc map[string]any, assignedBy string) (map[string]any, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	created := map[string]int{
		"locations": 0, "tags": 0, "network_contexts": 0, "vlans": 0,
		"assets": 0, "subnets": 0, "ip_addresses": 0, "services": 0,
	}

	// Locations first.
	for _, raw := range asRows(doc["locations"]) {
		name := str(raw["name"])
		slug := str(raw["slug"])
		parent := str(raw["parent_slug"])
		var parentID *string
		if parent != "" {
			var pid string
			if err := tx.QueryRow(ctx, `SELECT id FROM locations WHERE slug=$1`, parent).Scan(&pid); err == nil {
				parentID = &pid
			}
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO locations (name, slug, description, parent_id) VALUES ($1,$2,$3,$4)
			ON CONFLICT (slug) DO NOTHING`, name, slug, str(raw["description"]), nullableID(deref(parentID)))
		if err != nil {
			return nil, fmt.Errorf("location %s: %w", slug, err)
		}
		created["locations"] += int(tag.RowsAffected())
	}

	// Tags.
	for _, raw := range asRows(doc["tags"]) {
		tag, err := tx.Exec(ctx, `
			INSERT INTO tags (name, color) VALUES ($1, $2) ON CONFLICT (name) DO NOTHING`,
			str(raw["name"]), strOr(raw["color"], "zinc"))
		if err != nil {
			return nil, fmt.Errorf("tag %s: %w", str(raw["name"]), err)
		}
		created["tags"] += int(tag.RowsAffected())
	}

	// Extra contexts (default already exists).
	for _, raw := range asRows(doc["network_contexts"]) {
		tag, err := tx.Exec(ctx, `
			INSERT INTO network_contexts (name, slug, description) VALUES ($1,$2,$3)
			ON CONFLICT (slug) DO NOTHING`, str(raw["name"]), str(raw["slug"]), str(raw["description"]))
		if err != nil {
			return nil, fmt.Errorf("context %s: %w", str(raw["slug"]), err)
		}
		created["network_contexts"] += int(tag.RowsAffected())
	}

	// VLANs (context slug + vlan id natural key).
	for _, raw := range asRows(doc["vlans"]) {
		vlanID, _ := toInt(raw["vlan_id"])
		tag, err := tx.Exec(ctx, `
			INSERT INTO vlan_definitions (context_id, vlan_id, name, purpose, description)
			SELECT c.id, $1, $2, $3, $4 FROM network_contexts c WHERE c.slug = $5
			ON CONFLICT (context_id, vlan_id) DO NOTHING`,
			vlanID, str(raw["name"]), str(raw["purpose"]), str(raw["description"]), str(raw["context_slug"]))
		if err != nil {
			return nil, fmt.Errorf("vlan %d: %w", vlanID, err)
		}
		created["vlans"] += int(tag.RowsAffected())
	}

	// Assets: parents are emitted before children by the exporter; enforce order
	// by sorting again defensively (rows without parent first).
	assetRows := asRows(doc["assets"])
	sort.SliceStable(assetRows, func(i, j int) bool {
		return str(assetRows[i]["parent_name"]) < str(assetRows[j]["parent_name"])
	})
	for _, raw := range assetRows {
		name := str(raw["name"])
		parent := str(raw["parent_name"])
		location := str(raw["location_slug"])

		// assets.name carries no unique constraint, so idempotency is explicit:
		// existing assets are never overwritten (spec rule).
		var exists int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM assets a JOIN asset_types t ON t.id = a.type_id
			WHERE lower(a.name) = lower($1) AND t.slug = $2`,
			name, str(raw["type_slug"])).Scan(&exists); err != nil {
			return nil, fmt.Errorf("asset %s: %w", name, err)
		}
		if exists > 0 {
			continue
		}

		tag, err := tx.Exec(ctx, `
			INSERT INTO assets (name, hostname, type_id, status, lifecycle, environment,
			                    location_id, parent_id, manufacturer, model, serial_number,
			                    os_name, os_version, cpu_model, cpu_cores, ram_gb, storage_gb,
			                    notes, source)
			SELECT $1, $2, t.id, $3, $4, $5,
			       (SELECT id FROM locations WHERE slug = NULLIF($6,'') LIMIT 1),
			       (SELECT id FROM assets WHERE name = $7 ORDER BY created_at LIMIT 1),
			       $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
			FROM asset_types t WHERE t.slug = $19`,
			name, str(raw["hostname"]), strOr(raw["status"], "unknown"),
			strOr(raw["lifecycle"], "active"), strOr(raw["environment"], "production"),
			location, parent,
			str(raw["manufacturer"]), str(raw["model"]), str(raw["serial_number"]),
			str(raw["os_name"]), str(raw["os_version"]), str(raw["cpu_model"]),
			toIntAny(raw["cpu_cores"]), toFloatAny(raw["ram_gb"]), toFloatAny(raw["storage_gb"]),
			str(raw["notes"]), strOr(raw["source"], "import"),
			str(raw["type_slug"]))
		if err != nil {
			return nil, fmt.Errorf("asset %s: %w", name, err)
		}
		created["assets"] += int(tag.RowsAffected())
	}

	// Subnets.
	for _, raw := range asRows(doc["subnets"]) {
		vlanIDAny := raw["vlan_id"]
		var vlanID *int
		if vlanIDAny != nil {
			if v, ok := toInt(vlanIDAny); ok {
				vlanID = &v
			}
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO subnets (cidr, name, description, context_id, vlan_id, gateway, dns_servers, environment)
			SELECT $1, $2, $3, c.id, $4, NULLIF($5,'')::inet, $6, $7
			FROM network_contexts c WHERE c.slug = $8
			ON CONFLICT (context_id, cidr) DO NOTHING`,
			str(raw["cidr"]), str(raw["name"]), str(raw["description"]),
			nullableIntPtr(vlanID), str(raw["gateway"]), strSlice(raw["dns_servers"]),
			strOr(raw["environment"], "production"), str(raw["context_slug"]))
		if err != nil {
			return nil, fmt.Errorf("subnet %s: %w", str(raw["cidr"]), err)
		}
		created["subnets"] += int(tag.RowsAffected())
	}

	// IP addresses (skip available placeholders).
	for _, raw := range asRows(doc["ip_addresses"]) {
		address := str(raw["address"])
		if address == "" {
			continue
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO ip_addresses (subnet_id, address, status, assignment_method, hostname, description)
			SELECT s.id, $1::inet, $2, NULLIF($3,''), NULLIF($4,''), NULLIF($5,'')
			FROM subnets s WHERE text(s.cidr) = $6
			ON CONFLICT (subnet_id, address) DO NOTHING`,
			address, strOr(raw["status"], "available"), str(raw["assignment_method"]),
			str(raw["hostname"]), str(raw["description"]), str(raw["subnet_cidr"]))
		if err != nil {
			return nil, fmt.Errorf("ip %s: %w", address, err)
		}
		created["ip_addresses"] += int(tag.RowsAffected())
	}

	// Services.
	for _, raw := range asRows(doc["services"]) {
		tag, err := tx.Exec(ctx, `
			INSERT INTO services (name, slug, description, category, deployment_type, environment,
			                     status, repo_url, docs_url, notes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (slug) DO NOTHING`,
			str(raw["name"]), serviceSlug(str(raw["name"])), str(raw["description"]),
			strOr(raw["category"], "other"), strOr(raw["deployment_type"], "native"),
			strOr(raw["environment"], "production"), strOr(raw["status"], "unknown"),
			str(raw["repo_url"]), str(raw["docs_url"]), str(raw["notes"]))
		if err != nil {
			return nil, fmt.Errorf("service %s: %w", str(raw["name"]), err)
		}
		created["services"] += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	summary := map[string]any{"created": created, "total_created": totalCreated(created)}
	_ = assignedBy
	return summary, nil
}

func serviceSlug(name string) string {
	out := ""
	lastDash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out += string(r)
			lastDash = false
		} else if !lastDash && out != "" {
			out += "-"
			lastDash = true
		}
	}
	out = trimDashes(out)
	if out == "" {
		out = "service"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}

func trimDashes(s string) string {
	for len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == '-' {
		s = s[:len(s)-1]
	}
	return s
}

func asRows(v any) []map[string]any {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func strOr(v any, fallback string) string {
	if s := str(v); s != "" {
		return s
	}
	return fallback
}

func strSlice(v any) []string {
	if arr, ok := v.([]any); ok {
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	if _, ok := v.([]any); !ok && v != nil {
		return nil
	}
	return []string{}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func toIntAny(v any) *int {
	if n, ok := toInt(v); ok {
		return &n
	}
	return nil
}

func toFloatAny(v any) *float64 {
	switch n := v.(type) {
	case float64:
		return &n
	case int:
		f := float64(n)
		return &f
	}
	return nil
}

func nullableIntPtr(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func totalCreated(m map[string]int) int {
	total := 0
	for _, n := range m {
		total += n
	}
	return total
}
