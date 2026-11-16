# Lanvory

**Self-hosted homelab infrastructure & asset management.** One place for physical devices, VMs and
containers, IP addresses, subnets, VLANs, and the services that run on top of them.

Lanvory is the single source of truth that replaces scattered spreadsheets, Markdown notes, and
one-off config files. It is local-first, needs no cloud, and is designed for daily use by a single
homelab operator.

> **Status: feature-complete (Phases 1–6).** Asset inventory with parent/child hosting, IPAM with
> address grid and assignment history, service registry, network topology, CSV/JSON import/export,
> and read-only integrations (Proxmox, Docker, Kubernetes, ICMP, DNS, Cloudflare, AdGuard Home,
> ARP scan) are implemented. Remaining "planned" connectors (SNMP, SSH, UniFi, OPNsense) are
> explicitly labeled in the UI — never faked.

---

## Highlights

- **Modular monolith** — one Go binary (chi + pgx) and one React SPA, PostgreSQL as the only state.
- **Complete asset inventory** — physical/virtual assets with host→guest hierarchy, interfaces,
  typed relationships (cycle-safe), locations, tags, bulk operations, saved views.
- **Practical IPAM** — subnets with overlap protection per context, VLANs, interactive address
  grid, next-available finder, bulk reserve/release, assignment history, RFC 3021 /31-/32 handling.
- **Service registry** — endpoints with quick links, multi-host deployments, host distribution.
- **Network topology** — React Flow canvas from explicit links only (physical / virtualization /
  dependency views), drag-position persistence, connection port metadata.
- **DNS inventory** — domains and subdomain records (A/AAAA/CNAME/MX/TXT/SRV/CAA/NS) with declared
  vs. observed values: one-click DNS resolution, drift detection, and a `dns` integration
  (AXFR zone transfer or crt.name certificate-transparency enumeration) that syncs observed state.
- **Data portability** — CSV import with column mapping + row-level preview, transactional commit,
  JSON inventory export and idempotent restore.
- **Integrations** — read-only Proxmox (nodes/VMs/LXC), Docker, Kubernetes (nodes, pods, Services),
  DNS (AXFR + crt.name certificate transparency), Cloudflare (zones + DNS records), AdGuard Home
  (local rewrites + client observations) and ARP scan (gopacket sweep → Discovered Devices +
  automatic IPAM registration) syncs, opt-in ICMP checks; secrets encrypted at rest (AES-256-GCM),
  observed state kept separate from declared inventory.
- **Agent access (MCP)** — a Model Context Protocol server at `/mcp` (streamable HTTP, Bearer
  tokens) exposing full CRUD over inventory data to agents; DB-backed, revocable API tokens.
  See [docs/MCP.md](docs/MCP.md).
- **Argon2id** password hashing, server-side sessions, double-submit CSRF, rate-limited auth
  endpoints, structured audit trail.
- **Full normalized schema from day one** — assets, interfaces, relationships, subnets, VLANs,
  IP allocations + history, services, integrations (native `inet`/`cidr` types, IPv6-ready).
- **Operational dashboard** with real database-derived counts; every card links to its module.
- **Command palette** (`⌘K`) searching assets, IPs, MACs, serials, subnets, and services live.
- **Dark / light / system theme**, compact professional UI in the spirit of Linear/Vercel/Grafana.

## Quickstart (Docker Compose)

```bash
cd deploy
cp .env.example .env       # review the password!
docker compose up -d --build
```

Then open **http://localhost:8088** — or from any machine on your network,
**http://<host-ip>:8088** (both the web UI and the API bind to `0.0.0.0`). First run shows the
administrator setup wizard; everything persists in the `pgdata` volume across restarts.

| Entry point | URL | Scope |
|---|---|---|
| Web UI | `http://<host-ip>:8088` | SPA + `/api` proxy (recommended) |
| API (direct) | `http://<host-ip>:8080/api/v1/...` | Authenticated REST for scripts/clients |

> Remote access runs over plain HTTP — fine for a trusted home LAN. If you expose Lanvory
> beyond your LAN, terminate TLS at a reverse proxy and set `LV_COOKIE_SECURE=true` so session
> cookies are marked Secure.

What compose gives you:

| Service  | Image               | Role                                            |
|----------|---------------------|-------------------------------------------------|
| `db`     | postgres:17-alpine  | Persistent PostgreSQL (internal network only)   |
| `backend`| built from source   | REST API, runs migrations on boot               |
| `frontend`| built from source  | nginx serving the SPA + proxying `/api`         |

### ARP scan deployment (LAN Layer-2 discovery)

The **ARP scan** connector sweeps the **active interface's subnet** in-process (gopacket — no
external binary) and registers everything automatically: devices become **Discovered Device**
assets (one per MAC, updated in place) and their IPs are recorded in IPAM — the subnet is
auto-created when none matches. Raw packet access is required:

- **Linux**: `NET_RAW` — the compose file already grants it, and the image sets the file
  capability on the binary. For LAN reachability from Docker, start with the macvlan override
  and set the connector interface to `eth1` (the default route stays on the compose bridge):

  ```bash
  cd deploy
  # .env: ARP_PARENT (host LAN interface), ARP_SUBNET, ARP_GATEWAY,
  #       ARP_CONTAINER_IP (a FREE LAN IP — reserve it)
  docker compose -f docker-compose.yml -f docker-compose.arp.yml up -d --build
  ```
- **macOS**: BPF devices are root-only; run the backend as root or loosen the devices once
  (`sudo chmod g+rw /dev/bpf*` — resets on reboot). Empty interface = the default-route one.
- **table** mode (no privileges) only lists neighbors the host has talked to — and note that
  macOS may deny spawned helper processes the neighbor table entirely; prefer the sweep there.

## Development

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for the hot-reload workflow (Go server + Vite dev
proxy), integration tests against a disposable PostgreSQL, and repository layout.

## Operations

- Backup & restore: [docs/BACKUP.md](docs/BACKUP.md)
- Architecture, ERD, and design decisions: [ARCHITECTURE.md](ARCHITECTURE.md)
- API contract: [docs/api/openapi.yaml](docs/api/openapi.yaml)
- Phases and acceptance criteria: [ROADMAP.md](ROADMAP.md)

## Security notes

- The database is never exposed outside the compose network.
- Set `LV_COOKIE_SECURE=true` when serving over HTTPS behind your reverse proxy.
- Login and setup endpoints are rate-limited per IP; audit events record security-relevant actions
  (logins, logouts, password changes, settings edits).
- Service credentials must never be stored in Lanvory — integration secrets are stored encrypted
  at rest (AES-256-GCM) and are never returned by the API.
