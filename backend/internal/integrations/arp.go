package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/lanvory/internal/store"
)

// SyncARP reads Layer-2 neighbors (ip ↔ mac) and records them as observations.
//
// Two modes:
//
//	scan  — active in-process ARP sweep (gopacket, like the arpscan example):
//	        crafts requests on the interface and collects replies. Needs raw
//	        packet access (NET_RAW on Linux, BPF on macOS), but no external
//	        binary — notably it works where the OS denies the neighbor table
//	        to spawned helper binaries.
//	table — parse the local ARP cache (/proc/net/arp on Linux, `arp -an`
//	        elsewhere). Zero privileges, but only shows neighbors this host
//	        has talked to, and on macOS the OS may hand spawned helpers an
//	        empty cache.
//
// Every entry IP is marked as observed on a matching IP allocation
// (last_observed_at). When the MAC belongs to an asset interface, the asset gets
// a metadata.observed.arp entry with the last seen IP. Nothing new is created:
// unknown devices appear in the sync summary only, declared state is untouched.
//
// Config: {"mode": "scan"|"table", "interface": "eth0", "cidr": "192.168.1.0/24"}
// (interface/cidr apply to scan mode only; empty interface sweeps every
// suitable interface on its own subnet).
func SyncARP(ctx context.Context, pool *pgxpool.Pool, integ store.Integration) (map[string]any, error) {
	var cfg struct {
		Mode      string `json:"mode"`
		Interface string `json:"interface"`
		CIDR      string `json:"cidr"`
		TimeoutS  int    `json:"timeout_seconds"`
	}
	if len(integ.Config) > 0 {
		if err := json.Unmarshal(integ.Config, &cfg); err != nil {
			return nil, fmt.Errorf("invalid config: %w", err)
		}
	}
	if cfg.Mode == "" {
		cfg.Mode = "scan"
	}
	if cfg.TimeoutS <= 0 {
		cfg.TimeoutS = 5
	}
	if cfg.TimeoutS > 60 {
		cfg.TimeoutS = 60
	}

	entries, sweepTargets, err := collectArpEntries(ctx, cfg.Mode, cfg.Interface, cfg.CIDR, time.Duration(cfg.TimeoutS)*time.Second)
	if err != nil {
		return nil, err
	}

	// Index interface MACs → owning asset (first interface wins on duplicates).
	ifaces := store.NewInterfaces(pool)
	index, err := ifaces.MACIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("load interface MACs: %w", err)
	}
	macToAsset := map[string]string{}
	duplicateMACs := 0
	for _, e := range index {
		norm := normalizeMAC(e.MAC)
		if norm == "" {
			continue
		}
		if _, seen := macToAsset[norm]; seen {
			duplicateMACs++
			continue
		}
		macToAsset[norm] = e.AssetID
	}

	now := time.Now().UTC()
	ips := store.NewIPAddresses(pool)
	integrations := store.NewIntegrations(pool)

	seenIPs := map[string]bool{}
	skipped, macsMatched, macsUnknown := 0, 0, 0
	assetSightings := map[string]map[string]any{} // assetID → last arp observation
	for _, e := range entries {
		if !e.IP.IsValid() || e.MAC == "" || !isUnicastMAC(e.MAC) {
			skipped++ // unparseable, or broadcast/multicast link-layer entries
			continue
		}
		if !seenIPs[e.IP.String()] {
			if err := ips.MarkObserved(ctx, e.IP.String(), now); err != nil {
				return nil, fmt.Errorf("observe %s: %w", e.IP, err)
			}
			seenIPs[e.IP.String()] = true
		}
		assetID, ok := macToAsset[e.MAC]
		if !ok {
			macsUnknown++
			continue
		}
		macsMatched++
		// Last sighting per asset wins; distinct interfaces just overwrite.
		assetSightings[assetID] = map[string]any{
			"ip": e.IP.String(), "mac": e.MAC,
			"checked_at": now.Format(time.RFC3339),
		}
	}
	assetsSeen := 0
	for assetID, obs := range assetSightings {
		if err := integrations.SetObservedSub(ctx, assetID, "arp", obs); err != nil {
			return nil, fmt.Errorf("record arp observation on asset %s: %w", assetID, err)
		}
		assetsSeen++
	}

	// Promotion: every discovered device (one asset per MAC) becomes part of
	// the inventory automatically — no prerequisite. Each device also gets its
	// LAN interface (carrying the discovered MAC) so allocations attach to a
	// NIC and the asset's primary_ip resolves. The name follows reverse DNS
	// when the network can answer it, falling back to the MAC.
	byMAC := arpGroupByMac(entries)
	hostnames := resolvePtrNames(ctx, byMAC)
	assetIDs := map[string]string{}
	ifaceIDs := map[string]string{}
	assetsCreated := 0
	for mac, ip := range byMAC {
		host := hostnames[ip]
		name := mac
		if host != "" {
			name = host
		}
		id, wasCreated, err := integrations.UpsertDiscoveredAsset(ctx, store.DiscoveredAsset{
			Name:          name,
			Hostname:      host,
			TypeSlug:      "discovered",
			Source:        "arp",
			ExternalKey:   "mac",
			ExternalValue: mac,
			Metadata: map[string]any{
				"arp":      map[string]any{"ip": ip, "checked_at": now.Format(time.RFC3339)},
				"observed": map[string]any{"status": "online", "checked_at": now.Format(time.RFC3339)},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("upsert discovered device %s: %w", mac, err)
		}
		assetIDs[mac] = id
		ifaceID, ierr := ensureDiscoveredInterface(ctx, ifaces, id, mac)
		if ierr != nil {
			return nil, ierr
		}
		ifaceIDs[mac] = ifaceID
		if wasCreated {
			assetsCreated++
		}
	}

	// IP registration: each discovered IP lands in IPAM. Declared subnets are
	// the preferred home; a swept network without one gets an auto-created
	// subnet (clearly marked) so the registration is fully automatic.
	subnetRows, err := store.NewSubnets(pool).List(ctx, "", "", "")
	if err != nil {
		return nil, fmt.Errorf("load subnets: %w", err)
	}
	type declaredSubnet struct {
		id     string
		prefix netip.Prefix
	}
	declared := make([]declaredSubnet, 0, len(subnetRows))
	for _, sn := range subnetRows {
		if p, perr := netip.ParsePrefix(sn.CIDR); perr == nil {
			declared = append(declared, declaredSubnet{id: sn.ID, prefix: p})
		}
	}
	var contextID string
	if contexts, cerr := store.NewNetworkContexts(pool).List(ctx); cerr == nil && len(contexts) > 0 {
		contextID = contexts[0].ID
		for _, c := range contexts {
			if c.Slug == "default" {
				contextID = c.ID
				break
			}
		}
	}

	subnetsCreated := 0
	autoSubnets := map[string]string{} // swept prefix → subnet id ("" = unusable)
	ipsRegistered, allocationConflicts := 0, 0
	for mac, ipStr := range byMAC {
		ip, perr := netip.ParseAddr(ipStr)
		if perr != nil {
			continue
		}
		subnetID := ""
		for _, d := range declared {
			if d.prefix.Contains(ip) {
				subnetID = d.id
				break
			}
		}
		if subnetID == "" {
			for _, t := range sweepTargets {
				if !t.prefix.Contains(ip) {
					continue
				}
				id, known := autoSubnets[t.prefix.String()]
				if !known {
					sn := store.Subnet{
						CIDR:        t.prefix.String(),
						Name:        t.prefix.String(),
						Description: "Auto-created by the ARP scan connector",
						ContextID:   contextID,
						Environment: "production",
					}
					if cerr := store.NewSubnets(pool).Create(ctx, &sn); cerr != nil {
						id = "" // overlap/invalid: leave those IPs unregistered
					} else {
						id = sn.ID
						subnetsCreated++
					}
					autoSubnets[t.prefix.String()] = id
				}
				subnetID = id
				break
			}
		}
		if subnetID == "" {
			continue
		}
		// Already tracked (this sync or a previous one) → leave it alone;
		// operator-owned rows are never overwritten.
		if _, err := ips.GetByAddress(ctx, subnetID, ipStr); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("lookup %s: %w", ipStr, err)
		}
		ifaceID := ifaceIDs[mac]
		err := ips.Allocate(ctx, &store.IPAddress{
			SubnetID:    subnetID,
			Address:     ipStr,
			Status:      "assigned",
			Description: "Discovered via ARP scan",
			InterfaceID: &ifaceID,
		}, assetIDs[mac], "")
		if errors.Is(err, store.ErrConflict) {
			allocationConflicts++ // operator-owned row: never overwrite
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("register ip %s: %w", ipStr, err)
		}
		ipsRegistered++
	}

	summary := map[string]any{
		"mode":                 cfg.Mode,
		"entries":              len(entries),
		"ips_seen":             len(seenIPs),
		"macs_matched":         macsMatched,
		"macs_unknown":         macsUnknown,
		"assets_seen":          assetsSeen,
		"macs_promoted":        len(byMAC),
		"assets_created":       assetsCreated,
		"subnets_created":      subnetsCreated,
		"ips_registered":       ipsRegistered,
		"allocation_conflicts": allocationConflicts,
		"skipped":              skipped,
		"macs_duplicate":       duplicateMACs,
	}
	switch {
	case cfg.Mode == "table" && len(entries) == 0:
		// A bridge-mode container reads its own netns, which never sees the
		// host's (or the LAN's) neighbor cache; on macOS the OS may hand a
		// spawned helper an empty cache outright.
		summary["note"] = "the server's own ARP cache was empty — a container cannot see the host's cache and macOS may deny spawned helpers; prefer scan mode or run the backend natively on the LAN-attached host, see README"
	case cfg.Mode == "scan" && len(entries) == 0:
		summary["note"] = "no devices responded — check the interface/CIDR config, raw packet privileges (NET_RAW on Linux; BPF via root on modern macOS), and that the server sits on the scanned network (docker: macvlan override)"
	}
	return summary, nil
}

