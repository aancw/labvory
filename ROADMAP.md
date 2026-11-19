# LabVory — Roadmap & Acceptance Criteria

Phases are built sequentially; each phase ships working end-to-end functionality before the next begins.

## Phase 1 — Foundation ✅ (shipped)

**Scope**: repo structure, full database schema + migrations, backend REST API core, frontend app
shell + design system, authentication, setup wizard, settings, navigation, deployment.

Deliverables:
- `backend/`: Go/chi API — health, setup wizard, login/logout/me, CSRF, rate limiting, audit log,
  app settings, dashboard summary stats, global search scaffold.
- `frontend/`: React SPA — login, setup, dashboard (real counts), activity page, settings page,
  persistent sidebar per spec, dark/light theme, Cmd+K search wired to real backend search.
- `deploy/`: docker-compose (Postgres + backend + frontend), multi-stage Dockerfiles, `.env.example`.
- Docs: README, ARCHITECTURE (with ERD), BACKUP, DEVELOPMENT.

**Acceptance criteria**
1. `docker compose up` from `deploy/` boots Postgres + API + SPA; all containers healthy.
2. First visit redirects to setup; creating the administrator persists across restarts.
3. Wrong password rejected with rate limiting; successful login issues a secure session cookie.
4. CSRF token required for unsafe API methods; request without it gets 403.
5. Dashboard renders real (possibly zero) counts from the database; activity page lists audit events.
6. Settings page updates instance name; change-password verifies old password and revokes other sessions.
7. `go build ./... && go vet ./...` clean; unit tests pass; frontend `tsc --noEmit` + `vite build` clean.

## Phase 2 — Asset Management ✅ (shipped)

- Asset CRUD (physical + virtual), asset types, hardware metadata, environments, statuses.
- Network interfaces per asset; asset relationships (runs_on / connected_to / depends_on) with
  type validation; locations (tree); tags.
- Inventory data table: configurable columns, sorting, filtering, pagination, bulk operations.
- Asset detail page: Overview / Hardware / Network / Hosted Resources / Relationships / Activity.

**Acceptance criteria**: register a Proxmox server, then VMs and LXC containers parented to it;
inventory filters by type/status/environment; detail page shows child assets; relationships reject
invalid combinations; all mutations audited.

## Phase 3 — IPAM ✅ (shipped)

- Subnets (CIDR validation, gateway, DHCP range, VLAN, context), VLANs, network contexts.
- IP allocations with statuses and assignment methods; assignment workflow via interfaces.
- Subnet detail page: utilization bar, sequential table, address grid, bulk reserve/release,
  next-available-IP finder, assign-IP-to-asset flow, search/filter.
- Duplicate active allocation prevention at DB level; /31 and /32 handled; utilization math unit-tested.

**Acceptance criteria**: create 10.0.0.0/24, grid renders all addresses, allocate an IP to an
interface, duplicate assignment rejected with 409, release creates history row, IP search in
command palette jumps to the owning asset.

## Phase 4 — Services & Dashboard ✅ (shipped)

- Service CRUD, endpoints, deployments (service ↔ host), quick links.
- Operational dashboard: assets by type/OS/status, IP utilization per subnet, service distribution,
  alerts card, recently modified; every card links to a filtered inventory view.
- Activity page upgraded with entity filters and before/after diffs; saved filters; column
  customization.

**Acceptance criteria**: dashboard cards navigate to pre-filtered lists; service page shows hosting
infrastructure; deleting an asset is blocked or cascades only per explicit rules, with audit trail.

## Phase 5 — Topology & Data Portability ✅ (shipped)

- React Flow topology (physical / logical / virtualization / dependency views), manual connections
  with interface+port metadata, layout persistence, filters, detail sidebar.
- CSV import (assets, IPs, subnets) with column mapping, validation, duplicate detection, preview,
  transactional writes; JSON export of full inventory.

**Acceptance criteria**: import preview shows row-level errors without writing; successful import is
atomic; exported JSON re-imports losslessly; topology renders parent-child collapse and persists
layout.

## Phase 6 — Integrations ✅ (shipped: Proxmox, Docker, ICMP, DNS, Cloudflare, Kubernetes, AdGuard Home, ARP scan; SNMP/SSH/UniFi/OPNsense designed, not implemented)

- Proxmox VE (read-only API token): import nodes/VMs/LXCs, sync CPU/RAM/storage, last-sync display,
  manual metadata preserved, source attribution on imported fields.
- Docker Engine API discovery; opt-in ICMP reachability; sync history per integration.
- Kubernetes (kubeconfig, API token, or in-cluster): nodes and pods as assets, Services into the
  service registry.
- DNS enumeration (AXFR zone transfer or crt.name certificate transparency) and Cloudflare (zones +
  records): observed DNS records under declared domains, declared values never overwritten.
- AdGuard Home: local DNS rewrites imported as observed records; client IPs marked as seen on
  matching allocations.
- ARP scan: in-process gopacket sweep of the active interface — devices become Discovered Device
  assets and their IPs are registered automatically (the subnet is auto-created when none matches).
- Discovery opt-in with explicit scope allowlists; observed vs. declared state distinguished;
  secrets encrypted at rest, never returned in API responses.

**Acceptance criteria**: Proxmox import creates assets parented to the node with `source=proxmox`;
re-sync updates only imported fields; disabling an integration stops collection; sync runs are
auditable.

## Engineering standards (all phases)

- Strict TypeScript, idiomatic Go, small reusable components, predictable error envelope.
- Every mutation audited; no fake buttons; no hardcoded infrastructure data.
- Tests: IPAM math unit tests (P3), API integration tests (P1+), DB constraint tests (P3).
