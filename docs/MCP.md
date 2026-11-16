# MCP Server — agent access to the inventory

Lanvory ships a Model Context Protocol (MCP) server at `/mcp` (streamable HTTP
transport, stateless mode) so agents and scripts can create, list and manage
inventory data: assets, locations, subnets, IP allocations, VLANs, network
contexts, services, interfaces and relationships.

Authentication uses **API tokens** (DB-backed, revocable). Tokens never
expire; revoke them instead.

Tokens are **purpose-scoped**: an `mcp` token authenticates only this `/mcp`
endpoint (and is rejected by the REST API), while an `api` token
authenticates only the `/api/v1` REST API (and is rejected here). Issue a
separate token per purpose so compromise or revocation stays contained.

## 1. Create a token

The easiest path is **Settings → API tokens** in the web UI (choose *MCP
agent* as the purpose; the plaintext is shown once). Via curl:

```bash
# Login and keep the cookie jar
curl -c jar.txt http://localhost:8090/api/v1/csrf
CSRF=$(grep lv_csrf jar.txt | awk '{print $7}')
curl -b jar.txt -c jar.txt -X POST http://localhost:8090/api/v1/auth/login \
  -H "Content-Type: application/json" -H "X-CSRF-Token: $CSRF" \
  -d '{"username":"admin","password":"..."}'

# Issue an MCP-purpose token (plaintext is shown exactly once)
curl -b jar.txt -c jar.txt -X POST http://localhost:8090/api/v1/tokens \
  -H "Content-Type: application/json" -H "X-CSRF-Token: $CSRF" \
  -d '{"name":"homelab-agent","purpose":"mcp"}'
# → {"id":"...","token":"lv_...","name":"homelab-agent","purpose":"mcp",...}
```

For REST API automation use `"purpose":"api"` instead:

```bash
curl -H "Authorization: Bearer lv_..." http://localhost:8090/api/v1/assets
# No CSRF token or session cookie needed — bearer tokens are explicit credentials.
```

## 2. Point the agent at the MCP endpoint

Any MCP client speaking streamable HTTP works. Configure:

- **URL**: `http://<host>:<port>/mcp`
- **Auth header**: `Authorization: Bearer lv_...`

Example for clients that accept raw config (e.g. `mcp-remote`-style bridges or
custom agent runtimes):

```json
{
  "mcpServers": {
    "lanvory": {
      "type": "http",
      "url": "http://localhost:8090/mcp",
      "headers": { "Authorization": "Bearer lv_<your-token>" }
    }
  }
}
```

## 3. Tools

| Tool | Actions |
|---|---|
| `manage_assets` | list, get, create, update, delete |
| `manage_locations` | list, get, create, update, delete |
| `manage_subnets` | list, get, create, update, delete |
| `manage_ip_addresses` | list, allocate, next_available, update, release, history, delete |
| `manage_vlans` | list, create, update, delete |
| `manage_network_contexts` | list, create, update |
| `manage_services` | list, get, create, update, delete, create_endpoint |
| `manage_interfaces` | list, create, update, delete |
| `manage_relationships` | create, delete |
| `manage_domains` | list, get, create, update, delete domains; create_subdomain, update_subdomain, delete_subdomain, resolve_subdomain, resolve_domain |
| `list_asset_types` | list |
| `search` | full-text search (`{"q": "..."}`) |

All domain tools share one argument shape:

```json
{ "action": "create", "id": "<uuid or omitted>", "data": { ... }, "query": { ... } }
```

Payloads are exactly the REST API payloads (the tools dispatch through the
same handlers, so validation, audit logging and error envelopes are identical).
Results carry the REST response JSON in `content[0].text`; failed calls set
`isError: true` with the standard error envelope.

## 4. Token management

```bash
curl -H "Authorization: Bearer lv_..." ...        # use a token (any /mcp call)
curl -b jar.txt http://localhost:8090/api/v1/tokens          # list (no secrets)
curl -b jar.txt -X DELETE http://localhost:8090/api/v1/tokens/<id>  # revoke
```

Revocation is immediate: the next request with a revoked token gets 401.
