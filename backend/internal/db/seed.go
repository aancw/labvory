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

// SeedDevHomelab inserts a small but realistic homelab: router, Proxmox host,
// VMs, LXC containers, Docker services, subnets, VLANs, and allocations.
func SeedDevHomelab(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	var assets int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM assets`).Scan(&assets); err != nil {
		return err
	}
	if assets > 0 {
		logger.Info("seed skipped: inventory is not empty")
		return nil
	}

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

	type seedAsset struct {
		name, hostname, typeSlug, status, environment, manufacturer, model, os, osVersion string
		cores                                                                             int
		ram                                                                               float64
		parent                                                                            string // name of parent seed asset
		observed                                                                          string
	}
	seedAssets := []seedAsset{
		{name: "opnsense", hostname: "opnsense.lab", typeSlug: "firewall", status: "online", environment: "production", manufacturer: "Protectli", model: "VP4650", os: "OPNsense", osVersion: "25.7", cores: 4, ram: 16, observed: "online"},
		{name: "pve-01", hostname: "pve-01.lab", typeSlug: "server", status: "online", environment: "production", manufacturer: "Beelink", model: "SER9", os: "Proxmox VE", osVersion: "9.1", cores: 12, ram: 96, observed: "online"},
		{name: "sw-core", hostname: "", typeSlug: "switch", status: "online", environment: "production", manufacturer: "MikroTik", model: "CRS328", observed: "online"},
		{name: "nas-01", hostname: "nas.lab", typeSlug: "nas", status: "online", environment: "production", manufacturer: "TrueNAS", model: "Mini X+", os: "TrueNAS SCALE", osVersion: "24.10", cores: 4, ram: 32, observed: "online"},
		{name: "pi-cam", hostname: "picam.lab", typeSlug: "sbc", status: "online", environment: "production", manufacturer: "Raspberry Pi", model: "Pi 5", os: "Raspberry Pi OS", osVersion: "12", cores: 4, ram: 8, observed: "online"},
		{name: "vm-homeassistant", hostname: "ha.lab", typeSlug: "vm", status: "online", environment: "production", os: "Home Assistant OS", osVersion: "14", cores: 2, ram: 4, parent: "pve-01", observed: "online"},
		{name: "vm-opnsense-test", hostname: "", typeSlug: "vm", status: "offline", environment: "lab", os: "OPNsense", osVersion: "25.7", cores: 2, ram: 2, parent: "pve-01", observed: "offline"},
		{name: "lxc-pihole", hostname: "pihole.lab", typeSlug: "lxc", status: "online", environment: "production", os: "Debian", osVersion: "12", cores: 1, ram: 0.5, parent: "pve-01", observed: "online"},
		{name: "lxc-jellyfin", hostname: "jellyfin.lab", typeSlug: "lxc", status: "online", environment: "production", os: "Debian", osVersion: "12", cores: 4, ram: 8, parent: "pve-01", observed: "online"},
		{name: "docker-host", hostname: "docker.lab", typeSlug: "lxc", status: "online", environment: "production", os: "Debian", osVersion: "12", cores: 4, ram: 8, parent: "pve-01", observed: "online"},
		{name: "grav", hostname: "", typeSlug: "docker", status: "online", environment: "production", os: "grav:latest", parent: "docker-host", observed: "online"},
		{name: "uptime-kuma", hostname: "", typeSlug: "docker", status: "online", environment: "production", os: "kuma:1", parent: "docker-host", observed: "online"},
	}

	ids := map[string]string{}
	for _, a := range seedAssets {
		parentID := any(nil)
		if a.parent != "" {
			parentID = ids[a.parent]
		}
		var id string
		meta, _ := json.Marshal(map[string]any{
			"observed": map[string]any{"status": a.observed, "checked_at": time.Now().UTC().Format(time.RFC3339)},
		})
		err := pool.QueryRow(ctx, `
			INSERT INTO assets (name, hostname, type_id, status, environment, manufacturer, model,
			                    os_name, os_version, cpu_cores, ram_gb, parent_id, source, metadata)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'seed',$13)
			RETURNING id`,
			a.name, a.hostname, types[a.typeSlug], a.status, a.environment, a.manufacturer, a.model,
			a.os, a.osVersion, a.cores, a.ram, parentID, meta,
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("seed asset %s: %w", a.name, err)
		}
		ids[a.name] = id
	}

	// Interfaces for the host + router.
	for _, iface := range []struct{ asset, name, mac string }{
		{"opnsense", "igc0", "aa:bb:cc:00:00:01"},
		{"opnsense", "igc1", "aa:bb:cc:00:00:02"},
		{"pve-01", "enp1s0", "bc:24:11:af:12:01"},
		{"docker-host", "eth0", "bc:24:11:af:12:99"},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO network_interfaces (asset_id, name, kind, mac_address)
			VALUES ($1, $2, 'ethernet', $3)`, ids[iface.asset], iface.name, iface.mac); err != nil {
			return fmt.Errorf("seed interface %s/%s: %w", iface.asset, iface.name, err)
		}
	}

	// VLANs.
	vlans := []struct {
		vid           int
		name, purpose string
	}{
		{1, "Management", "management"}, {10, "Servers", "servers"},
		{20, "Trusted clients", "clients"}, {30, "IoT", "iot"}, {40, "Guest", "guest"},
	}
	vlanIDs := map[int]string{}
	for _, v := range vlans {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO vlan_definitions (context_id, vlan_id, name, purpose)
			SELECT c.id, $1, $2, $3 FROM network_contexts c WHERE c.slug='default'
			RETURNING id`, v.vid, v.name, v.purpose).Scan(&id); err != nil {
			return fmt.Errorf("seed vlan %d: %w", v.vid, err)
		}
		vlanIDs[v.vid] = id
	}

	// Subnets + allocations.
	seedSubnets := []struct {
		cidr, name, vlanName, gateway string
		allocs                        []struct{ ip, status, hostname, desc string }
	}{
		{cidr: "10.10.10.0/24", name: "Management", vlanName: "Management", gateway: "10.10.10.1", allocs: []struct{ ip, status, hostname, desc string }{
			{"10.10.10.5", "assigned", "opnsense", "LAN gateway interface"},
			{"10.10.10.10", "assigned", "pve-01", "Proxmox management"},
			{"10.10.10.11", "assigned", "nas-01", ""},
		}},
		{cidr: "10.10.20.0/24", name: "Servers", vlanName: "Servers", gateway: "10.10.20.1", allocs: []struct{ ip, status, hostname, desc string }{
			{"10.10.20.10", "assigned", "pve-01", "VM bridge"},
			{"10.10.20.20", "assigned", "ha.lab", "Home Assistant"},
			{"10.10.20.21", "assigned", "pihole.lab", "Pi-hole DNS"},
			{"10.10.20.22", "assigned", "jellyfin.lab", ""},
			{"10.10.20.30", "assigned", "docker.lab", ""},
			{"10.10.20.100", "reserved", "", "reserved for future cluster node"},
			{"10.10.20.101", "reserved", "", "reserved for future cluster node"},
		}},
		{cidr: "10.10.30.0/24", name: "IoT", vlanName: "IoT", gateway: "10.10.30.1", allocs: []struct{ ip, status, hostname, desc string }{
			{"10.10.30.50", "assigned", "picam.lab", "camera"},
		}},
	}

	subnetIDs := map[string]string{}
	for _, s := range seedSubnets {
		var id string
		vlanID := vlanIDs[mapName(s.vlanName)]
		err := pool.QueryRow(ctx, `
			INSERT INTO subnets (cidr, name, description, context_id, vlan_id, gateway, dns_servers, environment)
			VALUES ($1,$2,$3,(SELECT id FROM network_contexts WHERE slug='default'),$4,$5,$6,'production')
			RETURNING id`,
			s.cidr, s.name, "Seeded example network", vlanID, s.gateway, []string{"10.10.20.21"}).Scan(&id)
		if err != nil {
			return fmt.Errorf("seed subnet %s: %w", s.cidr, err)
		}
		subnetIDs[s.cidr] = id

		for _, alloc := range s.allocs {
			assetID := any(nil)
			if ids[alloc.hostname] != "" {
				assetID = ids[alloc.hostname]
			} else if ids[alloc.desc] != "" {
				assetID = ids[alloc.desc]
			}
			status := alloc.status
			method := "static"
			if status == "reserved" {
				method = "reserved"
			}
			var ipID string
			if err := pool.QueryRow(ctx, `
				INSERT INTO ip_addresses (subnet_id, address, status, assignment_method, hostname, description)
				VALUES ($1, $2::inet, $3, $4, $5, $6)
				RETURNING id`,
				id, alloc.ip, status, method, alloc.hostname, alloc.desc).Scan(&ipID); err != nil {
				return fmt.Errorf("seed ip %s: %w", alloc.ip, err)
			}
			if status == "assigned" {
				if _, err := pool.Exec(ctx, `
					INSERT INTO ip_assignments (ip_address_id, asset_id, method)
					VALUES ($1, $2, 'static')`, ipID, assetID); err != nil {
					return fmt.Errorf("seed ip assignment %s: %w", alloc.ip, err)
				}
			}
		}
	}

	// Services.
	seedServices := []struct {
		name, category, deployType string
		url                        string
		hosts                      []string
	}{
		{name: "Pi-hole", category: "network", deployType: "lxc", url: "http://pihole.lab/admin", hosts: []string{"lxc-pihole"}},
		{name: "Home Assistant", category: "home-automation", deployType: "vm", url: "http://ha.lab:8123", hosts: []string{"vm-homeassistant"}},
		{name: "Jellyfin", category: "media", deployType: "lxc", url: "http://jellyfin.lab:8096", hosts: []string{"lxc-jellyfin"}},
		{name: "Grav CMS", category: "productivity", deployType: "docker", url: "http://docker.lab:8080", hosts: []string{"docker-host"}},
		{name: "Uptime Kuma", category: "monitoring", deployType: "docker", url: "http://docker.lab:3001", hosts: []string{"docker-host"}},
	}
	for _, svc := range seedServices {
		var svcID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO services (name, slug, description, category, deployment_type, environment, status)
			VALUES ($1, $2, $3, $4, $5, 'production', 'online')
			RETURNING id`,
			svc.name, slugifySeed(svc.name), svc.name+" — seeded demo service", svc.category, svc.deployType).Scan(&svcID); err != nil {
			return fmt.Errorf("seed service %s: %w", svc.name, err)
		}
		if svc.url != "" {
			if _, err := pool.Exec(ctx, `
				INSERT INTO service_endpoints (service_id, label, protocol, port, url)
				VALUES ($1, 'Web UI', 'http', NULL, $2)`, svcID, svc.url); err != nil {
				return fmt.Errorf("seed endpoint %s: %w", svc.name, err)
			}
		}
		for _, host := range svc.hosts {
			if _, err := pool.Exec(ctx, `
				INSERT INTO service_deployments (service_id, asset_id, deployment_type)
				VALUES ($1, $2, $3)`, svcID, ids[host], svc.deployType); err != nil {
				return fmt.Errorf("seed deployment %s: %w", svc.name, err)
			}
		}
	}

	logger.Info("dev seed applied", "assets", len(seedAssets), "subnets", len(seedSubnets), "services", len(seedServices))
	return nil
}

func mapName(vlan string) int {
	switch vlan {
	case "Management":
		return 1
	case "Servers":
		return 10
	case "IoT":
		return 30
	}
	return 1
}

func slugifySeed(name string) string {
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
	if out == "" {
		out = "service"
	}
	return out
}
