package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/labvory/internal/store"
)

// SyncCloudflare imports the account's zones as domains and syncs each zone's
// DNS records as observed subdomains.
//
// Config: {"zone_names": ["example.com", ...]} — optional allowlist; empty
// imports every zone visible to the token.
// Secret: api_token (Cloudflare API token with Zone:Read + DNS:Read).
func SyncCloudflare(ctx context.Context, pool *pgxpool.Pool, integ store.Integration, apiToken string) (map[string]any, error) {
	if strings.TrimSpace(apiToken) == "" {
		return nil, errors.New("cloudflare integration needs the api_token secret (Zone:Read + DNS:Read)")
	}

	var cfg struct {
		ZoneNames []string `json:"zone_names"`
		APIBase   string   `json:"api_base"`
	}
	if len(integ.Config) > 0 {
		if err := json.Unmarshal(integ.Config, &cfg); err != nil {
			return nil, fmt.Errorf("invalid config: %w", err)
		}
	}
	filter := map[string]bool{}
	for _, name := range cfg.ZoneNames {
		name = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(name, ".")))
		if name != "" {
			filter[name] = true
		}
	}

	base := strings.TrimSuffix(cfg.APIBase, "/")
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	cf := &cloudflareClient{token: apiToken, base: base, hc: &http.Client{Timeout: 20 * time.Second}}
	zones, err := cf.zones(ctx)
	if err != nil {
		return nil, err
	}

	domains := store.NewDomains(pool)
	zonesSynced, zonesCreated, recordsSynced, recordsSkipped := 0, 0, 0, 0

	for _, zone := range zones {
		apex := strings.ToLower(zone.Name)
		if len(filter) > 0 && !filter[apex] {
			continue
		}

		domain, err := domains.GetByName(ctx, apex)
		if errors.Is(err, store.ErrNotFound) {
			domain, err = domains.Create(ctx, apex, "cloudflare", "Imported from Cloudflare.", nil)
			if err != nil {
				return nil, fmt.Errorf("create domain %s: %w", apex, err)
			}
			zonesCreated++
		} else if err != nil {
			return nil, fmt.Errorf("load domain %s: %w", apex, err)
		}

		recs, err := cf.dnsRecords(ctx, zone.ID)
		if err != nil {
			return nil, fmt.Errorf("zone %s records: %w", apex, err)
		}
		for _, rec := range recs {
			rtype := strings.ToUpper(rec.Type)
			if !cloudflareRecordTypes[rtype] {
				recordsSkipped++
				continue
			}
			name := strings.ToLower(strings.TrimSuffix(rec.Name, "."))
			if name != apex && !strings.HasSuffix(name, "."+apex) {
				recordsSkipped++
				continue
			}
			label := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, apex), "."), ".")
			value := rec.Content
			if rtype == "MX" && rec.Priority != nil {
				value = fmt.Sprintf("%d %s", *rec.Priority, rec.Content)
			}
			if _, err := domains.UpsertDiscovered(ctx, domain.ID, apex, label, rtype, value); err != nil {
				return nil, fmt.Errorf("store %s %s: %w", rtype, name, err)
			}
			recordsSynced++
		}
		zonesSynced++
	}

	return map[string]any{
		"zones":           zonesSynced,
		"zones_created":   zonesCreated,
		"records":         recordsSynced,
		"records_skipped": recordsSkipped,
	}, nil
}

// cloudflareRecordTypes are the record types the inventory models.
var cloudflareRecordTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "MX": true,
	"TXT": true, "SRV": true, "CAA": true, "NS": true,
}

// ---------- Cloudflare API v4 (minimal client) ----------

type cloudflareClient struct {
	token string
	base  string
	hc    *http.Client
}

type cloudflareZone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cloudflareRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority *int   `json:"priority"`
	TTL      int    `json:"ttl"`
}

type cloudflareEnvelope[T any] struct {
	Success    bool              `json:"success"`
	Errors     []cloudflareError `json:"errors"`
	Result     T                 `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

type cloudflareError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *cloudflareClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare request: %w", err)
	}
	defer resp.Body.Close()

	var env cloudflareEnvelope[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("cloudflare decode (%s): %w", resp.Status, err)
	}
	if resp.StatusCode != http.StatusOK || !env.Success {
		msg := resp.Status
		if len(env.Errors) > 0 {
			msg = fmt.Sprintf("%s: %s", resp.Status, env.Errors[0].Message)
		}
		return fmt.Errorf("cloudflare rejected the request (%s) — check the API token scopes", msg)
	}
	return json.Unmarshal(env.Result, out)
}

// zones lists every zone visible to the token.
func (c *cloudflareClient) zones(ctx context.Context) ([]cloudflareZone, error) {
	var all []cloudflareZone
	for page := 1; page <= 50; page++ {
		var batch []cloudflareZone
		if err := c.get(ctx, fmt.Sprintf("/zones?per_page=50&page=%d", page), &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 50 {
			break
		}
	}
	return all, nil
}

// dnsRecords lists all records of one zone.
func (c *cloudflareClient) dnsRecords(ctx context.Context, zoneID string) ([]cloudflareRecord, error) {
	var all []cloudflareRecord
	for page := 1; page <= 100; page++ {
		var batch []cloudflareRecord
		path := fmt.Sprintf("/zones/%s/dns_records?per_page=100&page=%d", url.PathEscape(zoneID), page)
		if err := c.get(ctx, path, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}
