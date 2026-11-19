package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aancw/labvory/internal/auth"
	"github.com/aancw/labvory/internal/store"
)

// TestTwoFactorFlow covers the TOTP lifecycle: enrollment, confirmation,
// second-step login, replay rejection, session listing/revocation, and
// disable. Requires LV_SECRET_KEY (newTestServerWithSecrets) so secrets can
// be stored encrypted.
func TestTwoFactorFlow(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}
	srv, _, client, do := newTestServerWithSecrets(t, databaseURL)
	_ = srv
	_ = client

	do(http.MethodPost, "/api/v1/setup/admin", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusCreated)

	// Me before enrollment: no TOTP.
	me := do(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
	if me["user"].(map[string]any)["totp_enabled_at"] != nil {
		t.Fatal("totp should be disabled before enrollment")
	}

	// Enrollment: setup returns the secret and the otpauth URI.
	setup := do(http.MethodPost, "/api/v1/auth/2fa/setup", nil, http.StatusOK)
	secret := setup["secret"].(string)
	if !strings.Contains(setup["otpauth_uri"].(string), "otpauth://totp/LabVory:admin") {
		t.Fatal("otpauth URI should target the admin account")
	}
	if setup["secret"] == "" {
		t.Fatal("setup must return the plaintext secret once")
	}

	// Confirming with a bad code fails.
	do(http.MethodPost, "/api/v1/auth/2fa/enable", map[string]any{"code": "900900"}, http.StatusUnprocessableEntity)

	code, err := auth.TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatalf("compute code: %v", err)
	}
	do(http.MethodPost, "/api/v1/auth/2fa/enable", map[string]any{"code": code}, http.StatusOK)

	me = do(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
	if me["user"].(map[string]any)["totp_enabled_at"] == nil {
		t.Fatal("totp should be enabled after confirmation")
	}

	// Two active sessions now: the setup session and... only one so far.
	sessions := do(http.MethodGet, "/api/v1/auth/sessions", nil, http.StatusOK)
	if len(sessions["sessions"].([]any)) != 1 {
		t.Fatal("one session expected after setup")
	}
	if sessions["current_id"].(string) == "" {
		t.Fatal("current session must be identified")
	}

	// Sign out; login without a code is refused.
	do(http.MethodPost, "/api/v1/auth/logout", nil, http.StatusOK)
	resp := do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusForbidden)
	if resp["error"].(map[string]any)["code"] != "totp_required" {
		t.Fatal("login without code should be rejected with totp_required")
	}

	// A code from far in the future is valid TOTP but wrong right now.
	future, _ := auth.TOTPCode(secret, time.Now().Add(10*30*time.Second))
	resp = do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "password-123", "totp_code": future,
	}, http.StatusForbidden)
	if resp["error"].(map[string]any)["code"] != "invalid_totp" {
		t.Fatal("wrong code should be rejected with invalid_totp")
	}

	// The code for the next step signs in (the enabling code's step is burned
	// by the replay guard; within one 30s window only S+1 validates and only
	// once).
	code2, _ := auth.TOTPCode(secret, time.Now().Add(30*time.Second))
	do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "password-123", "totp_code": code2,
	}, http.StatusOK)

	// Replaying the same code is refused even though it was valid a second ago.
	do(http.MethodPost, "/api/v1/auth/login", map[string]any{
		"username": "admin", "password": "password-123", "totp_code": code2,
	}, http.StatusForbidden)

	// A second session (inserted directly — another live login would need a
	// fresh TOTP step, and only one validates per 30s window).
	sess := store.Session{
		UserID:    currentUserID(t, srv),
		TokenHash: auth.TokenHash("second-session-token"),
		ExpiresAt: time.Now().Add(time.Hour),
		SourceIP:  "10.0.0.9",
		UserAgent: "test-other-client",
	}
	if err := store.NewSessions(srv.pool).Create(t.Context(), sess); err != nil {
		t.Fatalf("create second session: %v", err)
	}
	sessions = do(http.MethodGet, "/api/v1/auth/sessions", nil, http.StatusOK)
	if len(sessions["sessions"].([]any)) != 2 {
		t.Fatalf("two sessions expected, got %d", len(sessions["sessions"].([]any)))
	}
	revoke := do(http.MethodDelete, "/api/v1/auth/sessions/others", nil, http.StatusOK)
	if revoke["revoked"].(float64) != 1 {
		t.Fatalf("one other session should be revoked, got %v", revoke["revoked"])
	}
	sessions = do(http.MethodGet, "/api/v1/auth/sessions", nil, http.StatusOK)
	if len(sessions["sessions"].([]any)) != 1 {
		t.Fatal("only the current session should remain")
	}

	// Disabling requires password + code; wrong password is refused.
	do(http.MethodPost, "/api/v1/auth/2fa/disable", map[string]any{
		"password": "wrong", "code": freshCode(t, secret),
	}, http.StatusForbidden)
	do(http.MethodPost, "/api/v1/auth/2fa/disable", map[string]any{
		"password": "password-123", "code": freshCode(t, secret),
	}, http.StatusOK)

	// The active session sees the disabled state.
	me = do(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
	if me["user"].(map[string]any)["totp_enabled_at"] != nil {
		t.Fatal("totp should be disabled")
	}
}

