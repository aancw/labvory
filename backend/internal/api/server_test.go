package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aancw/lanvory/internal/auth"
	"github.com/aancw/lanvory/internal/config"
	"github.com/aancw/lanvory/internal/db"
	"github.com/aancw/lanvory/internal/store"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestAPIWorkflow is an end-to-end integration test. It requires PostgreSQL and is skipped
// unless TEST_DATABASE_URL is set (see docs/DEVELOPMENT.md):
//
//	TEST_DATABASE_URL=postgres://... go test ./internal/api/...
func TestAPIWorkflow(t *testing.T) {
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
	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	csrfToken := ""
	getCSRF := func() string {
		t.Helper()
		resp, err := client.Get(ts.URL + "/api/v1/csrf")
		if err != nil {
			t.Fatalf("csrf fetch: %v", err)
		}
		defer resp.Body.Close()
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Token == "" {
			t.Fatal("empty csrf token")
		}
		return body.Token
	}
	post := func(path string, payload any, wantStatus int) map[string]any {
		t.Helper()
		csrfToken = getCSRF()
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("post %s: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != wantStatus {
			t.Fatalf("post %s: want %d, got %d", path, wantStatus, resp.StatusCode)
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	// 1. Fresh instance needs setup.
	resp, err := client.Get(ts.URL + "/api/v1/setup/status")
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !status.NeedsSetup {
		t.Fatal("fresh instance must need setup")
	}

	// 2. CSRF enforcement: login without header is rejected.
	b, _ := json.Marshal(loginRequest{Username: "admin", Password: "password-123"})
	reqNoCSRF, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/auth/login", bytes.NewReader(b))
	reqNoCSRF.Header.Set("Content-Type", "application/json")
	respNoCSRF, err := client.Do(reqNoCSRF)
	if err != nil {
		t.Fatal(err)
	}
	respNoCSRF.Body.Close()
	if respNoCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("login without CSRF must be 403, got %d", respNoCSRF.StatusCode)
	}

	// 3. Create the first admin (weak password rejected).
	post("/api/v1/setup/admin", createAdminRequest{Username: "admin", Password: "short"}, http.StatusUnprocessableEntity)
	post("/api/v1/setup/admin", createAdminRequest{Username: "admin", DisplayName: "Operator", Password: "password-123"}, http.StatusCreated)

	// 4. Setup closed afterwards.
	post("/api/v1/setup/admin", createAdminRequest{Username: "second", Password: "password-123"}, http.StatusForbidden)

	// 5. Who am I?
	resp, err = client.Get(ts.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	var me struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()
	if me.User.Username != "admin" {
		t.Fatalf("me: want admin, got %q", me.User.Username)
	}

	// 6. Settings round trip.
	csrfToken = getCSRF()
	putReq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/settings",
		bytes.NewReader([]byte(`{"settings":{"instance.name":"Homelab HQ"}}`)))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("X-CSRF-Token", csrfToken)
	resp, err = client.Do(putReq)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings update: got %d", resp.StatusCode)
	}

	// 7. Dashboard returns zeroed but real stats.
	resp, err = client.Get(ts.URL + "/api/v1/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	var dash map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&dash); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	assets := dash["assets"].(map[string]any)
	if assets["total"].(float64) != 0 {
		t.Fatalf("expected 0 assets, got %v", assets["total"])
	}

	// 8. Activity shows the audit trail (user.created, login.success ...).
	resp, err = client.Get(ts.URL + "/api/v1/activity")
	if err != nil {
		t.Fatal(err)
	}
	var activity struct {
		Events []map[string]any `json:"events"`
		Total  float64          `json:"total"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&activity)
	resp.Body.Close()
	if activity.Total < 2 {
		t.Fatalf("expected audit events, got %v", activity.Total)
	}

	// 9. Logout revokes the session.
	post("/api/v1/auth/logout", nil, http.StatusOK)
	resp, err = client.Get(ts.URL + "/api/v1/auth/me")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout must be 401, got %d", resp.StatusCode)
	}

	// 10. Wrong password rejected; correct one accepted.
	post("/api/v1/auth/login", loginRequest{Username: "admin", Password: "wrong-password"}, http.StatusUnauthorized)
	post("/api/v1/auth/login", loginRequest{Username: "admin", Password: "password-123"}, http.StatusOK)
}

func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(t.Context(), `
		TRUNCATE audit_events, sessions, users, app_settings, entity_tags, tags,
		         service_deployments, service_endpoints, services, ip_assignments,
		         ip_addresses, subnets, vlan_definitions, network_contexts,
		         asset_relationships, network_interfaces, assets, asset_types, locations,
		         integration_sync_runs, integrations, subdomains, domains, api_tokens CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
	// Restore reference data expected by the app (mirrors migration 0002_defaults).
	_, err = pool.Exec(t.Context(), `
		INSERT INTO asset_types (slug, label, category, icon) VALUES
			('server','Server','physical','server'),
			('desktop','Desktop','physical','monitor'),
			('laptop','Laptop','physical','laptop'),
			('mini-pc','Mini PC','physical','cpu'),
			('router','Router','physical','router'),
			('switch','Switch','physical','network'),
			('firewall','Firewall','physical','shield'),
			('access-point','Wireless AP','physical','wifi'),
			('nas','NAS','physical','hard-drive'),
			('sbc','SBC / Raspberry Pi','physical','circuit-board'),
			('ups','UPS','physical','battery-charging'),
			('iot','IoT Device','physical','radio'),
			('other-physical','Other (Physical)','physical','box'),
			('vm','Virtual Machine','virtual','monitor-dot'),
			('lxc','LXC Container','virtual','container'),
			('docker','Docker Container','virtual','container'),
			('k8s-node','Kubernetes Node','virtual','boxes'),
			('cloud-host','Cloud / Remote Host','virtual','cloud'),
			('pod','Kubernetes Pod','virtual','container')
		ON CONFLICT (slug) DO NOTHING`)
	if err != nil {
		t.Fatalf("reseed: %v", err)
	}
	_, err = pool.Exec(t.Context(), fmt.Sprintf(
		`INSERT INTO app_settings (key, value) VALUES ('instance.name', '"Lanvory"') ON CONFLICT (key) DO NOTHING`))
	if err != nil {
		t.Fatalf("reseed settings: %v", err)
	}
	_, err = pool.Exec(t.Context(), `
		INSERT INTO network_contexts (name, slug, description) VALUES ('Default','default','Main routed network context')
		ON CONFLICT (slug) DO NOTHING`)
	if err != nil {
		t.Fatalf("reseed default context: %v", err)
	}
}
