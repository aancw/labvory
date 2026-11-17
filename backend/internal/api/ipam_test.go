package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/aancw/lanvory/internal/auth"
	"github.com/aancw/lanvory/internal/config"
	"github.com/aancw/lanvory/internal/db"
	"github.com/aancw/lanvory/internal/secrets"
	"github.com/aancw/lanvory/internal/store"
)

// TestIPAMWorkflow is the Phase 3 acceptance scenario: contexts, VLANs, subnet
// creation with overlap rejection, allocation (auto + explicit), duplicate
// prevention, release history, bulk reserve/release, and grid data.
func TestIPAMWorkflow(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}

	srv, _, client, do := newTestServer(t, databaseURL)
	_ = srv
	_ = client

	do(http.MethodPost, "/api/v1/setup/admin", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusCreated)

	// Contexts + VLANs.
	ctxResp := do(http.MethodGet, "/api/v1/network-contexts", nil, http.StatusOK)
	var contextID string
	for _, raw := range ctxResp["contexts"].([]any) {
		c := raw.(map[string]any)
		if c["slug"] == "default" {
			contextID = c["id"].(string)
		}
	}
	if contextID == "" {
		t.Fatal("default context missing")
	}

	vlan := do(http.MethodPost, "/api/v1/vlans", map[string]any{
		"context_id": contextID, "vlan_id": 10, "name": "Servers", "purpose": "management",
	}, http.StatusCreated)
	vlanID := vlan["id"].(string)

	do(http.MethodPost, "/api/v1/vlans", map[string]any{
		"context_id": contextID, "vlan_id": 10, "name": "Duplicate",
	}, http.StatusConflict)
	do(http.MethodPost, "/api/v1/vlans", map[string]any{
		"context_id": contextID, "vlan_id": 5000, "name": "OutOfRange",
	}, http.StatusUnprocessableEntity)

	// Subnet creation.
	subnet := do(http.MethodPost, "/api/v1/subnets", map[string]any{
		"cidr": "10.10.0.0/24", "name": "Servers LAN", "context_id": contextID,
		"vlan_id": vlanID, "gateway": "10.10.0.1", "environment": "production",
		"dns_servers": []string{"10.10.0.53"},
	}, http.StatusCreated)
	subnetID := subnet["subnet"].(map[string]any)["id"].(string)

	// Overlap rejected in the same context.
	do(http.MethodPost, "/api/v1/subnets", map[string]any{
		"cidr": "10.10.0.0/25", "name": "Overlap", "context_id": contextID,
	}, http.StatusConflict)

	// Network/broadcast rejection.
	do(http.MethodPost, "/api/v1/subnets", map[string]any{
		"cidr": "10.10.0.0/24", "name": "BadGW", "context_id": contextID, "gateway": "10.10.0.0",
	}, http.StatusUnprocessableEntity)

	// Grid: 254 usable cells, network+broadcast excluded.
	grid := do(http.MethodGet, "/api/v1/subnets/"+subnetID+"/grid", nil, http.StatusOK)
	if int(grid["usable"].(float64)) != 254 {
		t.Fatalf("expected 254 usable, got %v", grid["usable"])
	}
	if len(grid["cells"].([]any)) != 254 {
		t.Fatalf("expected 254 grid cells")
	}
	if len(grid["next_available"].([]any)) == 0 {
		t.Fatal("expected next-available suggestions")
	}

	// Explicit allocation.
	alloc := do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses", map[string]any{
		"address": "10.10.0.10", "status": "assigned", "assignment_method": "static",
		"hostname": "pve-01", "description": "Proxmox host",
	}, http.StatusCreated)
	ipID := alloc["id"].(string)

	// Duplicate active allocation rejected.
	do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses", map[string]any{
		"address": "10.10.0.10", "status": "reserved",
	}, http.StatusConflict)

	// Auto allocation picks the lowest free address (10.10.0.1).
	auto := do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses", map[string]any{
		"status": "reserved", "description": "reserved for uplink",
	}, http.StatusCreated)
	if auto["address"] != "10.10.0.1" {
		t.Fatalf("auto allocation should take .1, got %v", auto["address"])
	}

	// Network + broadcast rejected.
	do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses", map[string]any{
		"address": "10.10.0.0", "status": "reserved",
	}, http.StatusUnprocessableEntity)
	do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses", map[string]any{
		"address": "10.10.0.255", "status": "reserved",
	}, http.StatusUnprocessableEntity)

	// Utilization counters.
	detail := do(http.MethodGet, "/api/v1/subnets/"+subnetID, nil, http.StatusOK)
	sn := detail["subnet"].(map[string]any)
	if int(sn["assigned"].(float64)) != 1 || int(sn["reserved"].(float64)) != 1 {
		t.Fatalf("utilization counters wrong: %+v", sn)
	}
	if int(sn["utilization"].(float64)) == 0 {
		t.Fatal("utilization should be nonzero")
	}

	// Release + history.
	do(http.MethodPost, "/api/v1/ip-addresses/"+ipID+"/release", nil, http.StatusOK)
	history := do(http.MethodGet, "/api/v1/ip-addresses/"+ipID+"/history", nil, http.StatusOK)
	h := history["history"].([]any)
	if len(h) != 1 {
		t.Fatalf("expected 1 closed history row, got %d", len(h))
	}
	first := h[0].(map[string]any)
	if first["released_at"] == nil {
		t.Fatal("released history row must carry released_at")
	}

	// Re-allocate the released address succeeds (row was released → claimable).
	do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses", map[string]any{
		"address": "10.10.0.10", "status": "assigned", "assignment_method": "static",
	}, http.StatusCreated)

	// Bulk reserve .200-.209, then verify, then bulk release.
	bulk := do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses/bulk", map[string]any{
		"action": "reserve", "start": "10.10.0.200", "end": "10.10.0.209",
		"description": "ap management",
	}, http.StatusOK)
	if int(bulk["affected"].(float64)) != 10 {
		t.Fatalf("bulk reserve affected mismatch: %v", bulk["affected"])
	}
	detail = do(http.MethodGet, "/api/v1/subnets/"+subnetID, nil, http.StatusOK)
	if int(detail["subnet"].(map[string]any)["reserved"].(float64)) != 11 { // .1 + 10
		t.Fatalf("reserved count mismatch: %v", detail["subnet"].(map[string]any)["reserved"])
	}
	do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses/bulk", map[string]any{
		"action": "release", "start": "10.10.0.200", "end": "10.10.0.209",
	}, http.StatusOK)

	// Subnet delete blocked while allocations exist.
	do(http.MethodDelete, "/api/v1/subnets/"+subnetID, nil, http.StatusConflict)

	// Global IP list works.
	ips := do(http.MethodGet, "/api/v1/ip-addresses?q=10.10.0.10", nil, http.StatusOK)
	if int(ips["total"].(float64)) < 1 {
		t.Fatal("global ip search should find 10.10.0.10")
	}

	// Second subnet in a different context may overlap (VRF-style separation).
	ctx2 := do(http.MethodPost, "/api/v1/network-contexts", map[string]any{
		"name": "Lab VRF", "description": "isolated lab",
	}, http.StatusCreated)
	labContext := ctx2["id"].(string)
	do(http.MethodPost, "/api/v1/subnets", map[string]any{
		"cidr": "10.10.0.0/24", "name": "Lab mirror", "context_id": labContext,
	}, http.StatusCreated)

	// Subnet delete works after cleanup.
	do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses/bulk", map[string]any{
		"action": "release", "start": "10.10.0.1", "end": "10.10.0.254",
	}, http.StatusOK)
	do(http.MethodDelete, "/api/v1/ip-addresses/"+ipID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/subnets/"+subnetID, nil, http.StatusOK)
}

