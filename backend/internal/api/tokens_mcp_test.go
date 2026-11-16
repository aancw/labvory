package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// bootAdmin creates the first admin and logs in, returning the CSRF-aware
// session client.
func bootAdmin(t *testing.T, do func(string, string, any, int) map[string]any) {
	t.Helper()
	do(http.MethodPost, "/api/v1/setup/admin", map[string]any{
		"username": "admin", "password": "password-123",
	}, http.StatusCreated)
}

// mcpCall issues one MCP JSON-RPC request over HTTP and returns the status
// code plus the decoded body (if any).
func mcpCall(t *testing.T, baseURL, token string, id any, rpcMethod string, params any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": rpcMethod, "params": params,
	})
	req, err := http.NewRequest(http.MethodPost, baseURL+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("mcp request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mcp call: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

// mcpToolResult unwraps a tools/call response into (payload, isError).
func mcpToolResult(t *testing.T, resp map[string]any) (map[string]any, bool) {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %v", resp)
	}
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("missing content: %v", result)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	var payload map[string]any
	_ = json.Unmarshal([]byte(text), &payload)
	isError, _ := result["isError"].(bool)
	return payload, isError
}

// TestAPITokenLifecycle covers creation, listing and revocation of API tokens
// over the session-authenticated REST surface.
func TestAPITokenLifecycle(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	_, _, _, do := newTestServer(t, databaseURL)
	bootAdmin(t, do)

	created := do(http.MethodPost, "/api/v1/tokens", map[string]any{"name": "agent"}, http.StatusCreated)
	plaintext, _ := created["token"].(string)
	if len(plaintext) < 40 || plaintext[:3] != "lv_" {
		t.Fatalf("expected lv_-prefixed plaintext token, got %q", plaintext)
	}
	if created["token_hash"] != nil {
		t.Fatalf("plaintext response must not include the hash")
	}
	// Purpose defaults to mcp when omitted.
	if created["purpose"] != "mcp" {
		t.Fatalf("expected default purpose mcp, got %v", created["purpose"])
	}

	list := do(http.MethodGet, "/api/v1/tokens", nil, http.StatusOK)
	tokens, _ := list["tokens"].([]any)
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}

	tokenID, _ := created["id"].(string)
	do(http.MethodDelete, "/api/v1/tokens/"+tokenID, nil, http.StatusOK)
	do(http.MethodDelete, "/api/v1/tokens/"+tokenID, nil, http.StatusNotFound) // already revoked
}

// TestMCPWorkflow covers the full agent path: bearer auth, initialize,
// tools/list, tools/call (create + list + delete), and rejection without a
// valid token.
func TestMCPWorkflow(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	_, baseURL, _, do := newTestServer(t, databaseURL)
	bootAdmin(t, do)

	created := do(http.MethodPost, "/api/v1/tokens", map[string]any{"name": "mcp-agent"}, http.StatusCreated)
	token, _ := created["token"].(string)

	// No token → 401.
	status, _ := mcpCall(t, baseURL, "", 1, "tools/list", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", status)
	}

	// Garbage token → 401.
	status, _ = mcpCall(t, baseURL, "lv_invalidtoken", 1, "tools/list", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 with invalid token, got %d", status)
	}

	// initialize
	_, init := mcpCall(t, baseURL, token, 1, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test", "version": "0"},
	})
	initResult, _ := init["result"].(map[string]any)
	if initResult == nil || initResult["protocolVersion"] == nil {
		t.Fatalf("initialize failed: %v", init)
	}

	// tools/list
	_, list := mcpCall(t, baseURL, token, 2, "tools/list", nil)
	listResult, _ := list["result"].(map[string]any)
	tools, _ := listResult["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("expected tools, got none")
	}

	// tools/call: create a location
	_, call := mcpCall(t, baseURL, token, 3, "tools/call", map[string]any{
		"name": "manage_locations",
		"arguments": map[string]any{
			"action": "create",
			"data":   map[string]any{"name": "Rack A", "description": "main rack"},
		},
	})
	loc, isErr := mcpToolResult(t, call)
	if isErr || loc["id"] == nil {
		t.Fatalf("create location failed: %v", call)
	}
	locID, _ := loc["id"].(string)

	// tools/call: list
	_, listCall := mcpCall(t, baseURL, token, 4, "tools/call", map[string]any{
		"name":      "manage_locations",
		"arguments": map[string]any{"action": "list"},
	})
	listed, isErr := mcpToolResult(t, listCall)
	if isErr {
		t.Fatalf("list locations failed: %v", listCall)
	}
	if locs, ok := listed["locations"].([]any); !ok || len(locs) != 1 {
		t.Fatalf("expected 1 location, got %v", listed)
	}

	// tools/call: delete
	_, del := mcpCall(t, baseURL, token, 5, "tools/call", map[string]any{
		"name":      "manage_locations",
		"arguments": map[string]any{"action": "delete", "id": locID},
	})
	if _, isErr := mcpToolResult(t, del); isErr {
		t.Fatalf("delete location failed: %v", del)
	}

	// Revoked tokens stop working.
	tokenID, _ := created["id"].(string)
	do(http.MethodDelete, "/api/v1/tokens/"+tokenID, nil, http.StatusOK)
	status, _ = mcpCall(t, baseURL, token, 6, "tools/list", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 after revocation, got %d", status)
	}
}

