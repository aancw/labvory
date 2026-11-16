package api

import (
	"net/http"
	"testing"
)

// TestTopologyAndPortability covers Phase 5: topology graph, relationship
// metadata, CSV import preview/commit, JSON export and idempotent restore.
func TestTopologyAndPortability(t *testing.T) {
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

	typeID := ""
	types := do(http.MethodGet, "/api/v1/asset-types", nil, http.StatusOK)
	for _, raw := range types["types"].([]any) {
		typ := raw.(map[string]any)
		if typ["slug"] == "server" {
			typeID = typ["id"].(string)
		}
	}

	host := do(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "pve-01", "type_id": typeID, "status": "online", "serial_number": "SN-001",
	}, http.StatusCreated)
	hostID := host["asset"].(map[string]any)["id"].(string)

	vm := do(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "vm-web", "type_id": typeID, "parent_id": hostID,
	}, http.StatusCreated)
	vmID := vm["asset"].(map[string]any)["id"].(string)

	// Relationship with connection metadata.
	rel := do(http.MethodPost, "/api/v1/assets/"+vmID+"/relationships", map[string]any{
		"target_asset_id": hostID, "kind": "connected_to",
		"source_port": "eth0", "target_port": "port3",
	}, http.StatusCreated)
	relID := rel["id"].(string)

	// PATCH metadata.
	do(http.MethodPatch, "/api/v1/relationships/"+relID, map[string]any{
		"kind": "connected_to", "note": "uplink", "source_port": "enp1s0", "target_port": "port7",
	}, http.StatusOK)

	// Topology graph: nodes + hosts edge + relationship edge with ports.
	graph := do(http.MethodGet, "/api/v1/topology", nil, http.StatusOK)
	if len(graph["nodes"].([]any)) != 2 {
		t.Fatalf("expected 2 topology nodes, got %d", len(graph["nodes"].([]any)))
	}
	var hasHosts, hasConnected bool
	for _, raw := range graph["edges"].([]any) {
		e := raw.(map[string]any)
		if e["kind"] == "hosts" {
			hasHosts = true
		}
		if e["kind"] == "connected_to" && e["source_port"] == "enp1s0" && e["target_port"] == "port7" {
			hasConnected = true
		}
	}
	if !hasHosts || !hasConnected {
		t.Fatalf("topology edges incomplete: hosts=%v connected_with_ports=%v", hasHosts, hasConnected)
	}

	// CSV import: preview shows ok + duplicate + error rows.
	preview := do(http.MethodPost, "/api/v1/import/preview", map[string]any{
		"entity": "assets",
		"rows": []map[string]any{
			{"name": "sw-core", "type_slug": "switch", "manufacturer": "Mikrotik"},
			{"name": "pve-01", "type_slug": "server", "serial_number": "SN-001"},
			{"name": "", "type_slug": "switch"},
		},
	}, http.StatusOK)
	statuses := []string{}
	for _, raw := range preview["rows"].([]any) {
		statuses = append(statuses, raw.(map[string]any)["status"].(string))
	}
	if statuses[0] != "ok" || statuses[1] != "duplicate" || statuses[2] != "error" {
		t.Fatalf("preview statuses wrong: %v", statuses)
	}

	// Commit skips duplicates, creates the new one.
	commit := do(http.MethodPost, "/api/v1/import/commit", map[string]any{
		"entity": "assets", "skip_duplicates": true,
		"rows": []map[string]any{
			{"name": "sw-core", "type_slug": "switch", "manufacturer": "Mikrotik"},
			{"name": "pve-01", "type_slug": "server", "serial_number": "SN-001"},
		},
	}, http.StatusOK)
	if int(commit["created"].(float64)) != 1 || int(commit["skipped"].(float64)) != 1 {
		t.Fatalf("commit summary wrong: %+v", commit)
	}

	// Export inventory JSON.
	exported := do(http.MethodGet, "/api/v1/export/inventory.json", nil, http.StatusOK)
	if len(exported["assets"].([]any)) != 3 {
		t.Fatalf("export should contain 3 assets, got %d", len(exported["assets"].([]any)))
	}

	// Restore is idempotent: re-importing the same export creates nothing.
	restore := do(http.MethodPost, "/api/v1/import/restore", map[string]any{
		"inventory": exported,
	}, http.StatusOK)
	created := restore["created"].(map[string]any)
	if created["assets"].(float64) != 0 {
		t.Fatalf("restore should skip existing assets, created %v", created["assets"])
	}

	// Wipe assets, then restore recreates them with parent links intact.
	do(http.MethodDelete, "/api/v1/assets/"+vmID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/assets/"+hostID, nil, http.StatusOK)
	// sw-core has no children; delete it too by listing.
	list := do(http.MethodGet, "/api/v1/assets?q=sw-core", nil, http.StatusOK)
	for _, raw := range list["assets"].([]any) {
		a := raw.(map[string]any)
		do(http.MethodDelete, "/api/v1/assets/"+a["id"].(string), nil, http.StatusOK)
	}

	restore2 := do(http.MethodPost, "/api/v1/import/restore", map[string]any{
		"inventory": exported,
	}, http.StatusOK)
	if restore2["created"].(map[string]any)["assets"].(float64) != 3 {
		t.Fatalf("restore should recreate 3 assets, got %v", restore2["created"].(map[string]any)["assets"])
	}

	// Parent-child link survived the restore cycle.
	children := do(http.MethodGet, "/api/v1/assets?q=vm-web", nil, http.StatusOK)
	if int(children["total"].(float64)) != 1 {
		t.Fatal("restored child asset missing")
	}
	child := children["assets"].([]any)[0].(map[string]any)
	if child["parent_name"] != "pve-01" {
		t.Fatalf("restored child should be parented to pve-01, got %v", child["parent_name"])
	}
}
