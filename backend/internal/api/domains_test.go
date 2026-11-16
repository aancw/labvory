package api

import (
	"net/http"
	"strings"
	"testing"
)

// TestDomainsWorkflow covers domain/subdomain CRUD plus DNS resolution
// (observed state) and drift reporting against the local resolver.
func TestDomainsWorkflow(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	_, _, _, do := newTestServer(t, databaseURL)
	bootAdmin(t, do)

	// Domain creation + validation.
	domain := do(http.MethodPost, "/api/v1/domains", map[string]any{
		"name": "localhost", "registrar": "internal", "notes": "test zone",
	}, http.StatusCreated)
	domainID, _ := domain["id"].(string)
	if domainID == "" {
		t.Fatalf("missing domain id: %v", domain)
	}
	do(http.MethodPost, "/api/v1/domains", map[string]any{"name": "localhost"}, http.StatusConflict)
	do(http.MethodPost, "/api/v1/domains", map[string]any{"name": "not a domain"}, http.StatusUnprocessableEntity)

	list := do(http.MethodGet, "/api/v1/domains", nil, http.StatusOK)
	if domains, _ := list["domains"].([]any); len(domains) != 1 {
		t.Fatalf("expected 1 domain, got %v", list)
	}

	// Apex A record pointing at the loopback address.
	rec := do(http.MethodPost, "/api/v1/domains/"+domainID+"/subdomains", map[string]any{
		"label": "", "record_type": "A", "value": "127.0.0.1", "ttl": 300,
	}, http.StatusCreated)
	recID, _ := rec["id"].(string)
	if rec["fqdn"] != "localhost" {
		t.Fatalf("apex fqdn = %v, want localhost", rec["fqdn"])
	}

	// Duplicate label+type is a conflict; bad type and bad IP are rejected.
	do(http.MethodPost, "/api/v1/domains/"+domainID+"/subdomains", map[string]any{
		"label": "", "record_type": "A", "value": "127.0.0.2",
	}, http.StatusConflict)
	do(http.MethodPost, "/api/v1/domains/"+domainID+"/subdomains", map[string]any{
		"label": "x", "record_type": "BOGUS", "value": "1.2.3.4",
	}, http.StatusUnprocessableEntity)
	do(http.MethodPost, "/api/v1/domains/"+domainID+"/subdomains", map[string]any{
		"label": "x", "record_type": "A", "value": "not-an-ip",
	}, http.StatusUnprocessableEntity)

	// Resolve: localhost resolves locally; declared value matches → no drift.
	resolved := do(http.MethodPost, "/api/v1/subdomains/"+recID+"/resolve", nil, http.StatusOK)
	sd, _ := resolved["subdomain"].(map[string]any)
	if sd == nil || sd["observed_at"] == nil {
		t.Fatalf("resolve did not store an observation: %v", resolved)
	}
	observed, _ := sd["observed_value"].(string)
	if !strings.Contains(observed, "127.0.0.1") {
		t.Fatalf("observed = %q, want 127.0.0.1", observed)
	}
	if drift, _ := resolved["drift"].(bool); drift {
		t.Fatalf("unexpected drift: %v", resolved)
	}

	// Declare a wrong address → resolve reports drift.
	do(http.MethodPut, "/api/v1/subdomains/"+recID, map[string]any{
		"label": "", "record_type": "A", "value": "10.9.9.9",
	}, http.StatusOK)
	resolved = do(http.MethodPost, "/api/v1/subdomains/"+recID+"/resolve", nil, http.StatusOK)
	if drift, _ := resolved["drift"].(bool); !drift {
		t.Fatalf("expected drift after declared value changed: %v", resolved)
	}

	// Domain-level resolve covers every record.
	bulk := do(http.MethodPost, "/api/v1/domains/"+domainID+"/resolve", nil, http.StatusOK)
	if total, _ := bulk["total"].(float64); total != 1 {
		t.Fatalf("expected 1 record resolved, got %v", bulk)
	}

	// Records are visible through the domain detail endpoint.
	detail := do(http.MethodGet, "/api/v1/domains/"+domainID, nil, http.StatusOK)
	if subs, _ := detail["subdomains"].([]any); len(subs) != 1 {
		t.Fatalf("expected 1 subdomain, got %v", detail)
	}

	// Delete cascades to records.
	do(http.MethodDelete, "/api/v1/domains/"+domainID, nil, http.StatusOK)
	do(http.MethodGet, "/api/v1/domains/"+domainID, nil, http.StatusNotFound)
}
