package integrations

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/labvory/internal/store"
)

// SyncOPNsense imports DHCP lease state from an OPNsense firewall.
//
// Leases: every lease that resolves to a tracked IP address marks that
// allocation as observed (last_observed_at) and records the reported
// hostname/MAC under metadata.observed.opnsense. The sync is read-only:
// leases never create assets, and declared fields (status, hostname,
// description, assignment) are never overwritten.
//
// Config: {"endpoint": "https://opnsense.lan", "lease_path":
// "/api/dhcpd/v4/leases/searchLease", "verify_tls": true} — lease_path is
// overridable because the endpoint moved between OPNsense releases.
// Secrets:  api_key + api_secret (OPNsense API key pair).
func SyncOPNsense(ctx context.Context, pool *pgxpool.Pool, integ store.Integration, apiKey, apiSecret string) (map[string]any, error) {
	if strings.TrimSpace(apiKey) == "" || strings.TrimSpace(apiSecret) == "" {
		return nil, errors.New("opnsense integration needs the api_key and api_secret secrets (create an API key pair under System › Access › Users)")
	}

	var cfg struct {
		Endpoint  string `json:"endpoint"`
		LeasePath string `json:"lease_path"`
		VerifyTLS *bool  `json:"verify_tls"`
	}
	if len(integ.Config) > 0 {
		if err := json.Unmarshal(integ.Config, &cfg); err != nil {
			return nil, fmt.Errorf("invalid config: %w", err)
		}
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("opnsense integration needs config.endpoint (e.g. https://opnsense.lan)")
	}
	if cfg.LeasePath == "" {
		cfg.LeasePath = "/api/dhcpd/v4/leases/searchLease"
	}
	verify := cfg.VerifyTLS == nil || *cfg.VerifyTLS
	client := &opnsenseClient{
		base:   strings.TrimSuffix(strings.TrimSpace(cfg.Endpoint), "/"),
		key:    apiKey,
		secret: apiSecret,
		path:   cfg.LeasePath,
		hc: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion:         tls.VersionTLS12,
					InsecureSkipVerify: !verify, //nolint:gosec // user-controlled self-signed instances
				},
			},
		},
	}

	rows, err := client.leases(ctx)
	if err != nil {
		return nil, err
	}

	ips := store.NewIPAddresses(pool)
	now := time.Now().UTC()
	seen, marked, unknown, skipped := 0, 0, 0, 0
	for _, row := range rows {
		address := row.firstOf("address", "ip", "ip_address")
		if i := strings.IndexByte(address, '/'); i > 0 {
			address = address[:i] // tolerate address/prefix forms
		}
		addr, err := netip.ParseAddr(strings.TrimSpace(address))
		if err != nil || addr.IsUnspecified() {
			skipped++ // static entries, in-progress rows, or unusable shapes
			continue
		}
		seen++

		obs := map[string]any{"checked_at": now.Format(time.RFC3339)}
		if mac := row.firstOf("mac", "mac_address"); mac != "" {
			obs["mac"] = normalizeMAC(mac)
		}
		if host := row.firstOf("hostname", "client_hostname", "client-hostname"); host != "" {
			obs["hostname"] = host
		}
		matched, err := ips.ObserveLease(ctx, addr.String(), now, "opnsense", obs)
		if err != nil {
			return nil, fmt.Errorf("observe lease %s: %w", addr, err)
		}
		if matched {
			marked++
		} else {
			unknown++ // not tracked in IPAM: observed state only, nothing created
		}
	}

	return map[string]any{
		"leases":      len(rows),
		"ips_seen":    seen,
		"ips_marked":  marked,
		"ips_unknown": unknown,
		"skipped":     skipped,
	}, nil
}

// ---------- OPNsense REST client (minimal) ----------

type opnsenseClient struct {
	base   string
	key    string
	secret string
	path   string
	hc     *http.Client
}

// leaseRow is one raw lease object; field names vary between OPNsense
// releases, so lookups are tolerant.
type leaseRow map[string]any

// firstOf returns the first non-empty string among the candidate keys.
func (r leaseRow) firstOf(keys ...string) string {
	for _, k := range keys {
		for name, v := range r {
			if strings.EqualFold(name, k) {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
	}
	return ""
}

// leases fetches the lease list. The endpoint may answer a bare array or a
// {"rows": [...]} envelope depending on the release.
func (c *opnsenseClient) leases(ctx context.Context) ([]leaseRow, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+c.path, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.key, c.secret)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opnsense request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("opnsense rejected the credentials (%s) — check the API key pair", resp.Status)
		}
		return nil, fmt.Errorf("opnsense returned %s for %s — adjust lease_path if this release uses another endpoint", resp.Status, c.path)
	}

	var body json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("opnsense decode (%s): %w", resp.Status, err)
	}
	var rows []leaseRow
	if err := json.Unmarshal(body, &rows); err == nil {
		return rows, nil
	}
	var envelope struct {
		Rows []leaseRow `json:"rows"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("opnsense lease response shape not recognized (expected an array or {rows: [...]})")
	}
	return envelope.Rows, nil
}
