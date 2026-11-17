package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aancw/lanvory/internal/store"
)

// TestServiceWorkflow covers Phase 4: service CRUD, endpoints, deployments, alerts.
func TestServiceWorkflow(t *testing.T) {
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

	// Host asset to deploy on.
	typeID := ""
	types := do(http.MethodGet, "/api/v1/asset-types", nil, http.StatusOK)
	for _, raw := range types["types"].([]any) {
		typ := raw.(map[string]any)
		if typ["slug"] == "server" {
			typeID = typ["id"].(string)
		}
	}
	host := do(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "pve-01", "type_id": typeID, "status": "online",
	}, http.StatusCreated)
	hostID := host["asset"].(map[string]any)["id"].(string)

	// Service CRUD.
	svc := do(http.MethodPost, "/api/v1/services", map[string]any{
		"name": "Jellyfin", "category": "media", "deployment_type": "lxc",
		"description": "Media server", "repo_url": "https://github.com/jellyfin/jellyfin",
	}, http.StatusCreated)
	serviceID := svc["service"].(map[string]any)["id"].(string)

	do(http.MethodPost, "/api/v1/services", map[string]any{
		"name": "Jellyfin", "category": "media",
	}, http.StatusConflict)
	do(http.MethodPost, "/api/v1/services", map[string]any{
		"name": "Bad", "category": "not-a-category",
	}, http.StatusUnprocessableEntity)

	// Endpoints.
	ep := do(http.MethodPost, "/api/v1/services/"+serviceID+"/endpoints", map[string]any{
		"label": "Web UI", "protocol": "http", "port": 8096, "url": "http://jellyfin.lab:8096",
	}, http.StatusCreated)
	epID := ep["id"].(string)

	do(http.MethodPost, "/api/v1/services/"+serviceID+"/endpoints", map[string]any{
		"protocol": "http", "port": 999999,
	}, http.StatusUnprocessableEntity)

	// Deployments.
	dep := do(http.MethodPost, "/api/v1/services/"+serviceID+"/deployments", map[string]any{
		"asset_id": hostID, "deployment_type": "lxc",
	}, http.StatusCreated)
	depID := dep["id"].(string)

	do(http.MethodPost, "/api/v1/services/"+serviceID+"/deployments", map[string]any{
		"asset_id": hostID, "deployment_type": "docker",
	}, http.StatusConflict)

	// Detail carries endpoints + deployments.
	detail := do(http.MethodGet, "/api/v1/services/"+serviceID, nil, http.StatusOK)
	if len(detail["endpoints"].([]any)) != 1 || len(detail["deployments"].([]any)) != 1 {
		t.Fatal("service detail should carry 1 endpoint and 1 deployment")
	}
	if detail["deployments"].([]any)[0].(map[string]any)["asset_name"] != "pve-01" {
		t.Fatal("deployment should reference the host by name")
	}

	// Service list shows host count.
	list := do(http.MethodGet, "/api/v1/services", nil, http.StatusOK)
	first := list["services"].([]any)[0].(map[string]any)
	if int(first["host_count"].(float64)) != 1 {
		t.Fatal("host_count mismatch")
	}

	// Dashboard aggregates.
	dash := do(http.MethodGet, "/api/v1/dashboard", nil, http.StatusOK)
	if dash["services"].(float64) != 1 {
		t.Fatal("dashboard services count mismatch")
	}
	alerts := dash["alerts"].(map[string]any)
	if alerts["services_without_host"].(float64) != 0 {
		t.Fatal("service has a host; alert should be 0")
	}

	// Cleanup.
	do(http.MethodDelete, "/api/v1/service-endpoints/"+epID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/service-deployments/"+depID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/services/"+serviceID, nil, http.StatusOK)
}

