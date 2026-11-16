// Package mcp implements the server side of the Model Context Protocol
// (streamable HTTP transport, stateless mode). Clients POST JSON-RPC 2.0
// messages; responses are plain JSON (no SSE streams needed — every tool
// call completes within a single request). Authentication is enforced by
// the caller (bearer middleware) before the handler runs.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ProtocolVersion is the MCP revision this server speaks.
const ProtocolVersion = "2025-06-18"

// supportedVersions are protocol revisions echoed back to clients.
var supportedVersions = map[string]bool{ProtocolVersion: true, "2025-03-26": true}

// Tool describes one callable tool exposed over MCP.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// CallFunc executes a tool and returns the HTTP-ish status plus the payload
// to embed in the MCP result content.
type CallFunc func(ctx context.Context, name string, args json.RawMessage) (status int, payload any)

// Handler serves the MCP streamable-HTTP endpoint.
type Handler struct {
	ServerName string
	Version    string
	ListTools  func() []Tool
	Call       CallFunc
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

const maxRequestBytes = 1 << 20 // 1 MiB

// ServeHTTP handles POST JSON-RPC messages; other methods are rejected
// (this server never initiates streams, so GET/DELETE have nothing to offer).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "MCP endpoint accepts POST only", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", Error: &rpcError{-32700, "Parse error"}})
		return
	}
	if req.JSONRPC != "2.0" {
		writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{-32600, "Invalid Request: jsonrpc must be \"2.0\""}})
		return
	}

	// Notifications carry no id and get an empty 202.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		switch req.Method {
		case "notifications/initialized", "notifications/cancelled":
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
		return
	}

	switch req.Method {
	case "initialize":
		var p initializeParams
		_ = json.Unmarshal(req.Params, &p)
		version := ProtocolVersion
		if supportedVersions[p.ProtocolVersion] {
			version = p.ProtocolVersion
		}
		writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": h.ServerName, "version": h.Version},
			"instructions":    "Manage homelab inventory: assets, locations, IPAM (subnets/IPs/VLANs/network contexts), services, interfaces and the DNS inventory (domains and records). Destructive actions are real — confirm with the user before deleting.",
		}})
	case "ping":
		writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": h.ListTools()}})
	case "tools/call":
		var p toolCallParams
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{-32602, "Invalid params: name required"}})
			return
		}
		status, payload := h.Call(r.Context(), p.Name, p.Arguments)
		if status == http.StatusNotFound && payload == nil {
			writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{-32602, fmt.Sprintf("Unknown tool: %s", p.Name)}})
			return
		}
		text, _ := json.Marshal(payload)
		writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(text)}},
			"isError": status >= 400,
		}})
	default:
		writeRPC(w, jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{-32601, fmt.Sprintf("Method not found: %s", req.Method)}})
	}
}

func writeRPC(w http.ResponseWriter, resp jsonRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