// testDatabaseURL reads the integration DB URL, skipping the test when absent.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := testDBURLFromEnv
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	return url
}

// newTestServer boots the API against the given database and returns a helper
// for JSON requests with automatic CSRF handling.
func newTestServer(t *testing.T, databaseURL string) (*Server, string, *http.Client, func(string, string, any, int) map[string]any) {
	t.Helper()
	return newTestServerCfg(t, databaseURL, "")
}

func newTestServerCfg(t *testing.T, databaseURL, secretKey string) (*Server, string, *http.Client, func(string, string, any, int) map[string]any) {
	t.Helper()
	if err := db.Migrate(databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	truncateAll(t, pool)

	cfg := config.Config{Env: "production", CookieSecure: false, SessionTTL: time.Hour, SecretKey: secretKey}
	logger := testLogger()
	var box *secrets.Box
	if secretKey != "" {
		box, _ = secrets.NewBox(secretKey)
	}
	authSvc := auth.NewService(store.NewUsers(pool), store.NewSessions(pool), store.NewAuditEvents(pool), logger, cfg.SessionTTL, box)
	srv := NewServer(cfg, logger, pool, authSvc)
	ts := newHTTPTestServer(t, srv.Routes())
	activeTestServerURL = ts.URL
	t.Cleanup(func() { activeTestServerURL = "" })

	jar, _ := newCookieJar()
	client := &http.Client{Jar: jar}

	do := func(method, path string, payload any, wantStatus int) map[string]any {
		t.Helper()
		return performJSON(t, client, ts.URL, method, path, payload, wantStatus)
	}
	return srv, ts.URL, client, do
}
