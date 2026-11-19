package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aancw/labvory/internal/auth"
)

// TestEndpointMaintenance covers the planned maintenance window: declaring a
// future window, rejecting past/garbage values, clearing it, and the
// maintenance flag surfacing on the service detail endpoint list.
func TestEndpointMaintenance(t *testing.T) {
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

	svc := do(http.MethodPost, "/api/v1/services", map[string]any{
		"name": "Jellyfin", "category": "media", "deployment_type": "lxc",
	}, http.StatusCreated)
	serviceID := svc["service"].(map[string]any)["id"].(string)
	ep := do(http.MethodPost, "/api/v1/services/"+serviceID+"/endpoints", map[string]any{
		"label": "Web UI", "protocol": "http", "port": 8096, "url": "http://jellyfin.lab:8096",
	}, http.StatusCreated)
	epID := ep["id"].(string)

	// Garbage and past timestamps are refused.
	do(http.MethodPost, "/api/v1/service-endpoints/"+epID+"/maintenance", map[string]any{
		"until": "next tuesday",
	}, http.StatusUnprocessableEntity)
	do(http.MethodPost, "/api/v1/service-endpoints/"+epID+"/maintenance", map[string]any{
		"until": time.Now().Add(-time.Hour).Format(time.RFC3339),
	}, http.StatusUnprocessableEntity)

	// A future window sticks and shows up in the service detail payload.
	inOneHour := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	do(http.MethodPost, "/api/v1/service-endpoints/"+epID+"/maintenance", map[string]any{
		"until": inOneHour,
	}, http.StatusOK)
	detail := do(http.MethodGet, "/api/v1/services/"+serviceID, nil, http.StatusOK)
	got := detail["endpoints"].([]any)[0].(map[string]any)["maintenance_until"]
	if got == nil {
		t.Fatal("maintenance_until should be set while the window is active")
	}
	if _, err := time.Parse(time.RFC3339, got.(string)); err != nil {
		t.Fatalf("maintenance_until not RFC3339: %v", err)
	}

	// Unknown endpoints keep their 404.
	do(http.MethodPost, "/api/v1/service-endpoints/00000000-0000-0000-0000-000000000000/maintenance",
		map[string]any{"until": inOneHour}, http.StatusNotFound)

	// Clearing (null until) removes the window.
	do(http.MethodPost, "/api/v1/service-endpoints/"+epID+"/maintenance", map[string]any{
		"until": nil,
	}, http.StatusOK)
	detail = do(http.MethodGet, "/api/v1/services/"+serviceID, nil, http.StatusOK)
	if detail["endpoints"].([]any)[0].(map[string]any)["maintenance_until"] != nil {
		t.Fatal("maintenance_until should be cleared")
	}

	// Every step is audited for the Activity page.
	events := do(http.MethodGet, "/api/v1/activity?limit=100", nil, http.StatusOK)
	sawSet, sawClear := false, false
	for _, raw := range events["events"].([]any) {
		action := raw.(map[string]any)["action"].(string)
		if action == "maintenance_set" {
			sawSet = true
		}
		if action == "maintenance_cleared" {
			sawClear = true
		}
	}
	if !sawSet || !sawClear {
		t.Fatalf("expected maintenance_set and maintenance_cleared audits (set=%v clear=%v)", sawSet, sawClear)
	}
}

// TestRequireTwoFAEnforcement covers the instance-wide 2FA requirement: an
// unenrolled account is walled off from the data API (403 totp_required) but
// keeps access to /me, enrollment, and sign-out; enrolling lifts the wall.
// Requires LV_SECRET_KEY so TOTP secrets can be stored.
func TestRequireTwoFAEnforcement(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}
	srv, baseURL, client, do := newTestServerWithSecrets(t, databaseURL)
	_ = srv
	_ = client

	do(http.MethodPost, "/api/v1/setup/admin", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusCreated)
	do(http.MethodPost, "/api/v1/users", map[string]any{
		"username": "viewer2fa", "password": "password-123", "role": "viewer",
	}, http.StatusCreated)

	// Admin enrolls first so it can still manage settings afterwards.
	setup := do(http.MethodPost, "/api/v1/auth/2fa/setup", nil, http.StatusOK)
	code, err := auth.TOTPCode(setup["secret"].(string), time.Now())
	if err != nil {
		t.Fatalf("compute admin code: %v", err)
	}
	do(http.MethodPost, "/api/v1/auth/2fa/enable", map[string]any{"code": code}, http.StatusOK)

	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"security.require_2fa": true},
	}, http.StatusOK)

	// The enforcement flag travels with /me.
	me := do(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
	if me["require_2fa"] != true {
		t.Fatal("/me should report require_2fa while enforced")
	}

	// Unenrolled viewer: data API walled, enrollment endpoints reachable.
	_, viewerDo := newSessionClient(t, baseURL, "viewer2fa", "password-123")
	vme := viewerDo(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
	if vme["require_2fa"] != true {
		t.Fatal("viewer /me should report require_2fa")
	}
	forbidden := viewerDo(http.MethodGet, "/api/v1/assets", nil, http.StatusForbidden)
	if code := forbidden["error"].(map[string]any)["code"]; code != "totp_required" {
		t.Fatalf("expected totp_required, got %v", code)
	}
	viewerDo(http.MethodPost, "/api/v1/auth/2fa/setup", nil, http.StatusOK)

	// Enrollment lifts the wall.
	vsetup := viewerDo(http.MethodPost, "/api/v1/auth/2fa/setup", nil, http.StatusOK)
	vcode, cerr := auth.TOTPCode(vsetup["secret"].(string), time.Now())
	if cerr != nil {
		t.Fatalf("compute viewer code: %v", cerr)
	}
	viewerDo(http.MethodPost, "/api/v1/auth/2fa/enable", map[string]any{"code": vcode}, http.StatusOK)
	viewerDo(http.MethodGet, "/api/v1/assets", nil, http.StatusOK)

	// Turning enforcement off reopens the API for everyone.
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"security.require_2fa": false},
	}, http.StatusOK)
}

// TestBackupSettings covers the backup.* settings validation: booleans,
// integer ranges, and the directory path.
func TestBackupSettings(t *testing.T) {
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

	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"backup.enabled": true, "backup.interval_hours": 24, "backup.retention": 14, "backup.dir": "./backups"},
	}, http.StatusOK)
	// Out-of-range and wrong-typed values are refused.
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"backup.interval_hours": 0},
	}, http.StatusUnprocessableEntity)
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"backup.interval_hours": "daily"},
	}, http.StatusUnprocessableEntity)
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"backup.retention": 0},
	}, http.StatusUnprocessableEntity)
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"backup.enabled": "yes"},
	}, http.StatusUnprocessableEntity)
	do(http.MethodPut, "/api/v1/settings", map[string]any{
		"settings": map[string]any{"backup.dir": ""},
	}, http.StatusOK) // empty falls back to ./backups

	all := do(http.MethodGet, "/api/v1/settings", nil, http.StatusOK)["settings"].(map[string]any)
	if all["backup.enabled"] != true {
		t.Fatal("backup.enabled should persist")
	}
	if all["backup.interval_hours"] != float64(24) {
		t.Fatalf("backup.interval_hours = %v, want 24", all["backup.interval_hours"])
	}
	if !strings.Contains(all["backup.dir"].(string), "backups") {
		t.Fatalf("backup.dir = %v, want ./backups fallback", all["backup.dir"])
	}
}
