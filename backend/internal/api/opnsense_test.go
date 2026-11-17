package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aancw/lanvory/internal/integrations"
	"github.com/aancw/lanvory/internal/store"
)

// TestOPNsenseSync runs the DHCP lease connector against a fake OPNsense API:
// leases mark tracked allocations observed and record hostname/MAC under
// metadata.observed.opnsense without creating assets or touching declared
// fields. It lives in the api package because that package owns the
// serialized integration-test database (truncateAll).
func TestOPNsenseSync(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}
	srv, _, _, do := newTestServer(t, databaseURL)
	bootAdmin(t, do)
	ctx := context.Background()

	var gotUser, gotPass, gotPath string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		key, secret, ok := r.BasicAuth()
		gotUser, gotPass = key, secret
		if !ok || key != "test-key" || secret != "test-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"rows":[
			{"address":"10.77.0.10","mac":"AA:BB:CC:DD:EE:01","hostname":"nas.lab","state":"active"},
			{"ip":"10.77.0.11","mac":"AA:BB:CC:DD:EE:02"},
			{"descr":"static-map"}
		]}`))
	}))
	defer fake.Close()

	// Seed a subnet and one tracked allocation (a context is required).
	contexts, err := store.NewNetworkContexts(srv.pool).List(ctx)
	if err != nil || len(contexts) == 0 {
		t.Fatalf("no network context available: %v", err)
	}
	subnets := store.NewSubnets(srv.pool)
	sn := store.Subnet{CIDR: "10.77.0.0/24", Name: "opn-lab", ContextID: contexts[0].ID, Environment: "lab"}
	if err := subnets.Create(ctx, &sn); err != nil {
		t.Fatalf("seed subnet: %v", err)
	}
	ip := store.IPAddress{SubnetID: sn.ID, Address: "10.77.0.10", Status: "assigned"}
	if err := store.NewIPAddresses(srv.pool).Allocate(ctx, &ip, "", ""); err != nil {
		t.Fatalf("seed allocation: %v", err)
	}

	integ := store.Integration{
		Kind:   "opnsense",
		Name:   "opn-lab",
		Config: []byte(`{"endpoint":"` + fake.URL + `","verify_tls":false}`),
	}
	summary, err := integrations.SyncOPNsense(ctx, srv.pool, integ, "test-key", "test-secret")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	if gotUser != "test-key" || gotPass != "test-secret" {
		t.Fatalf("basic auth not sent as expected (got %q/%q)", gotUser, gotPass)
	}
	if gotPath != "/api/dhcpd/v4/leases/searchLease" {
		t.Fatalf("default lease path = %q", gotPath)
	}
	if summary["leases"] != 3 || summary["ips_seen"] != 2 || summary["ips_marked"] != 1 || summary["ips_unknown"] != 1 || summary["skipped"] != 1 {
		t.Fatalf("summary mismatch: %v", summary)
	}

	// The tracked allocation is marked observed with the lease metadata.
	var observed bool
	var hostname, mac string
	err = srv.pool.QueryRow(ctx, `
		SELECT last_observed_at IS NOT NULL,
		       metadata->'observed'->'opnsense'->>'hostname',
		       metadata->'observed'->'opnsense'->>'mac'
		FROM ip_addresses WHERE address = '10.77.0.10'`).Scan(&observed, &hostname, &mac)
	if err != nil {
		t.Fatalf("load allocation: %v", err)
	}
	if !observed || hostname != "nas.lab" || mac != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("observation not recorded as expected (observed=%v hostname=%q mac=%q)", observed, hostname, mac)
	}

	// Declared fields untouched.
	var status string
	if err := srv.pool.QueryRow(ctx, `SELECT status FROM ip_addresses WHERE address='10.77.0.10'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "assigned" {
		t.Fatalf("declared status changed to %q", status)
	}

	// Bad credentials surface a clear error.
	if _, err := integrations.SyncOPNsense(ctx, srv.pool, integ, "wrong", "creds"); err == nil ||
		!strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected credential error, got %v", err)
	}
}

