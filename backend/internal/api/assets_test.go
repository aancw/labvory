package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/petruknisme/lanvory/internal/auth"
	"github.com/petruknisme/lanvory/internal/config"
	"github.com/petruknisme/lanvory/internal/db"
	"github.com/petruknisme/lanvory/internal/store"
)

// TestAssetLifecycle is the Phase 2 acceptance scenario: locations, tags, a physical
// host with virtual children, relationships, interfaces, filtering, and delete rules.
// Requires TEST_DATABASE_URL (skipped otherwise).
func TestAssetLifecycle(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	if err := db.Migrate(databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	truncateAll(t, pool)

	cfg := config.Config{Env: "production", CookieSecure: false, SessionTTL: time.Hour}
	logger := testLogger()
	authSvc := auth.NewService(store.NewUsers(pool), store.NewSessions(pool), store.NewAuditEvents(pool), logger, cfg.SessionTTL)
	srv := NewServer(cfg, logger, pool, authSvc)
	router := srv.Routes()
	ts := httptest.NewServer(router)
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	csrf := ""
	do := func(method, path string, payload any, wantStatus int) map[string]any {
		t.Helper()
		var body []byte
		if payload != nil {
			body, _ = json.Marshal(payload)
		}
		req, _ := http.NewRequest(method, ts.URL+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if method != http.MethodGet {
			csrf = fetchCSRF(t, client, ts.URL)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != wantStatus {
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(resp.Body)
			t.Fatalf("%s %s: want %d, got %d: %s", method, path, wantStatus, resp.StatusCode, buf.String())
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	// Auth bootstrap: setup admin, then exercise everything as an authenticated client.
	do(http.MethodPost, "/api/v1/setup/admin", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusCreated)

	typeID := func(slug string) string {
		t.Helper()
		out := do(http.MethodGet, "/api/v1/asset-types", nil, http.StatusOK)
		for _, raw := range out["types"].([]any) {
			typ := raw.(map[string]any)
			if typ["slug"] == slug {
				return typ["id"].(string)
			}
		}
		t.Fatalf("asset type %q not found", slug)
		return ""
	}

	// Locations + tags.
	loc := do(http.MethodPost, "/api/v1/locations", map[string]any{
		"name": "Basement Rack", "description": "Main rack",
	}, http.StatusCreated)
	locationID := loc["id"].(string)

	tag := do(http.MethodPost, "/api/v1/tags", map[string]any{"name": "core", "color": "blue"}, http.StatusCreated)
	tagID := tag["id"].(string)

	// Physical host.
	server := do(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "pve-01", "hostname": "pve-01.lab", "type_id": typeID("server"),
		"status": "online", "environment": "production", "location_id": locationID,
		"manufacturer": "Beelink", "model": "SER9", "os_name": "Proxmox VE", "os_version": "9.1",
		"cpu_model": "Ryzen 9 HX 370", "cpu_cores": 12, "ram_gb": 96.0, "storage_gb": 2048.0,
		"tag_ids": []string{tagID},
	}, http.StatusCreated)
	serverID := server["asset"].(map[string]any)["id"].(string)

	// Virtual children.
	vm := do(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "vm-homeassistant", "type_id": typeID("vm"),
		"status": "online", "environment": "production", "parent_id": serverID,
		"cpu_cores": 2, "ram_gb": 4.0,
	}, http.StatusCreated)
	vmID := vm["asset"].(map[string]any)["id"].(string)

	lxc := do(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "lxc-pihole", "type_id": typeID("lxc"),
		"status": "online", "parent_id": serverID,
	}, http.StatusCreated)
	lxcID := lxc["asset"].(map[string]any)["id"].(string)

	// Filtering: children of the server.
	children := do(http.MethodGet, fmt.Sprintf("/api/v1/assets?parent_id=%s", serverID), nil, http.StatusOK)
	if int(children["total"].(float64)) != 2 {
		t.Fatalf("expected 2 children, got %v", children["total"])
	}

	// Filtering: physical only + search by hostname.
	physical := do(http.MethodGet, "/api/v1/assets?category=physical", nil, http.StatusOK)
	if int(physical["total"].(float64)) != 1 {
		t.Fatalf("expected 1 physical asset, got %v", physical["total"])
	}
	found := do(http.MethodGet, "/api/v1/assets?q=pve-01", nil, http.StatusOK)
	if int(found["total"].(float64)) != 1 {
		t.Fatalf("expected search hit for pve-01, got %v", found["total"])
	}

	// Detail: children + tags + activity visible.
	detail := do(http.MethodGet, "/api/v1/assets/"+serverID, nil, http.StatusOK)
	if int(detail["asset"].(map[string]any)["child_count"].(float64)) != 2 {
		t.Fatal("detail child_count mismatch")
	}
	if len(detail["asset"].(map[string]any)["tags"].([]any)) != 1 {
		t.Fatal("expected 1 tag on server")
	}
	if len(detail["activity"].([]any)) < 1 {
		t.Fatal("expected audit activity on detail")
	}

	// Parent protection: deleting the host with guests must be rejected.
	do(http.MethodDelete, "/api/v1/assets/"+serverID, nil, http.StatusConflict)

	// Relationships.
	rel := do(http.MethodPost, "/api/v1/assets/"+vmID+"/relationships", map[string]any{
		"target_asset_id": serverID, "kind": "runs_on",
	}, http.StatusCreated)
	relID := rel["id"].(string)

	// Duplicate rejected.
	do(http.MethodPost, "/api/v1/assets/"+vmID+"/relationships", map[string]any{
		"target_asset_id": serverID, "kind": "runs_on",
	}, http.StatusConflict)

	// Cycle rejected: server runs_on vm would close the loop.
	do(http.MethodPost, "/api/v1/assets/"+serverID+"/relationships", map[string]any{
		"target_asset_id": vmID, "kind": "runs_on",
	}, http.StatusConflict)

	// Invalid kind rejected.
	do(http.MethodPost, "/api/v1/assets/"+vmID+"/relationships", map[string]any{
		"target_asset_id": lxcID, "kind": "is_buddy_of",
	}, http.StatusUnprocessableEntity)

	// Interfaces.
	iface := do(http.MethodPost, "/api/v1/assets/"+vmID+"/interfaces", map[string]any{
		"name": "eth0", "kind": "ethernet", "mac_address": "BC:24:11:AA:BB:CC",
	}, http.StatusCreated)
	ifaceID := iface["id"].(string)

	do(http.MethodPost, "/api/v1/assets/"+vmID+"/interfaces", map[string]any{
		"name": "eth0", "kind": "ethernet",
	}, http.StatusConflict)

	do(http.MethodPost, "/api/v1/assets/"+vmID+"/interfaces", map[string]any{
		"name": "wlan0", "kind": "ethernet", "mac_address": "not-a-mac",
	}, http.StatusUnprocessableEntity)

	// Global interface list.
	list := do(http.MethodGet, "/api/v1/interfaces", nil, http.StatusOK)
	if int(list["total"].(float64)) != 1 {
		t.Fatalf("expected 1 interface globally, got %v", list["total"])
	}

	// Update asset: status change is reflected and audited.
	updated := do(http.MethodPut, "/api/v1/assets/"+lxcID, map[string]any{
		"name": "lxc-pihole", "type_id": typeID("lxc"), "status": "maintenance",
	}, http.StatusOK)
	if updated["asset"].(map[string]any)["status"] != "maintenance" {
		t.Fatal("status update not applied")
	}

	// Bulk set_status.
	bulk := do(http.MethodPost, "/api/v1/assets/bulk", map[string]any{
		"action": "set_status", "ids": []string{vmID, lxcID}, "status": "offline",
	}, http.StatusOK)
	if int(bulk["affected"].(float64)) != 2 {
		t.Fatalf("bulk affected mismatch: %v", bulk["affected"])
	}

	// Delete children, then the host succeeds.
	do(http.MethodDelete, "/api/v1/relationships/"+relID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/interfaces/"+ifaceID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/assets/"+vmID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/assets/"+lxcID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/assets/"+serverID, nil, http.StatusOK)

	// Location with assets would have been rejected; now it is empty and deletable.
	do(http.MethodDelete, "/api/v1/locations/"+locationID, nil, http.StatusOK)
}

func fetchCSRF(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	resp, err := client.Get(base + "/api/v1/csrf")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body.Token
}
