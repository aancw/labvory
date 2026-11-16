package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aancw/lanvory/internal/mcp"
	"github.com/aancw/lanvory/internal/store"
)

// MCP tool surface. Tools reuse the existing REST handlers in-process: each
// action is dispatched through the same router (identical validation, audit
// logging and error envelopes), so MCP and the web UI cannot drift apart.

// actionSpec maps one tool action to an internal data-API request. "{id}" in
// the path is filled from the tool argument `id`; params maps the placeholder
// to the chi URL parameter name the handler expects.
type actionSpec struct {
	method string
	path   string
	params map[string]string
}

// domainToolSpec is one MCP tool backed by several REST actions.
type domainToolSpec struct {
	name        string
	description string
	actions     map[string]actionSpec
}

var mcpToolSpecs = []domainToolSpec{
	{
		name:        "manage_assets",
		description: "Manage inventory assets (physical devices, VMs, containers). list/get/create/update/delete. `id` is the asset UUID for get/update/delete; `data` is the asset payload (same JSON fields as the REST API).",
		actions: map[string]actionSpec{
			"list":   {method: http.MethodGet, path: "/assets"},
			"get":    {method: http.MethodGet, path: "/assets/{id}", params: map[string]string{"id": "assetID"}},
			"create": {method: http.MethodPost, path: "/assets"},
			"update": {method: http.MethodPut, path: "/assets/{id}", params: map[string]string{"id": "assetID"}},
			"delete": {method: http.MethodDelete, path: "/assets/{id}", params: map[string]string{"id": "assetID"}},
		},
	},
	{
		name:        "manage_locations",
		description: "Manage physical locations (rooms, racks, buildings) as a hierarchy. list/get/create/update/delete. `id` is the location UUID; `data` is {name, description, parent_id}.",
		actions: map[string]actionSpec{
			"list":   {method: http.MethodGet, path: "/locations"},
			"get":    {method: http.MethodGet, path: "/locations/{id}", params: map[string]string{"id": "locationID"}},
			"create": {method: http.MethodPost, path: "/locations"},
			"update": {method: http.MethodPut, path: "/locations/{id}", params: map[string]string{"id": "locationID"}},
			"delete": {method: http.MethodDelete, path: "/locations/{id}", params: map[string]string{"id": "locationID"}},
		},
	},
	{
		name:        "manage_subnets",
		description: "Manage IP subnets (CIDR). list/get/create/update/delete. `id` is the subnet UUID; `data` is the subnet payload.",
		actions: map[string]actionSpec{
			"list":   {method: http.MethodGet, path: "/subnets"},
			"get":    {method: http.MethodGet, path: "/subnets/{id}", params: map[string]string{"id": "subnetID"}},
			"create": {method: http.MethodPost, path: "/subnets"},
			"update": {method: http.MethodPut, path: "/subnets/{id}", params: map[string]string{"id": "subnetID"}},
			"delete": {method: http.MethodDelete, path: "/subnets/{id}", params: map[string]string{"id": "subnetID"}},
		},
	},
	{
		name:        "manage_ip_addresses",
		description: "Manage IP address allocations. list (all allocations), allocate (reserve an IP: `id` = subnet UUID, `data` = allocation payload), next_available (first free address: `id` = subnet UUID), update, release (mark allocated IP free), history, delete. `id` is the IP allocation UUID except for allocate/next_available.",
		actions: map[string]actionSpec{
			"list":           {method: http.MethodGet, path: "/ip-addresses"},
			"allocate":       {method: http.MethodPost, path: "/subnets/{id}/addresses", params: map[string]string{"id": "subnetID"}},
			"next_available": {method: http.MethodGet, path: "/subnets/{id}/next-available", params: map[string]string{"id": "subnetID"}},
			"update":         {method: http.MethodPut, path: "/ip-addresses/{id}", params: map[string]string{"id": "ipID"}},
			"release":        {method: http.MethodPost, path: "/ip-addresses/{id}/release", params: map[string]string{"id": "ipID"}},
			"history":        {method: http.MethodGet, path: "/ip-addresses/{id}/history", params: map[string]string{"id": "ipID"}},
			"delete":         {method: http.MethodDelete, path: "/ip-addresses/{id}", params: map[string]string{"id": "ipID"}},
		},
	},
	{
		name:        "manage_vlans",
		description: "Manage VLANs. list/create/update/delete. `id` is the VLAN UUID; `data` is the VLAN payload.",
		actions: map[string]actionSpec{
			"list":   {method: http.MethodGet, path: "/vlans"},
			"create": {method: http.MethodPost, path: "/vlans"},
			"update": {method: http.MethodPut, path: "/vlans/{id}", params: map[string]string{"id": "vlanID"}},
			"delete": {method: http.MethodDelete, path: "/vlans/{id}", params: map[string]string{"id": "vlanID"}},
		},
	},
	{
		name:        "manage_network_contexts",
		description: "Manage network contexts (management, lab, DMZ, ...). list/create/update. `id` is the context UUID; `data` is the context payload.",
		actions: map[string]actionSpec{
			"list":   {method: http.MethodGet, path: "/network-contexts"},
			"create": {method: http.MethodPost, path: "/network-contexts"},
			"update": {method: http.MethodPut, path: "/network-contexts/{id}", params: map[string]string{"id": "contextID"}},
		},
	},
	{
		name:        "manage_services",
		description: "Manage homelab services (web apps, databases, ...). list/get/create/update/delete, plus create_endpoint (attach a service endpoint: `id` = service UUID, `data` = endpoint payload).",
		actions: map[string]actionSpec{
			"list":            {method: http.MethodGet, path: "/services"},
			"get":             {method: http.MethodGet, path: "/services/{id}", params: map[string]string{"id": "serviceID"}},
			"create":          {method: http.MethodPost, path: "/services"},
			"update":          {method: http.MethodPut, path: "/services/{id}", params: map[string]string{"id": "serviceID"}},
			"delete":          {method: http.MethodDelete, path: "/services/{id}", params: map[string]string{"id": "serviceID"}},
			"create_endpoint": {method: http.MethodPost, path: "/services/{id}/endpoints", params: map[string]string{"id": "serviceID"}},
		},
	},
	{
		name:        "manage_interfaces",
		description: "Manage network interfaces on assets. list (all), create (attach to an asset: `id` = asset UUID, `data` = interface payload), update, delete. `id` is the interface UUID except for create.",
		actions: map[string]actionSpec{
			"list":   {method: http.MethodGet, path: "/interfaces"},
			"create": {method: http.MethodPost, path: "/assets/{id}/interfaces", params: map[string]string{"id": "assetID"}},
			"update": {method: http.MethodPut, path: "/interfaces/{id}", params: map[string]string{"id": "interfaceID"}},
			"delete": {method: http.MethodDelete, path: "/interfaces/{id}", params: map[string]string{"id": "interfaceID"}},
		},
	},
	{
		name:        "manage_relationships",
		description: "Manage relationships between assets (e.g. hypervisor hosts VM, switch uplinks to router). create (host the edge on: `id` = source asset UUID, `data` = relationship payload), delete. `id` is the relationship UUID for delete.",
		actions: map[string]actionSpec{
			"create": {method: http.MethodPost, path: "/assets/{id}/relationships", params: map[string]string{"id": "assetID"}},
			"delete": {method: http.MethodDelete, path: "/relationships/{id}", params: map[string]string{"id": "relationshipID"}},
		},
	},
	{
		name:        "manage_domains",
		description: "Manage the DNS inventory: domains (zones) and their subdomain records. list, get (`id` = domain UUID), create, update, delete domains; create_subdomain (`id` = domain UUID, `data` = {label, record_type, value, ttl}), update_subdomain / delete_subdomain (`id` = record UUID), resolve_subdomain (query DNS, store the observed answer, report drift; `id` = record UUID), resolve_domain (resolve every record of a domain; `id` = domain UUID).",
		actions: map[string]actionSpec{
			"list":              {method: http.MethodGet, path: "/domains"},
			"get":               {method: http.MethodGet, path: "/domains/{id}", params: map[string]string{"id": "domainID"}},
			"create":            {method: http.MethodPost, path: "/domains"},
			"update":            {method: http.MethodPut, path: "/domains/{id}", params: map[string]string{"id": "domainID"}},
			"delete":            {method: http.MethodDelete, path: "/domains/{id}", params: map[string]string{"id": "domainID"}},
			"create_subdomain":  {method: http.MethodPost, path: "/domains/{id}/subdomains", params: map[string]string{"id": "domainID"}},
			"update_subdomain":  {method: http.MethodPut, path: "/subdomains/{id}", params: map[string]string{"id": "subdomainID"}},
			"delete_subdomain":  {method: http.MethodDelete, path: "/subdomains/{id}", params: map[string]string{"id": "subdomainID"}},
			"resolve_subdomain": {method: http.MethodPost, path: "/subdomains/{id}/resolve", params: map[string]string{"id": "subdomainID"}},
			"resolve_domain":    {method: http.MethodPost, path: "/domains/{id}/resolve", params: map[string]string{"id": "domainID"}},
		},
	},
	{
		name:        "list_asset_types",
		description: "List the built-in asset type catalogue (server, switch, nas, ...). No arguments.",
		actions: map[string]actionSpec{
			"list": {method: http.MethodGet, path: "/asset-types"},
		},
	},
	{
		name:        "search",
		description: "Full-text search across the whole inventory (assets, subnets, services, ...). Pass `query` with a `q` key, e.g. {\"q\": \"proxmox\"}.",
		actions: map[string]actionSpec{
			"list": {method: http.MethodGet, path: "/search"},
		},
	},
}

