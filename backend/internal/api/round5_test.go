package api

import (
	"net/http"
	"testing"
)

// newSessionClient opens an independent authenticated session for one account
// and returns a do-helper bound to it (its own cookie jar + CSRF handling).
func newSessionClient(t *testing.T, baseURL, username, password string) (*http.Client, func(string, string, any, int) map[string]any) {
	t.Helper()
	jar, err := newCookieJar()
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	performJSON(t, client, baseURL, http.MethodPost, "/api/v1/auth/login",
		map[string]any{"username": username, "password": password}, http.StatusOK)
	do := func(method, path string, payload any, wantStatus int) map[string]any {
		return performJSON(t, client, baseURL, method, path, payload, wantStatus)
	}
	return client, do
}

// TestUserManagement covers the account lifecycle and role enforcement:
// creation, viewer read-only enforcement, operator inventory-but-not-admin
// enforcement, and the self-delete / last-admin guards.
func TestUserManagement(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}
	srv, baseURL, _, do := newTestServer(t, databaseURL)
	bootAdmin(t, do)
	adminID := currentUserID(t, srv)

	// Create accounts (invalid role and duplicate username rejected).
	do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "viewer1", "password": "password-123", "role": "viewer",
	}, http.StatusCreated)
	do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "operator1", "password": "password-123", "role": "operator",
	}, http.StatusCreated)
	do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "bad", "password": "password-123", "role": "root",
	}, http.StatusUnprocessableEntity)
	do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "viewer1", "password": "password-123", "role": "viewer",
	}, http.StatusConflict)

	// Viewer: reads fine, every mutation refused, account API hidden.
	_, viewerDo := newSessionClient(t, baseURL, "viewer1", "password-123")
	viewerDo(http.MethodGet, "/api/v1/assets", nil, http.StatusOK)
	viewerDo(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "nope", "type_id": "x",
	}, http.StatusForbidden)
	viewerDo(http.MethodGet, "/api/v1/users", nil, http.StatusForbidden)
	viewerDo(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"instance.name": "nope"},
	}, http.StatusForbidden)

	// Operator: inventory mutations allowed; accounts/settings/tokens refused.
	typeID := ""
	types := do(http.MethodGet, "/api/v1/asset-types", nil, http.StatusOK)
	for _, raw := range types["types"].([]any) {
		typ := raw.(map[string]any)
		if typ["slug"] == "server" {
			typeID = typ["id"].(string)
		}
	}
	_, operatorDo := newSessionClient(t, baseURL, "operator1", "password-123")
	created := operatorDo(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "operator-asset", "type_id": typeID, "status": "online",
	}, http.StatusCreated)
	assetID := created["asset"].(map[string]any)["id"].(string)
	operatorDo(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "nope", "password": "password-123", "role": "admin",
	}, http.StatusForbidden)
	operatorDo(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"instance.name": "nope"},
	}, http.StatusForbidden)
	operatorDo(http.MethodGet, "/api/v1/tokens", nil, http.StatusForbidden)
	operatorDo(http.MethodPost, "/api/v1/tokens", map[string]any{
		"name": "nope", "purpose": "api",
	}, http.StatusForbidden)

	// Admin: list, promote/demote, password reset.
	users := do(http.MethodGet, "/api/v1/users", nil, http.StatusOK)
	if len(users["users"].([]any)) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(users["users"].([]any)))
	}
	viewerID := ""
	for _, raw := range users["users"].([]any) {
		u := raw.(map[string]any)
		if u["username"] == "viewer1" {
			viewerID = u["id"].(string)
		}
	}
	do(http.MethodPut, "/api/v1/users/"+viewerID, map[string]any{
		"display_name": "Viewer One", "role": "admin",
	}, http.StatusOK)
	do(http.MethodPut, "/api/v1/users/"+viewerID, map[string]any{
		"role": "viewer",
	}, http.StatusOK) // two admins exist at this point, demote is fine
	do(http.MethodPut, "/api/v1/users/"+viewerID, map[string]any{
		"password": "password-456",
	}, http.StatusOK)

	// Guards: self-demotion and self-deletion are refused; the last admin
	// cannot be demoted or deleted.
	do(http.MethodPut, "/api/v1/users/"+adminID, map[string]any{
		"role": "viewer",
	}, http.StatusConflict)
	do(http.MethodDelete, "/api/v1/users/"+adminID, nil, http.StatusUnprocessableEntity)

	// Cleanup.
	do(http.MethodDelete, "/api/v1/users/"+viewerID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/assets/"+assetID, nil, http.StatusOK)
	// operator1 remains for other tests? test DB is truncated per test server
	// boot — delete for symmetry.
	var operatorID string
	for _, raw := range do(http.MethodGet, "/api/v1/users", nil, http.StatusOK)["users"].([]any) {
		u := raw.(map[string]any)
		if u["username"] == "operator1" {
			operatorID = u["id"].(string)
		}
	}
	do(http.MethodDelete, "/api/v1/users/"+operatorID, nil, http.StatusOK)
}

// TestAssetCheckNow covers the on-demand ICMP probe: an asset with a tracked
// IP records an offline observation for an unreachable target; an asset with
// neither hostname nor IP is refused with no_target.
func TestAssetCheckNow(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}
	srv, _, _, do := newTestServer(t, databaseURL)
	bootAdmin(t, do)

	typeID := ""
	types := do(http.MethodGet, "/api/v1/asset-types", nil, http.StatusOK)
	for _, raw := range types["types"].([]any) {
		typ := raw.(map[string]any)
		if typ["slug"] == "server" {
			typeID = typ["id"].(string)
		}
	}
	host := do(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "check-target", "type_id": typeID, "status": "online",
	}, http.StatusCreated)
	assetID := host["asset"].(map[string]any)["id"].(string)

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
		"cidr": "10.199.0.0/24", "name": "check-lab", "context_id": contextID,
	}, http.StatusCreated)
	subnetID := subnet["subnet"].(map[string]any)["id"].(string)
	iface := do(http.MethodPost, "/api/v1/assets/"+assetID+"/interfaces", map[string]any{
		"name": "lan", "kind": "ethernet",
	}, http.StatusCreated)
	ifaceID := iface["id"].(string)
	do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses", map[string]any{
		"address": "10.199.0.10", "status": "assigned", "interface_id": ifaceID,
		"asset_id": assetID,
	}, http.StatusCreated)

	// The probe runs synchronously and records the observation.
	result := do(http.MethodPost, "/api/v1/assets/"+assetID+"/check", nil, http.StatusOK)
	if result["target"] != "10.199.0.10" {
		t.Fatalf("check target = %v, want the tracked address", result["target"])
	}
	if result["reachable"] != false {
		t.Fatalf("TEST-NET-1 address should not be reachable, got %v", result["reachable"])
	}
	var state string
	err := srv.pool.QueryRow(t.Context(), `
		SELECT metadata->'observed'->'icmp'->>'status' FROM assets WHERE id = $1`, assetID).Scan(&state)
	if err != nil {
		t.Fatalf("load observation: %v", err)
	}
	if state != "offline" {
		t.Fatalf("observed icmp status = %q, want offline", state)
	}

	// An asset with neither hostname nor IP has nothing to check.
	bare := do(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "no-target", "type_id": typeID, "status": "unknown",
	}, http.StatusCreated)
	bareID := bare["asset"].(map[string]any)["id"].(string)
	do(http.MethodPost, "/api/v1/assets/"+bareID+"/check", nil, http.StatusUnprocessableEntity)

	do(http.MethodDelete, "/api/v1/assets/"+assetID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/assets/"+bareID, nil, http.StatusOK)
}