// ensureDiscoveredInterface finds or creates the LAN interface carrying the
// discovered MAC on the asset, so IP allocations attach to a NIC and the
// asset's primary_ip resolves.
func ensureDiscoveredInterface(ctx context.Context, ifaces *store.Interfaces, assetID, mac string) (string, error) {
	list, err := ifaces.ListByAsset(ctx, assetID)
	if err != nil {
		return "", fmt.Errorf("list interfaces for discovered device %s: %w", assetID, err)
	}
	for _, ni := range list {
		if ni.MACAddress != nil && *ni.MACAddress == mac {
			return ni.ID, nil
		}
	}
	ni := &store.NetworkInterface{
		AssetID:    assetID,
		Name:       "lan",
		Kind:       "ethernet",
		MACAddress: &mac,
		Notes:      "Discovered via ARP scan",
	}
	if err := ifaces.Create(ctx, ni); err != nil {
		return "", fmt.Errorf("create interface for discovered device %s: %w", assetID, err)
	}
	return ni.ID, nil
}

// ptrBudget bounds how long one sync spends on reverse DNS; unresolved
// devices simply keep their MAC as the name.
const ptrBudget = 8 * time.Second

// resolvePtrNames does best-effort reverse DNS for the discovered IPs, bounded
// by ptrBudget and a small concurrency pool. Missing answers map to "".
func resolvePtrNames(ctx context.Context, byMAC map[string]string) map[string]string {
	out := map[string]string{}
	if len(byMAC) == 0 {
		return out
	}
	budgetCtx, cancel := context.WithTimeout(ctx, ptrBudget)
	defer cancel()

	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, ip := range byMAC {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-budgetCtx.Done():
				return
			}
			defer func() { <-sem }()
			names, err := net.DefaultResolver.LookupAddr(budgetCtx, ip)
			if err != nil || len(names) == 0 {
				return
			}
			host := strings.TrimSuffix(strings.TrimSpace(names[0]), ".")
			if host == "" {
				return
			}
			mu.Lock()
			out[ip] = host
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
	return out
}

