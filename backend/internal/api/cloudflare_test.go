package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/petruknisme/lanvory/internal/integrations"
	"github.com/petruknisme/lanvory/internal/store"
)

// TestCloudflareSync runs the connector against a fake Cloudflare API and
// asserts zones become domains and records become observed subdomains.
// It lives in the api package because that package owns the serialized
// integration-test database (truncateAll), avoiding cross-package races.
func TestCloudflareSync(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	srv, _, _, do := newTestServer(t, databaseURL)
	bootAdmin(t, do)

	suffix := fmt.Sprintf("t%d.test", time.Now().UnixNano())
	apex := "cf-" + suffix
	other := "lab-" + suffix

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"auth"}]}`))
			return
		}
		switch r.URL.Path {
		case "/zones":
			_, _ = fmt.Fprintf(w, `{"success":true,"result":[
				{"id":"zone1","name":%q},
				{"id":"zone2","name":%q}
			],"result_info":{"page":1,"total_pages":1}}`, apex, other)
		case "/zones/zone1/dns_records":
			_, _ = fmt.Fprintf(w, `{"success":true,"result":[
				{"type":"A","name":%q,"content":"203.0.113.10","ttl":300},
				{"type":"A","name":%q,"content":"203.0.113.10","ttl":300},
				{"type":"CNAME","name":%q,"content":"ghs.example.org","ttl":300},
				{"type":"MX","name":%q,"content":%q,"priority":10,"ttl":300},
				{"type":"HTTPS","name":%q,"content":"1 . alpn=h2","ttl":300}
			],"result_info":{"page":1,"total_pages":1}}`,
				apex, "www."+apex, "blog."+apex, apex, "mail."+apex, apex)
		case "/zones/zone2/dns_records":
			_, _ = w.Write([]byte(`{"success":true,"result":[],"result_info":{"page":1,"total_pages":1}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	integ := store.Integration{
		Kind:   "cloudflare",
		Name:   "cf-" + suffix,
		Config: []byte(`{"api_base":"` + fake.URL + `","zone_names":["` + apex + `"]}`),
	}
	summary, err := integrations.SyncCloudflare(context.Background(), srv.pool, integ, "test-token")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := summary["zones"]; got != 1 {
		t.Fatalf("zones = %v, want 1 (allowlist filters the second zone)", got)
	}
	if got := summary["records"]; got != 4 {
		t.Fatalf("records = %v, want 4 (HTTPS type skipped)", got)
	}

	domains := store.NewDomains(srv.pool)
	domain, err := domains.GetByName(context.Background(), apex)
	if err != nil {
		t.Fatalf("domain lookup: %v", err)
	}
	if domain.Registrar != "cloudflare" {
		t.Fatalf("registrar = %q", domain.Registrar)
	}
	records, err := domains.ListSubdomains(context.Background(), domain.ID, domain.Name)
	if err != nil {
		t.Fatalf("list subdomains: %v", err)
	}
	byKey := map[string]store.Subdomain{}
	for _, rec := range records {
		byKey[rec.Label+"|"+rec.RecordType] = rec
	}
	apexA, ok := byKey["|A"]
	if !ok {
		t.Fatalf("missing apex A record: %+v", records)
	}
	if apexA.FQDN != apex || apexA.ObservedValue != "203.0.113.10" {
		t.Fatalf("apex A = %+v", apexA)
	}
	if _, ok := byKey["www|A"]; !ok {
		t.Fatalf("missing www A record")
	}
	if mx, ok := byKey["|MX"]; !ok || mx.ObservedValue != "10 mail."+apex {
		t.Fatalf("MX record wrong: %+v", mx)
	}
	if _, ok := byKey["blog|CNAME"]; !ok {
		t.Fatalf("missing blog CNAME")
	}
	// A filtered-out zone must not be created.
	if _, err := domains.GetByName(context.Background(), other); err == nil {
		t.Fatalf("filtered zone was imported")
	}

	// Missing token is refused before any API call.
	if _, err := integrations.SyncCloudflare(context.Background(), srv.pool, integ, ""); err == nil {
		t.Fatalf("expected error without api token")
	}
}