// currentUserID resolves the signed-in user's id through the API.
func currentUserID(t *testing.T, srv *Server) string {
	t.Helper()
	// The first admin in a fresh test database is the operator under test.
	rows, err := srv.pool.Query(t.Context(), `SELECT id FROM users ORDER BY created_at LIMIT 1`)
	if err != nil {
		t.Fatalf("query user: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no user found")
	}
	var id string
	if err := rows.Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// freshCode produces a fresh valid code; successive logins each need their own
// because of the replay guard.
func freshCode(t *testing.T, secret string) string {
	t.Helper()
	code, err := auth.TOTPCode(secret, time.Now())
	if err != nil {
		t.Fatalf("compute code: %v", err)
	}
	return code
}

// TestObservedIPConflict covers the ARP mismatch plumbing end to end: an
// allocation with an assigned interface MAC, an observed mismatch recorded
// under metadata.observed, the dashboard alert, and the list filter.
func TestObservedIPConflict(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	if databaseURL == "" {
		return
	}
	srv, _, _, do := newTestServer(t, databaseURL)
	ipsStore := store.NewIPAddresses(srv.pool)

	do(http.MethodPost, "/api/v1/setup/admin", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusCreated)

	// Subnet + host asset with a LAN interface carrying a declared MAC.
	ctxResp := do(http.MethodGet, "/api/v1/network-contexts", nil, http.StatusOK)
	var contextID string
	for _, raw := range ctxResp["contexts"].([]any) {
		c := raw.(map[string]any)
		if c["slug"] == "default" {
			contextID = c["id"].(string)
			break
		}
	}
	if contextID == "" {
		contextID = ctxResp["contexts"].([]any)[0].(map[string]any)["id"].(string)
	}
	subnet := do(http.MethodPost, "/api/v1/subnets", map[string]any{
		"cidr": "10.99.0.0/24", "name": "mismatch-lab", "context_id": contextID,
	}, http.StatusCreated)
	subnetID := subnet["subnet"].(map[string]any)["id"].(string)

	typeID := ""
	types := do(http.MethodGet, "/api/v1/asset-types", nil, http.StatusOK)
	for _, raw := range types["types"].([]any) {
		typ := raw.(map[string]any)
		if typ["slug"] == "server" {
			typeID = typ["id"].(string)
		}
	}
	host := do(http.MethodPost, "/api/v1/assets", map[string]any{
		"name": "server-01", "type_id": typeID, "status": "online",
	}, http.StatusCreated)
	assetID := host["asset"].(map[string]any)["id"].(string)
	iface := do(http.MethodPost, "/api/v1/assets/"+assetID+"/interfaces", map[string]any{
		"name": "lan", "kind": "ethernet", "mac_address": "AA:BB:CC:DD:EE:01",
	}, http.StatusCreated)
	ifaceID := iface["id"].(string)

	// Allocate the address to that interface.
	alloc := do(http.MethodPost, "/api/v1/subnets/"+subnetID+"/addresses", map[string]any{
		"address": "10.99.0.10", "status": "assigned", "interface_id": ifaceID, "asset_id": assetID,
	}, http.StatusCreated)
	ipID := alloc["id"].(string)

	// No observation yet: no alert.
	dash := do(http.MethodGet, "/api/v1/dashboard", nil, http.StatusOK)
	if dash["alerts"].(map[string]any)["observed_ip_conflicts"].(float64) != 0 {
		t.Fatal("no mismatch recorded yet, alert should be 0")
	}

	// The ARP sweep observes a different MAC answering at this address.
	err := ipsStore.SetObservedSub(t.Context(), ipID, "arp", map[string]any{
		"mac": "aa:bb:cc:dd:ee:99", "interface_mac": "AA:BB:CC:DD:EE:01",
		"matches": false, "checked_at": time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}

	dash = do(http.MethodGet, "/api/v1/dashboard", nil, http.StatusOK)
	if dash["alerts"].(map[string]any)["observed_ip_conflicts"].(float64) != 1 {
		t.Fatal("dashboard should report one observed MAC mismatch")
	}

	list := do(http.MethodGet, "/api/v1/ip-addresses?observed_conflict=true", nil, http.StatusOK)
	rows := list["addresses"].([]any)
	if len(rows) != 1 {
		t.Fatalf("filter should return the mismatching row, got %d", len(rows))
	}
	row := rows[0].(map[string]any)
	if row["address"].(string) != "10.99.0.10" || row["observed_conflict"] != true {
		t.Fatal("row should carry the observed_conflict flag")
	}

	// Plain list also flags the row.
	list = do(http.MethodGet, "/api/v1/ip-addresses", nil, http.StatusOK)
	first := list["addresses"].([]any)[0].(map[string]any)
	if first["observed_conflict"] != true {
		t.Fatal("unfiltered list should flag the mismatching row")
	}

	// Cleanup.
	do(http.MethodDelete, "/api/v1/assets/"+assetID, nil, http.StatusOK)
}