// arpEntry is one L2 neighbor sighting.
type arpEntry struct {
	IP  netip.Addr
	MAC string // normalized (lowercase, colon-separated)
}

// isUnicastMAC reports whether the normalized MAC is a unicast hardware
// address. Broadcast (ff:ff:ff:ff:ff:ff) and multicast (low bit of the first
// octet set — 01:00:5e:…, 33:33:…) entries are link-layer groups, not devices.
func isUnicastMAC(mac string) bool {
	first, err := strconv.ParseUint(mac[0:2], 16, 8)
	return err == nil && first&0x01 == 0
}

// collectArpEntries gathers neighbors from the chosen source; scan mode also
// returns the sweep targets (each carries the swept prefix for auto-subnets).
func collectArpEntries(ctx context.Context, mode, iface, cidr string, timeout time.Duration) ([]arpEntry, []sweepTarget, error) {
	switch mode {
	case "table":
		if raw, err := os.ReadFile("/proc/net/arp"); err == nil {
			return parseProcArp(string(raw)), nil, nil
		}
		arpBin, lookErr := exec.LookPath("arp")
		if lookErr != nil {
			// macOS keeps arp in /usr/sbin, which stripped-down process PATHs omit.
			for _, p := range []string{"/usr/sbin/arp", "/sbin/arp", "/usr/bin/arp"} {
				if _, statErr := os.Stat(p); statErr == nil {
					arpBin = p
					lookErr = nil
					break
				}
			}
		}
		if lookErr != nil {
			return nil, nil, fmt.Errorf("read ARP table: no /proc/net/arp and no arp binary on this host")
		}
		out, err := exec.CommandContext(ctx, arpBin, "-an").Output()
		if err != nil {
			return nil, nil, fmt.Errorf("read ARP table via %s: %w", arpBin, err)
		}
		return parseArpAnOutput(string(out)), nil, nil
	case "scan":
		targets, err := arpSweepTargets(strings.TrimSpace(iface), strings.TrimSpace(cidr), maxAutoSweepHosts)
		if err != nil {
			return nil, nil, err
		}
		seen := map[string]bool{}
		entries := []arpEntry{}
		for _, t := range targets {
			results, err := arpSweepInterface(ctx, t.iface, t.ip, t.hosts, timeout)
			if err != nil {
				return nil, nil, err
			}
			for ipStr, mac := range results {
				if seen[ipStr] {
					continue
				}
				seen[ipStr] = true
				addr, perr := netip.ParseAddr(ipStr)
				if perr != nil {
					continue
				}
				entries = append(entries, arpEntry{IP: addr, MAC: mac})
			}
		}
		return entries, targets, nil
	default:
		return nil, nil, fmt.Errorf("unsupported arp mode %q (use scan or table)", mode)
	}
}

