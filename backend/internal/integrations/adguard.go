package integrations

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/lanvory/internal/store"
)

// SyncAdGuard imports the AdGuard Home persistent client list and DNS rewrites.
//
// Clients: ids that parse as IPs are recorded as observations (last_observed_at)
// on matching IP allocations — discovered clients never become assets.
// Rewrites: only answers whose IP is local (private/loopback/link-local/ULA/
// CGNAT) are imported, as observed subdomains under auto-created domains.
// Declared values are never overwritten.
//
// Config: {"endpoint": "http://192.168.1.11:80", "username": "admin", "verify_tls": true}
// Secret:  password (basic auth).
func SyncAdGuard(ctx context.Context, pool *pgxpool.Pool, integ store.Integration, password string) (map[string]any, error) {
	var cfg struct {
		Endpoint  string `json:"endpoint"`
		Username  string `json:"username"`
		VerifyTLS *bool  `json:"verify_tls"`
	}
	if err := json.Unmarshal(integ.Config, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, errors.New("adguard integration needs config.endpoint (e.g. http://192.168.1.11:80)")
	}
	if strings.TrimSpace(cfg.Username) == "" {
		return nil, errors.New("adguard integration needs config.username")
	}
	verify := cfg.VerifyTLS == nil || *cfg.VerifyTLS
	ag := &adguardClient{
		base:     strings.TrimSuffix(strings.TrimSpace(cfg.Endpoint), "/"),
		username: cfg.Username,
		password: password,
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

	now := time.Now().UTC()
	ips := store.NewIPAddresses(pool)
	domains := store.NewDomains(pool)

	// Clients: record observations on matching IP allocations.
	clients, err := ag.clients(ctx)
	if err != nil {
		return nil, err
	}
	clientsMarked, clientsSkipped := 0, 0
	for _, c := range clients {
		for _, id := range c.IDs {
			addr, err := netip.ParseAddr(strings.TrimSpace(id))
			if err != nil {
				clientsSkipped++ // MACs, CIDRs and hostnames are not IP observations
				continue
			}
			if err := ips.MarkObserved(ctx, addr.String(), now); err != nil {
				return nil, fmt.Errorf("observe client %s: %w", id, err)
			}
			clientsMarked++
		}
	}

	// Rewrites: local answers only, as observed records.
	rewrites, err := ag.rewrites(ctx)
	if err != nil {
		return nil, err
	}
	domainsCreated, imported, skipped := 0, 0, 0
	domainIDs := map[string]string{}
	for _, r := range rewrites {
		if !r.Enabled {
			skipped++
			continue
		}
		addr, err := netip.ParseAddr(strings.TrimSpace(r.Answer))
		if err != nil || !isLocalAnswer(addr) {
			skipped++
			continue
		}
		apex, label := splitRewriteDomain(r.Domain)
		if apex == "" {
			skipped++
			continue
		}
		domainID, created, err := ensureDomain(ctx, domains, apex, domainIDs)
		if err != nil {
			return nil, err
		}
		if created {
			domainsCreated++
		}
		rtype := "A"
		if addr.Is6() {
			rtype = "AAAA"
		}
		if _, err := domains.UpsertDiscovered(ctx, domainID, apex, label, rtype, addr.String()); err != nil {
			return nil, fmt.Errorf("store rewrite %s: %w", r.Domain, err)
		}
		imported++
	}

	return map[string]any{
		"clients":          len(clients),
		"clients_marked":   clientsMarked,
		"clients_skipped":  clientsSkipped,
		"rewrites":         len(rewrites),
		"rewrites_local":   imported,
		"rewrites_skipped": skipped,
		"domains_created":  domainsCreated,
	}, nil
}

// isLocalAnswer reports whether an IP is "relevant for local": RFC1918, loopback,
// link-local, IPv6 ULA (all via netip) or CGNAT 100.64/10 (Tailscale & friends,
// which IsPrivate deliberately excludes).
func isLocalAnswer(addr netip.Addr) bool {
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return true
	}
	if addr.Is4() && netip.MustParsePrefix("100.64.0.0/10").Contains(addr) {
		return true
	}
	return false
}

// splitRewriteDomain splits an AdGuard rewrite into (apex, label) using the
// flat model AdGuard itself uses (no zone delegation): the last label is the
// apex, everything before it is the label. "kali.lab" → ("lab", "kali"),
// "*.dokploy.lab" → ("lab", "*.dokploy"), "*.lolab" → ("lolab", "*").
func splitRewriteDomain(name string) (apex, label string) {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if name == "" || name == "*" {
		return "", ""
	}
	labels := strings.Split(name, ".")
	apex = labels[len(labels)-1]
	label = strings.Join(labels[:len(labels)-1], ".")
	return apex, label
}

// ensureDomain find-or-creates the domain row for an apex, caching ids per sync.
func ensureDomain(ctx context.Context, domains *store.Domains, apex string, cache map[string]string) (id string, created bool, err error) {
	if id, ok := cache[apex]; ok {
		return id, false, nil
	}
	domain, err := domains.GetByName(ctx, apex)
	if errors.Is(err, store.ErrNotFound) {
		domain, err = domains.Create(ctx, apex, "adguard", "Imported from AdGuard Home rewrites.", nil)
		if err != nil {
			return "", false, fmt.Errorf("create domain %s: %w", apex, err)
		}
		created = true
	} else if err != nil {
		return "", false, fmt.Errorf("load domain %s: %w", apex, err)
	}
	cache[apex] = domain.ID
	return domain.ID, created, nil
}

// ---------- AdGuard Home API (minimal client) ----------

type adguardClient struct {
	base     string
	username string
	password string
	hc       *http.Client
}

type adguardClientEntry struct {
	Name string   `json:"name"`
	IDs  []string `json:"ids"`
}

type adguardRewrite struct {
	Domain  string `json:"domain"`
	Answer  string `json:"answer"`
	Enabled bool   `json:"enabled"`
}

func (c *adguardClient) get(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("HTTP %d from %s (check credentials): %s", resp.StatusCode, path, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *adguardClient) clients(ctx context.Context) ([]adguardClientEntry, error) {
	var out []adguardClientEntry
	if err := c.get(ctx, "/control/clients/list", &out); err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}
	return out, nil
}

func (c *adguardClient) rewrites(ctx context.Context) ([]adguardRewrite, error) {
	var out []adguardRewrite
	if err := c.get(ctx, "/control/rewrite/list", &out); err != nil {
		return nil, fmt.Errorf("list rewrites: %w", err)
	}
	return out, nil
}
