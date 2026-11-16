package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIntegrationsWorkflow covers Phase 6: encrypted secrets, Proxmox sync
// against a fake API, Docker sync against a fake Engine API, source-managed
// field updates (manual fields preserved), observed-vs-declared status, and
// sync run history.
func TestIntegrationsWorkflow(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}

	// Fake Proxmox API: 1 node, 1 VM, 1 LXC.
	proxmox := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api2/json/nodes":
			_, _ = w.Write([]byte(`{"data":[{"node":"pve-node1","status":"online"}]}`))
		case "/api2/json/nodes/pve-node1/qemu":
			_, _ = w.Write([]byte(`{"data":[{"vmid":101,"name":"vm-home","status":"running","maxmem":4294967296,"maxcpu":2,"template":0}]}`))
		case "/api2/json/nodes/pve-node1/lxc":
			_, _ = w.Write([]byte(`{"data":[{"vmid":201,"name":"ct-pihole","status":"running","maxmem":536870912,"maxcpu":1}]}`))
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer proxmox.Close()

	// Fake Docker Engine API: 2 containers.
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"Id":"abc123def456","Names":["/grav"],"Image":"grav:latest","State":"running"},
			{"Id":"999888777666","Names":["/uptime-kuma"],"Image":"kuma:1","State":"exited"}
		]`))
	}))
	defer docker.Close()

	srv, _, client, do := newTestServer(t, databaseURL)
	_ = srv
	_ = client

	do(http.MethodPost, "/api/v1/setup/admin", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusCreated)

	// Server has no LV_SECRET_KEY in unit tests; secret storage must be refused
	// rather than silently storing plaintext.
	createResp := performJSONExpectError(t, client, tsURL(t), http.MethodPost, "/api/v1/integrations", map[string]any{
		"kind": "proxmox", "name": "pve", "enabled": true,
		"config": map[string]any{"endpoint": proxmox.URL, "token_id": "root!sync", "verify_tls": false},
		"secrets": map[string]any{"token_secret": "super-secret-value"},
	})
	if createResp["error"].(map[string]any)["code"] != "secrets_unavailable" {
		t.Fatalf("expected secrets_unavailable without LV_SECRET_KEY, got %v", createResp)
	}

	// Non-secret creation works; sync reports the missing secret.
	integ := do(http.MethodPost, "/api/v1/integrations", map[string]any{
		"kind": "proxmox", "name": "pve", "enabled": true,
		"config": map[string]any{"endpoint": proxmox.URL, "token_id": "root!sync", "verify_tls": false},
	}, http.StatusCreated)
	integID := integ["id"].(string)

	// Secrets are never echoed back.
	configOut := integ["config"].(map[string]any)
	if _, present := configOut["secrets"]; present {
		t.Fatal("secrets must never be returned by the API")
	}

	do(http.MethodPost, "/api/v1/integrations/"+integID+"/sync", nil, http.StatusUnprocessableEntity) // no secret

	// Provision a LV_SECRET_KEY-enabled server for the rest of the flow.
	srv2, _, client2, do2 := newTestServerWithSecrets(t, databaseURL)
	_ = srv2
	_ = client2

	do2(http.MethodPost, "/api/v1/setup/admin", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusCreated)

	integ2 := do2(http.MethodPost, "/api/v1/integrations", map[string]any{
		"kind": "proxmox", "name": "pve", "enabled": true,
		"config": map[string]any{"endpoint": proxmox.URL, "token_id": "root!sync", "verify_tls": false},
		"secrets": map[string]any{"token_secret": "super-secret-value"},
	}, http.StatusCreated)
	integID2 := integ2["id"].(string)

	// Sync creates node + guests.
	sync := do2(http.MethodPost, "/api/v1/integrations/"+integID2+"/sync", nil, http.StatusOK)
	summary := sync["summary"].(map[string]any)
	if int(summary["nodes"].(float64)) != 1 || int(summary["created"].(float64)) != 3 {
		t.Fatalf("proxmox sync summary wrong: %+v", summary)
	}

	// Imported assets carry source attribution; declared status stays unknown.
	list := do2(http.MethodGet, "/api/v1/assets?q=vm-home", nil, http.StatusOK)
	assets := list["assets"].([]any)
	if len(assets) != 1 {
		t.Fatal("imported VM missing")
	}
	vm := assets[0].(map[string]any)
	if vm["source"] != "proxmox" {
		t.Fatalf("source attribution missing: %v", vm["source"])
	}
	if vm["status"] != "unknown" {
		t.Fatalf("declared status must stay unknown, got %v", vm["status"])
	}
	if int(vm["cpu_cores"].(float64)) != 2 {
		t.Fatal("source-managed cpu_cores not imported")
	}

	// Guest is parented to the node.
	children := do2(http.MethodGet, "/api/v1/assets?parent_id="+vm["parent_id"].(string), nil, http.StatusOK)
	if int(children["total"].(float64)) != 2 {
		t.Fatal("node should host 2 guests")
	}

	// Re-sync updates without duplicating; manual notes survive.
	vmID := vm["id"].(string)
	do2(http.MethodPut, "/api/v1/assets/"+vmID, map[string]any{
		"name": "vm-home", "type_id": vm["type_id"], "notes": "my home automation",
	}, http.StatusOK)
	do2(http.MethodPost, "/api/v1/integrations/"+integID2+"/sync", nil, http.StatusOK)
	after := do2(http.MethodGet, "/api/v1/assets/"+vmID, nil, http.StatusOK)
	asset := after["asset"].(map[string]any)
	if asset["notes"] != "my home automation" {
		t.Fatal("manual notes were overwritten by sync")
	}
	if int(after["asset"].(map[string]any)["cpu_cores"].(float64)) != 2 {
		t.Fatal("source-managed fields lost on re-sync")
	}

	// Docker sync onto a host asset.
	types := do2(http.MethodGet, "/api/v1/asset-types", nil, http.StatusOK)
	var serverTypeID string
	for _, raw := range types["types"].([]any) {
		typ := raw.(map[string]any)
		if typ["slug"] == "server" {
			serverTypeID = typ["id"].(string)
		}
	}
	host := do2(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "docker-host", "type_id": serverTypeID, "status": "online",
	}, http.StatusCreated)
	hostID := host["asset"].(map[string]any)["id"].(string)

	dockerInteg := do2(http.MethodPost, "/api/v1/integrations", map[string]any{
		"kind": "docker", "name": "docker-host", "enabled": true,
		"config": map[string]any{"host": "tcp://" + strings.TrimPrefix(docker.URL, "http://"), "asset_id": hostID},
	}, http.StatusCreated)
	dockerID := dockerInteg["id"].(string)

	dsync := do2(http.MethodPost, "/api/v1/integrations/"+dockerID+"/sync", nil, http.StatusOK)
	if int(dsync["summary"].(map[string]any)["created"].(float64)) != 2 {
		t.Fatalf("docker sync should create 2 containers: %+v", dsync["summary"])
	}

	// Disabled integrations refuse syncs (opt-in enforcement).
	do2(http.MethodPut, "/api/v1/integrations/"+integID2, map[string]any{
		"name": "pve", "description": "", "enabled": false,
		"config": map[string]any{"endpoint": proxmox.URL, "token_id": "root!sync", "verify_tls": false},
	}, http.StatusOK)
	do2(http.MethodPost, "/api/v1/integrations/"+integID2+"/sync", nil, http.StatusUnprocessableEntity)
	do2(http.MethodPut, "/api/v1/integrations/"+integID2, map[string]any{
		"name": "pve", "description": "", "enabled": true,
		"config": map[string]any{"endpoint": proxmox.URL, "token_id": "root!sync", "verify_tls": false},
	}, http.StatusOK)

	// Sync run history.
	runs := do2(http.MethodGet, "/api/v1/integrations/"+integID2+"/sync-runs", nil, http.StatusOK)
	if len(runs["runs"].([]any)) != 2 {
		t.Fatalf("expected 2 proxmox sync runs, got %d", len(runs["runs"].([]any)))
	}
	firstRun := runs["runs"].([]any)[0].(map[string]any)
	if firstRun["status"] != "success" {
		t.Fatalf("latest run should be success, got %v", firstRun["status"])
	}

	// Secrets redacted on list.
	listResp := do2(http.MethodGet, "/api/v1/integrations", nil, http.StatusOK)
	for _, raw := range listResp["integrations"].([]any) {
		i := raw.(map[string]any)
		cfg := i["config"].(map[string]any)
		if _, present := cfg["secrets"]; present {
			t.Fatal("list must redact secrets")
		}
		if i["kind"] == "proxmox" {
			configured := cfg["secrets_configured"].(map[string]any)
			if configured["token_secret"] != true {
				t.Fatal("secrets_configured should report token_secret=true")
			}
		}
	}

	// Sync runs are audited.
	activity := do2(http.MethodGet, "/api/v1/activity?entity_type=integration", nil, http.StatusOK)
	if int(activity["total"].(float64)) < 1 {
		t.Fatal("integration syncs should be audited")
	}
}

var _ = json.Marshal
