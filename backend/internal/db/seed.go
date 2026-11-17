// Seed data for development/demo environments. Applied ONLY when LV_SEED_MODE=dev
// and the database has no assets — never in production (spec §8).
package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedDevHomelab inserts a realistic homelab covering every feature surface:
// locations, physical/virtual assets (incl. k8s nodes and pods), interfaces,
// VLANs, subnets with DHCP ranges, the full IPAM status spectrum, services
// with endpoint health (incl. one down service and one without a host),
// domains/subdomains, tags, sample integrations (disabled, no secrets), and
// a few audit events so the Activity page is not empty.
//
// Deliberately NOT seeded: users/sessions (first-run SetupPage), API tokens,
// integration secrets (write-only, AES-256-GCM — cannot fabricate a valid
// ciphertext), enabled connectors (would fire live syncs on boot).
func SeedDevHomelab(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	var assets int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM assets`).Scan(&assets); err != nil {
		return err
	}
	if assets > 0 {
		logger.Info("seed skipped: inventory is not empty")
		return nil
	}

	// --- Locations ---------------------------------------------------------
	type seedLocation struct{ name, slug, description string }
	seedLocations := []seedLocation{
		{name: "Home Rack", slug: "home-rack", description: "Garage rack, 12U"},
		{name: "Upstairs", slug: "upstairs", description: "Office and living-room gear"},
	}
	locationIDs := map[string]string{}
	for _, l := range seedLocations {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO locations (name, slug, description) VALUES ($1, $2, $3)
			RETURNING id`, l.name, l.slug, l.description).Scan(&id); err != nil {
			return fmt.Errorf("seed location %s: %w", l.slug, err)
		}
		locationIDs[l.slug] = id
	}

	// --- Asset types (reference data from migrations) -----------------------
	type typeRow struct{ id, slug string }
	types := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT id, slug FROM asset_types`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t typeRow
		if err := rows.Scan(&t.id, &t.slug); err != nil {
			rows.Close()
			return err
		}
		types[t.slug] = t.id
	}
	rows.Close()

	// --- Assets --------------------------------------------------------------
	type seedAsset struct {
		name, hostname, typeSlug, status, lifecycle, environment  string
		manufacturer, model, os, osVersion, cpuModel, serial, notes string
		cores                                                     int
		ram, storageGB                                            float64
		warrantyDays                                              int // 0 = not set; >0 = expires N days from now
		purchaseDate                                              string
		purchaseCost                                              float64
		location                                                  string // location slug
		parent                                                    string // name of parent seed asset
		observed                                                  string
		extraMeta                                                 map[string]any
	}
	now := time.Now().UTC()
	seedAssets := []seedAsset{
		{name: "opnsense", hostname: "opnsense.lab", typeSlug: "firewall", status: "online", environment: "production",
			manufacturer: "Protectli", model: "VP4650", os: "OPNsense", osVersion: "25.7", cpuModel: "Intel Core i5-12500", serial: "PTL-V4650-0842",
			cores: 4, ram: 16, storageGB: 512, warrantyDays: 400, purchaseDate: "2024-02-10", purchaseCost: 689, location: "home-rack", observed: "online"},
		{name: "pve-01", hostname: "pve-01.lab", typeSlug: "server", status: "online", environment: "production",
			manufacturer: "Beelink", model: "SER9", os: "Proxmox VE", osVersion: "9.1", cpuModel: "AMD Ryzen 9 HX 370", serial: "BL-SER9-1187",
			cores: 12, ram: 96, storageGB: 2048, warrantyDays: 21, purchaseDate: "2025-01-20", purchaseCost: 1150, location: "home-rack",
			notes: "Warranty window closes soon — extend or plan replacement.", observed: "online"},
		{name: "sw-core", typeSlug: "switch", status: "online", environment: "production",
			manufacturer: "MikroTik", model: "CRS328", location: "home-rack", observed: "online"},
		{name: "nas-01", hostname: "nas.lab", typeSlug: "nas", status: "online", environment: "production",
			manufacturer: "TrueNAS", model: "Mini X+", os: "TrueNAS SCALE", osVersion: "24.10", cpuModel: "Intel N100", serial: "TN-MX-5521",
			cores: 4, ram: 32, storageGB: 38400, warrantyDays: 45, purchaseDate: "2024-06-05", purchaseCost: 940, location: "home-rack", observed: "online"},
		{name: "pi-cam", hostname: "picam.lab", typeSlug: "sbc", status: "online", environment: "production",
			manufacturer: "Raspberry Pi", model: "Pi 5", os: "Raspberry Pi OS", osVersion: "12", cpuModel: "Cortex-A76", serial: "RP5-0000f3a1",
			cores: 4, ram: 8, storageGB: 64, location: "upstairs", observed: "online"},
		{name: "vm-homeassistant", hostname: "ha.lab", typeSlug: "vm", status: "online", environment: "production",
			os: "Home Assistant OS", osVersion: "14", cores: 2, ram: 4, parent: "pve-01", observed: "online"},
		{name: "vm-opnsense-test", typeSlug: "vm", status: "offline", lifecycle: "planned", environment: "lab",
			os: "OPNsense", osVersion: "25.7", cores: 2, ram: 2, parent: "pve-01", observed: "offline",
			notes: "Sandbox for upgrade testing before touching production."},
		{name: "lxc-pihole", hostname: "pihole.lab", typeSlug: "lxc", status: "online", lifecycle: "staged", environment: "production",
			os: "Debian", osVersion: "12", cores: 1, ram: 0.5, parent: "pve-01", observed: "online"},
		{name: "lxc-jellyfin", hostname: "jellyfin.lab", typeSlug: "lxc", status: "online", environment: "production",
			os: "Debian", osVersion: "12", cores: 4, ram: 8, parent: "pve-01", observed: "online"},
		{name: "docker-host", hostname: "docker.lab", typeSlug: "lxc", status: "online", environment: "production",
			os: "Debian", osVersion: "12", cores: 4, ram: 8, parent: "pve-01", observed: "online"},
		{name: "grav", typeSlug: "docker", status: "online", environment: "production",
			os: "grav:latest", parent: "docker-host", observed: "online"},
		{name: "uptime-kuma", typeSlug: "docker", status: "online", environment: "production",
			os: "kuma:1", parent: "docker-host", observed: "online"},
		{name: "k8s-node-01", typeSlug: "k8s-node", status: "online", environment: "lab",
			manufacturer: "Beelink", model: "Mini S12", os: "Kubernetes", osVersion: "1.31", cores: 8, ram: 32,
			observed: "online", notes: "Staging cluster for workload experiments."},
		{name: "default/grafana-pod-7d9f8c", typeSlug: "pod", status: "online", environment: "lab",
			cores: 1, ram: 1, parent: "k8s-node-01", observed: "online",
			extraMeta: map[string]any{"kubernetes": map[string]any{"namespace": "default", "phase": "Running", "value": "default/grafana-pod-7d9f8c", "images": []string{"grafana/grafana:11.3.0"}}},
		},
		{name: "kube-system/coredns-5d78c9869d-x2vqk", typeSlug: "pod", status: "online", environment: "lab",
			cores: 1, ram: 0.5, parent: "k8s-node-01", observed: "online",
			extraMeta: map[string]any{"kubernetes": map[string]any{"namespace": "kube-system", "phase": "Running", "value": "kube-system/coredns-5d78c9869d-x2vqk", "images": []string{"registry.k8s.io/coredns/coredns:v1.11.3"}}},
		},
		{name: "old-ap", typeSlug: "access-point", status: "retired", lifecycle: "decommissioned", environment: "production",
			manufacturer: "Ubiquiti", model: "UniFi AP AC Lite", serial: "UBNT-AP-2231",
			warrantyDays: 0, purchaseDate: "2020-03-15", purchaseCost: 89, location: "upstairs",
			notes: "Replaced by the new AP; kept for reference.", observed: "offline"},
	}

	ids := map[string]string{}
	for _, a := range seedAssets {
		if a.lifecycle == "" {
			a.lifecycle = "active"
		}
		if a.status == "" {
			a.status = "unknown"
		}
		if a.environment == "" {
			a.environment = "production"
		}
		parentID := any(nil)
		if a.parent != "" {
			parentID = ids[a.parent]
		}
		locationID := any(nil)
		if a.location != "" {
			locationID = locationIDs[a.location]
		}
		var warranty any
		if a.warrantyDays > 0 {
			warranty = now.AddDate(0, 0, a.warrantyDays)
		}
		var purchaseDate any
		if a.purchaseDate != "" {
			purchaseDate = a.purchaseDate
		}
		var storage any
		if a.storageGB > 0 {
			storage = a.storageGB
		}
		meta := map[string]any{
			"observed": map[string]any{"status": a.observed, "checked_at": now.Format(time.RFC3339)},
		}
		for k, v := range a.extraMeta {
			meta[k] = v
		}
		metaJSON, _ := json.Marshal(meta)
		var id string
		err := pool.QueryRow(ctx, `
			INSERT INTO assets (name, hostname, type_id, status, lifecycle, environment, manufacturer, model,
			                    os_name, os_version, cpu_cores, ram_gb, storage_gb, cpu_model, serial_number,
			                    purchase_date, purchase_cost, warranty_expires, notes,
			                    location_id, parent_id, source, metadata)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,'seed',$22)
			RETURNING id`,
			a.name, a.hostname, types[a.typeSlug], a.status, a.lifecycle, a.environment, a.manufacturer, a.model,
			a.os, a.osVersion, a.cores, a.ram, storage, a.cpuModel, a.serial,
			purchaseDate, a.purchaseCost, warranty, a.notes,
			locationID, parentID, metaJSON,
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("seed asset %s: %w", a.name, err)
		}
		ids[a.name] = id
	}

	// --- Network interfaces --------------------------------------------------
	type seedIface struct {
		asset, name, kind, mac string
		mtu, speed             int
	}
	seedIfaces := []seedIface{
		{asset: "opnsense", name: "igc0", kind: "ethernet", mac: "aa:bb:cc:00:00:01", mtu: 1500, speed: 2500},
		{asset: "opnsense", name: "igc1", kind: "ethernet", mac: "aa:bb:cc:00:00:02", mtu: 1500, speed: 2500},
		{asset: "pve-01", name: "enp1s0", kind: "ethernet", mac: "bc:24:11:af:12:01", mtu: 1500, speed: 1000},
		{asset: "sw-core", name: "sw0", kind: "ethernet", mac: "cc:2d:e0:11:22:33", mtu: 1500, speed: 10000},
		{asset: "nas-01", name: "eth0", kind: "ethernet", mac: "bc:24:11:af:12:42", mtu: 1500, speed: 2500},
		{asset: "pi-cam", name: "eth0", kind: "ethernet", mac: "bc:24:11:af:12:55", mtu: 1500, speed: 1000},
		{asset: "docker-host", name: "eth0", kind: "ethernet", mac: "bc:24:11:af:12:99", mtu: 1500, speed: 1000},
		{asset: "k8s-node-01", name: "enp1s0", kind: "ethernet", mac: "bc:24:11:af:12:71", mtu: 1500, speed: 1000},
		{asset: "vm-homeassistant", name: "tap0", kind: "vlan", mac: "bc:24:11:cd:00:01", mtu: 1500},
	}
	ifaceIDs := map[string]string{} // key: "<asset>/<ifname>"
	for _, iface := range seedIfaces {
		var mtu, speed any
		if iface.mtu > 0 {
			mtu = iface.mtu
		}
		if iface.speed > 0 {
			speed = iface.speed
		}
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO network_interfaces (asset_id, name, kind, mac_address, mtu, speed_mbps)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			ids[iface.asset], iface.name, iface.kind, iface.mac, mtu, speed).Scan(&id); err != nil {
			return fmt.Errorf("seed interface %s/%s: %w", iface.asset, iface.name, err)
		}
		ifaceIDs[iface.asset+"/"+iface.name] = id
	}

	// --- VLANs ----------------------------------------------------------------
	type seedVlan struct {
		vid          int
		name, purpose string
	}
	seedVlans := []seedVlan{
		{1, "Management", "management"}, {10, "Servers", "servers"},
		{20, "Trusted clients", "clients"}, {30, "IoT", "iot"}, {40, "Guest", "guest"},
	}
	vlanIDs := map[int]string{}
	for _, v := range seedVlans {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO vlan_definitions (context_id, vlan_id, name, purpose)
			SELECT c.id, $1, $2, $3 FROM network_contexts c WHERE c.slug='default'
			RETURNING id`, v.vid, v.name, v.purpose).Scan(&id); err != nil {
			return fmt.Errorf("seed vlan %d: %w", v.vid, err)
		}
		vlanIDs[v.vid] = id
	}

	// --- Subnets + allocations -------------------------------------------------
	type seedAlloc struct {
		ip, status, asset, iface, method, desc string
	}
	type seedSubnet struct {
		cidr, name, vlanName, gateway, environment string
		dhcpStart, dhcpEnd, reservedStart, reservedEnd, location string
		allocs []seedAlloc
	}
	seedSubnets := []seedSubnet{
		{cidr: "10.10.10.0/24", name: "Management", vlanName: "Management", gateway: "10.10.10.1", environment: "production",
			location: "home-rack", allocs: []seedAlloc{
				{ip: "10.10.10.5", status: "assigned", asset: "opnsense", iface: "opnsense/igc0", desc: "LAN gateway interface"},
				{ip: "10.10.10.10", status: "assigned", asset: "pve-01", iface: "pve-01/enp1s0", desc: "Proxmox management"},
				{ip: "10.10.10.11", status: "assigned", asset: "nas-01", iface: "nas-01/eth0"},
			}},
		{cidr: "10.10.20.0/24", name: "Servers", vlanName: "Servers", gateway: "10.10.20.1", environment: "production",
			reservedStart: "10.10.20.100", reservedEnd: "10.10.20.110", allocs: []seedAlloc{
				{ip: "10.10.20.10", status: "assigned", asset: "pve-01", iface: "pve-01/enp1s0", desc: "VM bridge"},
				{ip: "10.10.20.20", status: "assigned", asset: "vm-homeassistant", desc: "Home Assistant"},
				{ip: "10.10.20.21", status: "assigned", asset: "lxc-pihole", desc: "Pi-hole DNS"},
				{ip: "10.10.20.22", status: "assigned", asset: "lxc-jellyfin"},
				{ip: "10.10.20.30", status: "assigned", asset: "docker-host", iface: "docker-host/eth0"},
				{ip: "10.10.20.100", status: "reserved", desc: "reserved for future cluster node"},
				{ip: "10.10.20.101", status: "reserved", desc: "reserved for future cluster node"},
				{ip: "10.10.20.150", status: "available", desc: "free for staging"},
				{ip: "10.10.20.2", status: "excluded", desc: "excluded: switch management interface"},
			}},
		{cidr: "10.10.30.0/24", name: "IoT", vlanName: "IoT", gateway: "10.10.30.1", environment: "production",
			dhcpStart: "10.10.30.100", dhcpEnd: "10.10.30.200", allocs: []seedAlloc{
				{ip: "10.10.30.50", status: "assigned", asset: "pi-cam", iface: "pi-cam/eth0", method: "dhcp", desc: "camera"},
				{ip: "10.10.30.77", status: "conflict", desc: "Duplicate MAC claims this address (ARP sweep)"},
			}},
		{cidr: "10.10.40.0/24", name: "Trusted clients", vlanName: "Trusted clients", gateway: "10.10.40.1", environment: "production",
			dhcpStart: "10.10.40.100", dhcpEnd: "10.10.40.200", allocs: nil},
		{cidr: "10.10.50.0/24", name: "Guest", vlanName: "Guest", gateway: "10.10.50.1", environment: "production",
			dhcpStart: "10.10.50.100", dhcpEnd: "10.10.50.200", allocs: nil},
		{cidr: "10.10.99.0/24", name: "Lab", vlanName: "", gateway: "10.10.99.1", environment: "lab",
			allocs: []seedAlloc{
				{ip: "10.10.99.5", status: "assigned", asset: "vm-opnsense-test", desc: "test OPNsense instance"},
			}},
	}

	subnetIDs := map[string]string{}
	ipIDs := map[string]string{} // key: address (unique across the seed)
	for _, s := range seedSubnets {
		vlanID := any(nil)
		if s.vlanName != "" {
			for _, v := range seedVlans {
				if v.name == s.vlanName {
					vlanID = vlanIDs[v.vid]
					break
				}
			}
			if vlanID == nil {
				return fmt.Errorf("seed subnet %s: vlan %q not defined", s.cidr, s.vlanName)
			}
		}
		locationID := any(nil)
		if s.location != "" {
			locationID = locationIDs[s.location]
		}
		optInet := func(v string) any {
			if v == "" {
				return nil
			}
			return v
		}
		var id string
		err := pool.QueryRow(ctx, `
			INSERT INTO subnets (cidr, name, description, context_id, vlan_id, gateway, dns_servers, environment,
			                     dhcp_start, dhcp_end, reserved_start, reserved_end, location_id)
			VALUES ($1,$2,$3,(SELECT id FROM network_contexts WHERE slug='default'),$4,$5,$6,$7,$8,$9,$10,$11,$12)
			RETURNING id`,
			s.cidr, s.name, "Seeded example network", vlanID, s.gateway, []string{"10.10.20.21"}, s.environment,
			optInet(s.dhcpStart), optInet(s.dhcpEnd), optInet(s.reservedStart), optInet(s.reservedEnd), locationID).Scan(&id)
		if err != nil {
			return fmt.Errorf("seed subnet %s: %w", s.cidr, err)
		}
		subnetIDs[s.cidr] = id

		for _, alloc := range s.allocs {
			assetID := any(nil)
			if alloc.asset != "" {
				if _, ok := ids[alloc.asset]; !ok {
					return fmt.Errorf("seed ip %s: unknown seed asset %q", alloc.ip, alloc.asset)
				}
				assetID = ids[alloc.asset]
			}
			ifaceID := any(nil)
			if alloc.iface != "" {
				ifaceID = ifaceIDs[alloc.iface]
			}
			var method any
			switch {
			case alloc.method != "":
				method = alloc.method
			case alloc.status == "assigned":
				method = "static"
			case alloc.status == "reserved":
				method = "reserved"
			}
			var ipID string
			if err := pool.QueryRow(ctx, `
				INSERT INTO ip_addresses (subnet_id, address, status, assignment_method, hostname, description,
				                          interface_id, last_observed_at)
				VALUES ($1, $2::inet, $3, $4, '', $5, $6, $7)
				RETURNING id`,
				id, alloc.ip, alloc.status, method, alloc.desc, ifaceID, now).Scan(&ipID); err != nil {
				return fmt.Errorf("seed ip %s: %w", alloc.ip, err)
			}
			ipIDs[alloc.ip] = ipID
			if assetID != nil {
				if _, err := pool.Exec(ctx, `
					INSERT INTO ip_assignments (ip_address_id, asset_id, method, interface_id)
					VALUES ($1, $2, $3, $4)`, ipID, assetID, method, ifaceID); err != nil {
					return fmt.Errorf("seed ip assignment %s: %w", alloc.ip, err)
				}
			}
		}
	}

	// --- Services ----------------------------------------------------------------
	type seedEndpoint struct {
		label, protocol, url string
		port                 int
		isExternal           bool
		lastStatus           int // 0 = never checked
		healthEnabled        bool
	}
	type seedService struct {
		name, slug, category, deployType, status, environment, source string
		repoURL, docsURL, notes string
		endpoints []seedEndpoint
		hosts     []string // asset names; empty = deliberately host-less
	}
	seedServices := []seedService{
		{name: "Pi-hole", slug: "pi-hole", category: "network", deployType: "lxc", status: "online", environment: "production",
			notes: "Primary DNS for the whole lab; watch for FTL memory growth.",
			endpoints: []seedEndpoint{{label: "Web UI", protocol: "http", port: 80, url: "http://pihole.lab/admin", lastStatus: 200}},
			hosts:     []string{"lxc-pihole"}},
		{name: "Home Assistant", slug: "home-assistant", category: "home-automation", deployType: "vm", status: "online", environment: "production",
			endpoints: []seedEndpoint{{label: "Web UI", protocol: "http", port: 8123, url: "http://ha.lab:8123", lastStatus: 200}},
			hosts:     []string{"vm-homeassistant"}},
		{name: "Jellyfin", slug: "jellyfin", category: "media", deployType: "lxc", status: "online", environment: "production",
			repoURL: "https://github.com/jellyfin/jellyfin", docsURL: "https://jellyfin.org/docs",
			endpoints: []seedEndpoint{{label: "Web UI", protocol: "http", port: 8096, url: "http://jellyfin.lab:8096", lastStatus: 200}},
			hosts:     []string{"lxc-jellyfin"}},
		{name: "Grav CMS", slug: "grav-cms", category: "productivity", deployType: "docker", status: "online", environment: "production",
			endpoints: []seedEndpoint{
				{label: "Web UI", protocol: "http", port: 8080, url: "http://docker.lab:8080", lastStatus: 200},
				{label: "Public site", protocol: "https", url: "https://grav.example.com", isExternal: true, lastStatus: 200},
			},
			hosts: []string{"docker-host"}},
		{name: "Uptime Kuma", slug: "uptime-kuma", category: "monitoring", deployType: "docker", status: "online", environment: "production",
			repoURL: "https://github.com/louislam/uptime-kuma",
			endpoints: []seedEndpoint{{label: "Web UI", protocol: "http", port: 3001, url: "http://docker.lab:3001", lastStatus: 200}},
			hosts:     []string{"docker-host"}},
		{name: "Grafana", slug: "grafana", category: "monitoring", deployType: "docker", status: "offline", environment: "production",
			notes: "Container crashed after a bad datasource migration — fix pending.",
			endpoints: []seedEndpoint{{label: "Web UI", protocol: "http", port: 3000, url: "http://docker.lab:3000", lastStatus: 502, healthEnabled: true}},
			hosts:     []string{"docker-host"}},
		{name: "Vaultwarden", slug: "vaultwarden", category: "security", deployType: "docker", status: "online", environment: "production",
			notes: "Not attached to a tracked host yet — register its container or LXC.",
			endpoints: []seedEndpoint{{label: "Web UI", protocol: "https", port: 8222, url: "https://vault.lab", lastStatus: 200}},
			hosts:     nil},
		{name: "Argo CD", slug: "argo-cd", category: "development", deployType: "kubernetes", status: "online", environment: "lab", source: "kubernetes",
			repoURL: "https://github.com/argoproj/argo-cd",
			endpoints: []seedEndpoint{{label: "Web UI", protocol: "https", port: 443, url: "https://argo.k8s.lab", lastStatus: 200}},
			hosts:     []string{"k8s-node-01"}},
	}
	svcIDs := map[string]string{}
	for _, svc := range seedServices {
		var svcID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO services (name, slug, description, category, deployment_type, environment, status,
			                      repo_url, docs_url, notes, source)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id`,
			svc.name, svc.slug, svc.name+" — seeded demo service", svc.category, svc.deployType, svc.environment,
			svc.status, svc.repoURL, svc.docsURL, svc.notes, svc.source).Scan(&svcID); err != nil {
			return fmt.Errorf("seed service %s: %w", svc.name, err)
		}
		svcIDs[svc.name] = svcID
		for _, ep := range svc.endpoints {
			var lastStatus, latency any
			var port any
			if ep.port > 0 {
				port = ep.port
			}
			if ep.lastStatus > 0 {
				lastStatus = ep.lastStatus
				latency = 4 + ep.port%30 // plausible 4–33 ms spread
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO service_endpoints (service_id, label, protocol, port, url, is_external,
				                               health_enabled, last_checked_at, last_status, last_latency_ms)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
				svcID, ep.label, ep.protocol, port, ep.url, ep.isExternal,
				ep.healthEnabled, now.Add(-2*time.Minute), lastStatus, latency); err != nil {
				return fmt.Errorf("seed endpoint %s/%s: %w", svc.name, ep.label, err)
			}
		}
		for _, host := range svc.hosts {
			if _, err := pool.Exec(ctx, `
				INSERT INTO service_deployments (service_id, asset_id, deployment_type)
				VALUES ($1, $2, $3)`, svcID, ids[host], svc.deployType); err != nil {
				return fmt.Errorf("seed deployment %s/%s: %w", svc.name, host, err)
			}
		}
	}

	// --- Domains + records ---------------------------------------------------------
	var domainID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO domains (name, registrar, expires_on, notes)
		VALUES ('lab.example.com', 'Cloudflare', $1, 'Internal homelab zone')
		RETURNING id`, now.AddDate(0, 0, 200)).Scan(&domainID); err != nil {
		return fmt.Errorf("seed domain: %w", err)
	}
	seedRecords := []struct {
		label, recordType, value string
		ttl                      int
		ip, service              string
	}{
		{"", "A", "10.10.20.21", 300, "10.10.20.21", "Pi-hole"},
		{"pihole", "CNAME", "pihole.lab", 300, "", "Pi-hole"},
		{"jellyfin", "A", "10.10.20.22", 300, "10.10.20.22", "Jellyfin"},
		{"argo", "CNAME", "argo.k8s.lab", 300, "", "Argo CD"},
	}
	for _, r := range seedRecords {
		ipID := any(nil)
		if r.ip != "" {
			ipID = ipIDs[r.ip]
		}
		svcID := any(nil)
		if r.service != "" {
			svcID = svcIDs[r.service]
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO subdomains (domain_id, label, record_type, value, ttl, ip_address_id, service_id,
			                        observed_value, observed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $4, $8)`,
			domainID, r.label, r.recordType, r.value, r.ttl, ipID, svcID, now); err != nil {
			return fmt.Errorf("seed subdomain %s/%s: %w", r.recordType, r.label, err)
		}
	}

	// --- Tags -------------------------------------------------------------------------
	tagIDs := map[string]string{}
	for _, t := range []struct{ name, color string }{
		{"critical", "red"}, {"prod", "green"}, {"iot", "cyan"},
	} {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO tags (name, color) VALUES ($1, $2) RETURNING id`, t.name, t.color).Scan(&id); err != nil {
			return fmt.Errorf("seed tag %s: %w", t.name, err)
		}
		tagIDs[t.name] = id
	}
	for _, et := range []struct{ entityType, entity, tag string }{
		{"asset", "pve-01", "critical"}, {"asset", "nas-01", "critical"},
		{"asset", "opnsense", "prod"}, {"asset", "pi-cam", "iot"},
		{"service", "Uptime Kuma", "critical"}, {"service", "Jellyfin", "prod"},
	} {
		entityID := ids[et.entity]
		if et.entityType == "service" {
			entityID = svcIDs[et.entity]
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO entity_tags (entity_type, entity_id, tag_id) VALUES ($1, $2, $3)`,
			et.entityType, entityID, tagIDs[et.tag]); err != nil {
			return fmt.Errorf("seed entity tag %s/%s: %w", et.entityType, et.entity, err)
		}
	}

	// --- Integrations (all disabled; config only, never secrets) -----------------------
	type seedIntegration struct {
		kind, name, description string
		config                  map[string]any
	}
	seedIntegrations := []seedIntegration{
		{kind: "proxmox", name: "pve-01", description: "Demo placeholder — fill in the API token secret before enabling.",
			config: map[string]any{"endpoint": "https://pve-01.lab:8006", "token_id": "lanvory@pve!read", "verify_tls": false}},
		{kind: "docker", name: "docker-host", description: "Demo placeholder — pointing at the seeded docker host.",
			config: map[string]any{"host": "unix:///var/run/docker.sock", "asset_id": ids["docker-host"]}},
		{kind: "icmp", name: "core reachability", description: "Demo placeholder — ICMP reachability for core devices.",
			config: map[string]any{"asset_ids": []string{ids["opnsense"], ids["pve-01"], ids["nas-01"]}, "timeout_seconds": 2}},
		{kind: "dns", name: "internal zone", description: "Demo placeholder — AXFR against Pi-hole.",
			config: map[string]any{"domain_id": domainID, "mode": "axfr", "server": "10.10.20.21:53"}},
		{kind: "cloudflare", name: "cloudflare zones", description: "Demo placeholder — add the API token secret before enabling.",
			config: map[string]any{"zone_names": []string{"example.com"}}},
		{kind: "kubernetes", name: "k8s-node-01", description: "Demo placeholder — staging cluster.",
			config: map[string]any{"in_cluster": false, "verify_tls": false}},
		{kind: "adguard", name: "adguard-home", description: "Demo placeholder — add the password secret before enabling.",
			config: map[string]any{"endpoint": "http://10.10.20.21:80", "username": "admin", "verify_tls": true}},
		{kind: "arp", name: "lan sweep", description: "Demo placeholder — periodic ARP scan of the IoT subnet.",
			config: map[string]any{"mode": "scan", "interface": "enp1s0", "cidr": "10.10.30.0/24", "timeout_seconds": 5}},
	}
	for _, in := range seedIntegrations {
		configJSON, _ := json.Marshal(in.config)
		if _, err := pool.Exec(ctx, `
			INSERT INTO integrations (kind, name, description, enabled, config, sync_interval_seconds)
			VALUES ($1, $2, $3, false, $4, NULL)`,
			in.kind, in.name, in.description, configJSON); err != nil {
			return fmt.Errorf("seed integration %s/%s: %w", in.kind, in.name, err)
		}
	}

	// --- A few audit events so the Activity page has content ----------------------------
	seedAudit := []struct {
		entityType, entity, label, action, changes string
	}{
		{"asset", ids["pve-01"], "pve-01", "asset.created", `{"status":{"from":"unknown","to":"online"}}`},
		{"asset", ids["nas-01"], "nas-01", "asset.updated", `{"warranty_expires":{"from":null,"to":"2027-01-01"}}`},
		{"service", svcIDs["Grafana"], "Grafana", "service.updated", `{"status":{"from":"online","to":"offline"}}`},
		{"integration", "", "pve-01", "integration.created", `{}`},
	}
	for _, ev := range seedAudit {
		entityID := any(nil)
		if ev.entity != "" {
			entityID = ev.entity
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO audit_events (actor_user_id, actor_label, entity_type, entity_id, entity_label, action, changes)
			VALUES (NULL, 'seed', $1, $2, $3, $4, $5::jsonb)`,
			ev.entityType, entityID, ev.label, ev.action, ev.changes); err != nil {
			return fmt.Errorf("seed audit event %s: %w", ev.action, err)
		}
	}

	logger.Info("dev seed applied",
		"assets", len(seedAssets), "subnets", len(seedSubnets), "services", len(seedServices),
		"domains", 1, "integrations", len(seedIntegrations))
	return nil
}
