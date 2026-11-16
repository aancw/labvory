package api

import (
	"net/http"
	"testing"
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