var mcpToolIndex = func() map[string]domainToolSpec {
	idx := make(map[string]domainToolSpec, len(mcpToolSpecs))
	for _, t := range mcpToolSpecs {
		idx[t.name] = t
	}
	return idx
}()

// domainSchema builds the shared input schema for domain tools.
func domainSchema(actions []string, idDesc string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        actions,
				"description": "Operation to perform.",
			},
			"id": map[string]any{
				"type":        "string",
				"description": idDesc,
			},
			"data": map[string]any{
				"type":        "object",
				"description": "JSON payload for create/update/allocate (same fields as the REST API).",
			},
			"query": map[string]any{
				"type":                 "object",
				"description":          "Optional URL query parameters for list actions (e.g. {\"q\": \"proxmox\"} for search).",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
		"required": []string{"action"},
	}
}

var mcpToolList = func() []mcp.Tool {
	tools := make([]mcp.Tool, 0, len(mcpToolSpecs))
	for _, t := range mcpToolSpecs {
		var actions []string
		for a := range t.actions {
			actions = append(actions, a)
		}
		sortStrings(actions)
		idDesc := "Resource UUID (required for get/update/delete and for URL-scoped actions such as allocate/create_endpoint)."
		if t.name == "search" {
			idDesc = "Unused for search."
		}
		tools = append(tools, mcp.Tool{
			Name:        t.name,
			Description: t.description,
			InputSchema: domainSchema(actions, idDesc),
		})
	}
	return tools
}()

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// mcpRouter builds the /mcp subrouter: bearer-token auth around the MCP handler.
func (s *Server) mcpRouter() http.Handler {
	m := chi.NewRouter()
	m.Use(s.bearerAuth)
	m.Handle("/", &mcp.Handler{
		ServerName: "lanvory",
		Version:    "1.0.0",
		ListTools:  func() []mcp.Tool { return mcpToolList },
		Call:       s.callMCPTool,
	})
	return m
}