// parseProcArp parses /proc/net/arp rows; incomplete entries (flags 0x0,
// all-zero MAC) are dropped.
func parseProcArp(content string) []arpEntry {
	var out []arpEntry
	for i, line := range strings.Split(content, "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // header
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		ip, err := netip.ParseAddr(fields[0])
		if err != nil {
			continue
		}
		if fields[2] == "0x0" || fields[3] == "00:00:00:00:00:00" {
			continue // incomplete resolution
		}
		mac := normalizeMAC(fields[3])
		if mac == "" {
			continue
		}
		out = append(out, arpEntry{IP: ip, MAC: mac})
	}
	return out
}

var arpAnLine = regexp.MustCompile(`\(([^)]+)\) at (\S+) on \S+`)

// parseArpAnOutput parses `arp -an` output (macOS and net-tools variants).
// Incomplete entries ("at (incomplete)") are dropped; IPv6 zones are stripped.
func parseArpAnOutput(content string) []arpEntry {
	var out []arpEntry
	for _, m := range arpAnLine.FindAllStringSubmatch(content, -1) {
		addr := strings.SplitN(m[1], "%", 2)[0]
		ip, err := netip.ParseAddr(addr)
		if err != nil {
			continue
		}
		macRaw := m[2]
		if macRaw == "incomplete" || strings.HasPrefix(macRaw, "(") || macRaw == "<complete>" {
			continue
		}
		mac := normalizeMAC(macRaw)
		if mac == "" {
			continue
		}
		out = append(out, arpEntry{IP: ip, MAC: mac})
	}
	return out
}

var arpScanLine = regexp.MustCompile(`^(\d+\.\d+\.\d+\.\d+)\s+([0-9A-Fa-f:]{12,17})(?:\s|$)`)

// parseArpScanOutput parses arp-scan report lines ("IP<TAB>MAC<TAB>vendor"),
// ignoring banner and summary lines.
func parseArpScanOutput(content string) []arpEntry {
	var out []arpEntry
	for _, line := range strings.Split(content, "\n") {
		m := arpScanLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ip, err := netip.ParseAddr(m[1])
		if err != nil {
			continue
		}
		mac := normalizeMAC(m[2])
		if mac == "" {
			continue
		}
		out = append(out, arpEntry{IP: ip, MAC: mac})
	}
	return out
}

// normalizeMAC canonicalizes a MAC to lowercase colon form, accepting
// dash-separated, bare-hex and compressed colon variants; returns "" when it
// is not a MAC.
func normalizeMAC(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", ":")
	s = strings.TrimPrefix(s, "0x")
	if len(s) == 12 && !strings.Contains(s, ":") {
		var b strings.Builder
		for i := 0; i < len(s); i += 2 {
			if i > 0 {
				b.WriteByte(':')
			}
			b.WriteString(s[i : i+2])
		}
		s = b.String()
	}
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return ""
	}
	for i, p := range parts {
		if len(p) == 0 || len(p) > 2 {
			return ""
		}
		for j := 0; j < len(p); j++ {
			c := p[j]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return ""
			}
		}
		if len(p) == 1 {
			parts[i] = "0" + p
		}
	}
	return strings.Join(parts, ":")
}