// TestMCPUnknownTool asserts a clean JSON-RPC error for unknown tools.
func TestMCPUnknownTool(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	_, baseURL, _, do := newTestServer(t, databaseURL)
	bootAdmin(t, do)
	created := do(http.MethodPost, "/api/v1/tokens", map[string]any{"name": "x"}, http.StatusCreated)
	token, _ := created["token"].(string)

	_, resp := mcpCall(t, baseURL, token, 7, "tools/call", map[string]any{
		"name": "does_not_exist", "arguments": map[string]any{},
	})
	rpcErr, _ := resp["error"].(map[string]any)
	if rpcErr == nil {
		t.Fatalf("expected JSON-RPC error for unknown tool, got %v", resp)
	}
}

// bearerREST issues a REST request authenticated with a bearer token (no
// cookies, no CSRF) and returns the status code.
func bearerREST(t *testing.T, baseURL, token, method, path string, payload any) int {
	t.Helper()
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	req, err := http.NewRequest(method, baseURL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("bearer request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bearer call: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestTokenPurposeIsolation proves each token only authenticates the surface
// it was issued for: mcp tokens on /mcp, api tokens on /api/v1 REST.
func TestTokenPurposeIsolation(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	_, baseURL, _, do := newTestServer(t, databaseURL)
	bootAdmin(t, do)

	mcpCreated := do(http.MethodPost, "/api/v1/tokens", map[string]any{"name": "mcp", "purpose": "mcp"}, http.StatusCreated)
	mcpToken, _ := mcpCreated["token"].(string)
	apiCreated := do(http.MethodPost, "/api/v1/tokens", map[string]any{"name": "api", "purpose": "api"}, http.StatusCreated)
	apiToken, _ := apiCreated["token"].(string)

	// MCP surface.
	if status, _ := mcpCall(t, baseURL, mcpToken, 1, "tools/list", nil); status != http.StatusOK {
		t.Fatalf("mcp token on /mcp: want 200, got %d", status)
	}
	if status, _ := mcpCall(t, baseURL, apiToken, 1, "tools/list", nil); status != http.StatusForbidden {
		t.Fatalf("api token on /mcp: want 403, got %d", status)
	}

	// REST surface (bearer, no cookies/CSRF).
	if got := bearerREST(t, baseURL, apiToken, http.MethodGet, "/api/v1/assets", nil); got != http.StatusOK {
		t.Fatalf("api token on REST: want 200, got %d", got)
	}
	if got := bearerREST(t, baseURL, apiToken, http.MethodPost, "/api/v1/locations",
		map[string]any{"name": "Bearer Rack"}); got != http.StatusCreated {
		t.Fatalf("api token POST without CSRF: want 201, got %d", got)
	}
	if got := bearerREST(t, baseURL, mcpToken, http.MethodGet, "/api/v1/assets", nil); got != http.StatusForbidden {
		t.Fatalf("mcp token on REST: want 403, got %d", got)
	}

	// Invalid purpose is rejected at creation.
	do(http.MethodPost, "/api/v1/tokens", map[string]any{"name": "bad", "purpose": "admin"}, http.StatusUnprocessableEntity)
}