// toolArgs is the uniform argument shape for all domain tools.
type toolArgs struct {
	Action string            `json:"action"`
	ID     string            `json:"id"`
	Data   json.RawMessage   `json:"data"`
	Query  map[string]string `json:"query"`
}

// callMCPTool dispatches one tool action through the internal data router.
func (s *Server) callMCPTool(ctx context.Context, name string, args json.RawMessage) (int, any) {
	spec, ok := mcpToolIndex[name]
	if !ok {
		return http.StatusNotFound, nil
	}
	var a toolArgs
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			return http.StatusBadRequest, errEnvelope("invalid_json", "Arguments must be a JSON object: "+err.Error())
		}
	}
	action, ok := spec.actions[a.Action]
	if !ok {
		return http.StatusUnprocessableEntity, errEnvelope("invalid_action",
			fmt.Sprintf("Unknown action %q for tool %q.", a.Action, name))
	}

	// Resolve {id} placeholders and chi URL params.
	u, err := url.Parse(action.path)
	if err != nil {
		return http.StatusInternalServerError, errEnvelope("internal", err.Error())
	}
	pathID := a.ID
	if strings.Contains(u.Path, "{id}") {
		if pathID == "" {
			return http.StatusUnprocessableEntity, errEnvelope("missing_id",
				fmt.Sprintf("Action %q requires a resource `id`.", a.Action))
		}
		u.Path = strings.ReplaceAll(u.Path, "{id}", url.PathEscape(pathID))
	}
	q := u.Query()
	for k, v := range a.Query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	var body []byte
	if a.Data != nil {
		body = a.Data
	} else if action.method != http.MethodGet && action.method != http.MethodDelete {
		body = []byte("{}")
	}

	req, err := http.NewRequestWithContext(ctx, action.method, u.String(), bytes.NewReader(body))
	if err != nil {
		return http.StatusInternalServerError, errEnvelope("internal", err.Error())
	}
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	if pathID != "" {
		for _, chiParam := range action.params {
			rctx.URLParams.Add(chiParam, pathID)
		}
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	s.dataRouter.ServeHTTP(rr, req)

	var payload any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil || rr.Body.Len() == 0 {
		payload = map[string]any{"status": rr.Code}
	}
	return rr.Code, payload
}

func errEnvelope(code, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"code": code, "message": msg}}
}

var _ = store.ErrNotFound
