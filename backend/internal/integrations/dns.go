package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/petruknisme/lanvory/internal/dnsx"
	"github.com/petruknisme/lanvory/internal/store"
)

// SyncDNS enumerates a domain's records and stores them as observed subdomains.
// Two modes:
//
//	axfr    — full zone transfer against a nameserver (internal zones)
//	crtname — certificate transparency via crt.name (public domains)
//
// Existing records keep their declared values; only observed_* is refreshed.
//
// Config: {"domain_id": "...", "mode": "axfr"|"crtname", "server": "192.168.1.1:53"}
func SyncDNS(ctx context.Context, pool *pgxpool.Pool, integ store.Integration) (map[string]any, error) {
	var cfg struct {
		DomainID string `json:"domain_id"`
		Mode     string `json:"mode"`
		Server   string `json:"server"`
	}
	if err := json.Unmarshal(integ.Config, &cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if cfg.DomainID == "" {
		return nil, fmt.Errorf("dns integration needs config.domain_id (the domain to enumerate)")
	}
	if cfg.Mode == "" {
		cfg.Mode = "axfr"
	}

	domains := store.NewDomains(pool)
	domain, err := domains.Get(ctx, cfg.DomainID)
	if err != nil {
		return nil, fmt.Errorf("load domain: %w", err)
	}
	apex := strings.ToLower(strings.TrimSuffix(domain.Name, "."))

	var discovered []dnsx.Record
	switch cfg.Mode {
	case "axfr":
		discovered, err = dnsx.Transfer(ctx, cfg.Server, apex, 15*time.Second)
		if err != nil {
			return nil, err
		}
	case "crtname", "crtsh": // "crtsh" kept as an alias for older configs
		cfg.Mode = "crtname"
		names, err := crtNameHosts(ctx, apex)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			discovered = append(discovered, dnsx.Record{Name: name, Type: "A"})
		}
	default:
		return nil, fmt.Errorf("unsupported dns mode %q (use axfr or crtname)", cfg.Mode)
	}

	// Filter and de-duplicate first, then resolve missing values concurrently —
	// a CT sync can surface hundreds of names, so sequential lookups dominate.
	type item struct {
		label, rtype, name, value string
	}
	items := make([]item, 0, len(discovered))
	seen := map[string]bool{}
	skipped := 0
	for _, rec := range discovered {
		name := strings.ToLower(strings.TrimSuffix(rec.Name, "."))
		if name != apex && !strings.HasSuffix(name, "."+apex) {
			skipped++
			continue
		}
		label := strings.TrimSuffix(strings.TrimSuffix(name, apex), ".")
		label = strings.TrimSuffix(label, ".")
		key := label + "|" + rec.Type
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, item{label: label, rtype: rec.Type, name: name, value: rec.Value})
	}

	resolveCtx, cancelResolve := context.WithTimeout(ctx, ctResolveBudget)
	defer cancelResolve()
	sem := make(chan struct{}, 12)
	var wg sync.WaitGroup
	for i := range items {
		if items[i].value != "" {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-resolveCtx.Done():
				return
			}
			defer func() { <-sem }()
			if v, err := dnsx.ResolveRecord(resolveCtx, items[i].name, items[i].rtype); err == nil {
				items[i].value = v
			}
		}(i)
	}
	wg.Wait()

	stored, observed := 0, 0
	for _, it := range items {
		if _, err := domains.UpsertDiscovered(ctx, domain.ID, apex, it.label, it.rtype, it.value); err != nil {
			return nil, fmt.Errorf("store %s %s: %w", it.rtype, it.name, err)
		}
		stored++
		if it.value != "" {
			observed++
		}
	}

	// Drift pass: only records with a declared value can drift, which keeps a
	// large CT enumeration from triggering thousands of lookups.
	records, err := domains.ListSubdomains(ctx, domain.ID, apex)
	if err != nil {
		return nil, err
	}
	var check []store.Subdomain
	for _, rec := range records {
		if strings.TrimSpace(rec.Value) != "" {
			check = append(check, rec)
		}
	}
	resolved, drifted := resolveForDrift(ctx, domains, check)

	return map[string]any{
		"mode":       cfg.Mode,
		"domain":     apex,
		"discovered": stored,
		"observed":   observed,
		"skipped":    skipped,
		"resolved":   resolved,
		"drifted":    drifted,
	}, nil
}

// resolveForDrift re-resolves the given records with bounded concurrency and
// returns (resolved, drifted) counts.
func resolveForDrift(ctx context.Context, domains *store.Domains, records []store.Subdomain) (int, int) {
	var mu sync.Mutex
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	resolved, drifted := 0, 0
	for _, rec := range records {
		wg.Add(1)
		go func(rec store.Subdomain) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			v, err := dnsx.ResolveRecord(ctx, rec.FQDN, rec.RecordType)
			if err != nil || v == "" {
				return
			}
			if _, err := domains.SetObserved(ctx, rec.ID, v); err != nil {
				return
			}
			mu.Lock()
			resolved++
			if dnsx.Drift(rec.Value, v) {
				drifted++
			}
			mu.Unlock()
		}(rec)
	}
	wg.Wait()
	return resolved, drifted
}

// ctNameLimit bounds how many hostnames one CT sync imports. Public apexes can
// return tens of thousands of names, which is not useful inventory.
const ctNameLimit = 500

// ctResolveBudget bounds how long one CT sync spends filling in observed
// addresses. Unresolved names are still imported as records (declared values
// stay empty and can be filled later by a manual resolve).
const ctResolveBudget = 25 * time.Second

// crtNameBaseURL is overridable in tests.
var crtNameBaseURL = "https://crt.name"

// crtNameHosts fetches subdomain names for a zone from crt.name, a certificate
// transparency search service (crt.sh is no longer reliable).
// Endpoint: GET https://crt.name/v1/search?apex=<zone> → newline-separated names.
func crtNameHosts(ctx context.Context, apex string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, crtNameBaseURL+"/v1/search?apex="+url.QueryEscape(apex), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "lanvory-dns-sync/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crt.name request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return nil, fmt.Errorf("crt.name returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	// Never buffer an unbounded response (52k+ lines for busy apexes).
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("crt.name read: %w", err)
	}

	set := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		name := strings.ToLower(strings.TrimSpace(line))
		name = strings.TrimPrefix(name, "*.")
		if name == "" || !strings.HasSuffix(name, "."+apex) {
			continue
		}
		set[name] = true
		if len(set) >= ctNameLimit {
			break
		}
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