// TestExportsCSV covers the server-side CSV export endpoints: correct media
// type, headers, and complete rows (no pagination caps).
func TestExportsCSV(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}
	srv, baseURL, client, do := newTestServer(t, databaseURL)
	_ = srv
	bootAdmin(t, do)

	typeID := ""
	types := do(http.MethodGet, "/api/v1/asset-types", nil, http.StatusOK)
	for _, raw := range types["types"].([]any) {
		typ := raw.(map[string]any)
		if typ["slug"] == "server" {
			typeID = typ["id"].(string)
		}
	}
	do(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "csv-server", "hostname": "csv-host", "type_id": typeID, "status": "online",
		"serial_number": "SN-CSV-1",
	}, http.StatusCreated)

	// Subnet with one tracked address.
	ctxResp := do(http.MethodGet, "/api/v1/network-contexts", nil, http.StatusOK)
	var contextID string
	for _, raw := range ctxResp["contexts"].([]any) {
		c := raw.(map[string]any)
		if c["slug"] == "default" {
			contextID = c["id"].(string)
			break
		}
	}
	subnet := do(http.MethodPost, "/api/v1/subnets", map[string]any{
		"cidr": "10.66.0.0/24", "name": "csv-lab", "context_id": contextID,
	}, http.StatusCreated)
	subnetID := subnet["subnet"].(map[string]any)["id"].(string)
	do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses", map[string]any{
		"address": "10.66.0.10", "status": "assigned", "hostname": "printer",
	}, http.StatusCreated)

	// The cookie client carries the session; CSV endpoints are GET (no CSRF).
	fetchCSV := func(path string) (string, *http.Response) {
		t.Helper()
		resp, err := client.Get(baseURL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body), resp
	}

	body, resp := fetchCSV("/api/v1/export/assets.csv")
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("assets.csv content-type = %q", ct)
	}
	if !strings.Contains(body, "csv-server") || !strings.Contains(body, "csv-host") || !strings.Contains(body, "SN-CSV-1") {
		t.Fatalf("assets.csv missing seeded rows:\n%s", body)
	}
	if !strings.HasPrefix(body, "id,name,hostname,") {
		t.Fatalf("assets.csv header mismatch:\n%s", body)
	}

	body, resp = fetchCSV("/api/v1/export/ip-addresses.csv")
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("ip-addresses.csv content-type = %q", ct)
	}
	if !strings.Contains(body, "10.66.0.10") || !strings.Contains(body, "printer") {
		t.Fatalf("ip-addresses.csv missing seeded rows:\n%s", body)
	}
}

// TestSecurityNotifications covers the webhook paths: a failed-login burst
// notification on every 5th rejection within the window and a new-device
// notification when a sign-in uses a previously unseen source IP.
func TestSecurityNotifications(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}
	srv, _, _, do := newTestServerWithSecrets(t, databaseURL)
	bootAdmin(t, do)

	type received struct {
		title string
		body  string
	}
	hits := make(chan received, 8)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hits <- received{title: r.Header.Get("X-Title"), body: string(body)}
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()
	t.Cleanup(func() { close(hits) })

	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"notifications.webhook_url": receiver.URL},
	}, http.StatusOK)

	// Seed a session from a different source so the next real sign-in counts
	// as new-device (the audit query also needs a prior context to compare).
	if err := store.NewSessions(srv.pool).Create(t.Context(), store.Session{
		UserID:    currentUserID(t, srv),
		TokenHash: strings.Repeat("a", 64),
		ExpiresAt: time.Now().Add(time.Hour),
		SourceIP:  "203.0.113.77",
		UserAgent: "test-seeded-device",
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// Four rejections stay silent; the fifth bursts one notification.
	for i := 0; i < 4; i++ {
		do(http.MethodPost, "/api/v1/auth/login", map[string]any{
			"username": "admin", "password": "totally-wrong",
		}, http.StatusUnauthorized)
	}
	select {
	case hit := <-hits:
		t.Fatalf("unexpected notification after %d failures: %q", 4, hit.title)
	default:
	}
	do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "totally-wrong",
	}, http.StatusUnauthorized)
	select {
	case hit := <-hits:
		if hit.title != "Failed login burst" {
			t.Fatalf("burst title = %q", hit.title)
		}
		if !strings.Contains(hit.body, "5 failed sign-in attempts") {
			t.Fatalf("burst body = %q", hit.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no burst notification delivered")
	}

	// Sign out, then sign in again from an IP this account has never used —
	// the only remaining session is the seeded 203.0.113.77 one.
	do(http.MethodPost, "/api/v1/auth/logout", nil, http.StatusOK)
	do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusOK)
	select {
	case hit := <-hits:
		if hit.title != "New device signed in" {
			t.Fatalf("new-device title = %q", hit.title)
		}
		if !strings.Contains(hit.body, "admin") {
			t.Fatalf("new-device body = %q", hit.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no new-device notification delivered")
	}
}