// TestEndpointUptimeAndCertAlert covers the endpoint observability additions:
// rolling check history surfaced as uptime percentages in the service detail,
// TLS certificate expiry surfaced as a dashboard alert, and the
// audit.retention_days setting validation.
func TestEndpointUptimeAndCertAlert(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}
	srv, _, client, do := newTestServer(t, databaseURL)
	svcs := store.NewServices(srv.pool)

	do(http.MethodPost, "/api/v1/setup/admin", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusCreated)

	svc := do(http.MethodPost, "/api/v1/services", map[string]any{
		"name": "Uptime Probe Target", "category": "monitoring",
	}, http.StatusCreated)
	serviceID := svc["service"].(map[string]any)["id"].(string)
	ep := do(http.MethodPost, "/api/v1/services/"+serviceID+"/endpoints", map[string]any{
		"label": "Web", "protocol": "https", "url": "https://uptime.lab", "health_enabled": true,
	}, http.StatusCreated)
	epID := ep["id"].(string)

	// Observed history: three up, one down in the last day, plus one up two
	// days ago (outside the 24h window, inside the 7d window).
	for i := 0; i < 3; i++ {
		if err := svcs.RecordEndpointCheck(context.Background(), epID, true, 200, 12, ""); err != nil {
			t.Fatalf("record check: %v", err)
		}
	}
	if err := svcs.RecordEndpointCheck(context.Background(), epID, false, 0, 8, "connection refused"); err != nil {
		t.Fatalf("record check: %v", err)
	}
	tag, err := srv.pool.Exec(context.Background(), `
		INSERT INTO service_endpoint_checks (endpoint_id, up, status, checked_at)
		VALUES ($1, true, 200, now() - interval '2 days')`, epID)
	if err != nil {
		t.Fatalf("seed old check: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatal("old check row was not inserted")
	}

	// Observed TLS certificate expiring soon — should raise the dashboard alert.
	cert := store.CertObservation{NotAfter: time.Now().Add(10 * 24 * time.Hour), Subject: "CN=uptime.lab"}
	if err := svcs.RecordHealthCheck(context.Background(), epID, 200, 15, "", &cert); err != nil {
		t.Fatalf("record health check: %v", err)
	}

	detail := do(http.MethodGet, "/api/v1/services/"+serviceID, nil, http.StatusOK)
	uptime, ok := detail["uptime"].(map[string]any)[epID].(map[string]any)
	if !ok {
		t.Fatal("service detail should carry uptime keyed by endpoint id")
	}
	if got := uptime["up_24h"].(float64); got != 75 {
		t.Fatalf("up_24h = %v, want 75 (3 of 4 recent checks up)", got)
	}
	if got := uptime["up_7d"].(float64); got != 80 {
		t.Fatalf("up_7d = %v, want 80 (4 of 5 checks up)", got)
	}
	checks := uptime["checks"].([]any)
	if len(checks) != 5 {
		t.Fatalf("sparkline samples = %d, want 5", len(checks))
	}
	if checks[0].(map[string]any)["up"] != true {
		t.Fatal("samples should be oldest-first (2-day-old up check first)")
	}

	// The observed certificate shows up on the endpoint and on the dashboard.
	epObj := detail["endpoints"].([]any)[0].(map[string]any)
	if epObj["cert_subject"] != "CN=uptime.lab" {
		t.Fatalf("cert_subject = %v, want CN=uptime.lab", epObj["cert_subject"])
	}
	if epObj["cert_not_after"] == nil {
		t.Fatal("cert_not_after should be observed")
	}
	dash := do(http.MethodGet, "/api/v1/dashboard", nil, http.StatusOK)
	alerts := dash["alerts"].(map[string]any)
	if alerts["certs_expiring"].(float64) < 1 {
		t.Fatal("cert expiring within 30 days should raise the dashboard alert")
	}

	// Retention setting: accepted in range, rejected outside it.
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"audit.retention_days": 90},
	}, http.StatusOK)
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"audit.retention_days": -1},
	}, http.StatusUnprocessableEntity)
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"audit.retention_days": 4000},
	}, http.StatusUnprocessableEntity)
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"audit.retention_days": "ninety"},
	}, http.StatusUnprocessableEntity)
	settings := do(http.MethodGet, "/api/v1/settings", nil, http.StatusOK)
	if settings["settings"].(map[string]any)["audit.retention_days"].(float64) != 90 {
		t.Fatal("audit.retention_days should persist as 90")
	}

	// Cleanup: disable retention and remove the service (cascades endpoint).
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"audit.retention_days": 0},
	}, http.StatusOK)
	_ = client
	do(http.MethodDelete, "/api/v1/services/"+serviceID, nil, http.StatusOK)
}
