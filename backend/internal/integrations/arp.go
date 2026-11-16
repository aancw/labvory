package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/petruknisme/lanvory/internal/store"
)

// SyncARP reads Layer-2 neighbors (ip ↔ mac) and records them as observations.
//
// Two modes:
//
//	table — parse the local ARP cache (/proc/net/arp on Linux, `arp -an` elsewhere);
//	        zero privileges, but only shows neighbors this host has talked to.
//	scan  — run the `arp-scan` binary (active sweep of a subnet; needs the binary
//	        plus NET_RAW, and host networking when Lanvory runs in a container).
//
// Every entry IP is marked as observed on a matching IP allocation
// (last_observed_at). When the MAC belongs to an asset interface, the asset gets
// a metadata.observed.arp entry with the last seen IP. Nothing new is created:
// unknown devices appear in the sync summary only, declared state is untouched.
//
// Config: {"mode": "table"|"scan", "interface": "eth0", "cidr": "192.168.1.0/24"}
// (interface/cidr apply to scan mode only).
func SyncARP(ctx context.Context, pool *pgxpool.Pool, integ store.Integration) (map[string]any, error) {
	var cfg struct {
		Mode      string `json:"mode"`
		Interface string `json:"interface"`
		CIDR      string `json:"cidr"`
	}
	if len(integ.Config) > 0 {
		if err := json.Unmarshal(integ.Config, &cfg); err != nil {
			return nil, fmt.Errorf("invalid config: %w", err)
		}
	}
	if cfg.Mode == "" {
		cfg.Mode = "table"
	}

	entries, err := collectArpEntries(ctx, cfg.Mode, cfg.Interface, cfg.CIDR)
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
		if !e.IP.IsValid() || e.MAC == "" {
			skipped++
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

	return map[string]any{
		"mode":           cfg.Mode,
		"entries":        len(entries),
		"ips_seen":       len(seenIPs),
		"macs_matched":   macsMatched,
		"macs_unknown":   macsUnknown,
		"assets_seen":    assetsSeen,
		"skipped":        skipped,
		"macs_duplicate": duplicateMACs,
	}, nil
}

// arpEntry is one L2 neighbor sighting.
type arpEntry struct {
	IP  netip.Addr
	MAC string // normalized (lowercase, colon-separated)
}

// collectArpEntries gathers neighbors from the chosen source.
func collectArpEntries(ctx context.Context, mode, iface, cidr string) ([]arpEntry, error) {
	switch mode {
	case "table":
		if raw, err := os.ReadFile("/proc/net/arp"); err == nil {
			return parseProcArp(string(raw)), nil
		}
		out, err := exec.CommandContext(ctx, "arp", "-an").Output()
		if err != nil {
			return nil, fmt.Errorf("read ARP table: %w (no /proc/net/arp and no arp binary)", err)
		}
		return parseArpAnOutput(string(out)), nil
	case "scan":
		if _, err := exec.LookPath("arp-scan"); err != nil {
			return nil, fmt.Errorf("arp-scan binary not found on the server host: install it (e.g. apk add arp-scan) and run the backend with NET_RAW; scanning a LAN from a container also needs host networking")
		}
		args := []string{}
		if strings.TrimSpace(iface) != "" {
			args = append(args, "-I", strings.TrimSpace(iface))
		}
		if strings.TrimSpace(cidr) != "" {
			args = append(args, strings.TrimSpace(cidr))
		} else {
			args = append(args, "-l")
		}
		cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cmdCtx, "arp-scan", args...).Output()
		if err != nil {
			return nil, fmt.Errorf("arp-scan failed (needs root/NET_RAW and a LAN-facing interface): %w", err)
		}
		return parseArpScanOutput(string(out)), nil
	default:
		return nil, fmt.Errorf("unsupported arp mode %q (use table or scan)", mode)
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
