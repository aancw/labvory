# AGENTS.md

Guidance for AI agents working in this repository. Orientation lives in [README.md](README.md),
design and ERD in [ARCHITECTURE.md](ARCHITECTURE.md), setup and test workflow in
[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md), API contract in [docs/api/openapi.yaml](docs/api/openapi.yaml).

Lanvory is a self-hosted homelab inventory: Go (chi + pgx) backend, React SPA (Vite + TanStack
Query), PostgreSQL as the only state.

## Invariants

- **Declared vs. observed state.** Integrations never delete manually maintained assets and never
  overwrite operator-declared fields. Observations go under `metadata.observed.*`:
  `store.Integrations.SetObservedSub` merges one key — `SetObserved` replaces the whole map and
  clobbers other observers (e.g. ICMP reachability), so prefer the sub-key variant.
- **Secrets are write-only.** New connector kinds register their secret keys in
  `secretKeysByKind` (`backend/internal/api/handlers_integrations.go`); values are encrypted at
  rest (AES-256-GCM) and never echoed in API responses.
- **Every mutation is audited** via `Server.auditChange`; update handlers record per-field
  `{from, to}` diffs (see `diffMap` in `backend/internal/api/handlers_inventory.go`) — the
  Activity page renders these, so new update handlers should follow the same diff shape.
- **No fake functionality.** A connector is shippable only once its sync exists: register it in
  `implementedKinds`; designed-but-unbuilt kinds stay labeled planned in the UI.

## Adding an integration kind

1. Migration: extend the `integrations` kind CHECK — the new migration must list **all existing
   kinds plus the new one** (copy the latest numbered kind migration as the pattern; migrations
   are sequential in `backend/internal/db/migrations`, and each needs both `.up.sql` and
   `.down.sql`).
2. Sync implementation in `backend/internal/integrations/`, dispatched from `Runner`.
3. Register the kind in `implementedKinds`, `knownKinds`, and `secretKeysByKind`.
4. Frontend: add a `KIND_META` entry (label, icon, description) and a config-dialog section in
   `frontend/src/pages/IntegrationsPage.tsx`.

## Raw-socket platform split

ARP sweep code is deliberately split by build tag: `arpsweep_darwin.go` (pcap/cgo) vs
`arpsweep_linux.go` (afpacket, pure Go) so the Docker build stays `CGO_ENABLED=0`; the runtime
image sets `cap_net_raw` on the binary (`backend/Dockerfile`). Keep platform-specific packet code
inside those files.

## Environment map

- **Dev** (`make dev`): PostgreSQL + API on :8090 + Vite on :5173 with hot reload. **Compose**
  serves the whole stack on :8088 — different world, don't mix the URLs.
- `make help` lists all targets; integration tests run against a disposable PostgreSQL only —
  they truncate the target database ([docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)).

## Verify before finishing

```
cd backend  && go build ./... && go vet ./... && go test ./...
cd frontend && ./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/vite build
```

Both gates clean = done. Invoke the frontend binaries directly (`./node_modules/.bin/…`).
