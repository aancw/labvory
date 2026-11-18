# Development Guide

## Prerequisites

- Go ≥ 1.27
- Node ≥ 22 with npm
- Docker (for a disposable PostgreSQL)

## Local workflow (hot reload)

Or simply `make db && make dev` — it starts the database, API and Vite with hot
reload, sources `deploy/.env` (see `deploy/.env.example`) if present, and stops
both processes on Ctrl+C. `make run` does the same against a production build.

1. **Database** — start a dev PostgreSQL:

   ```bash
   docker run -d --name lanvory-pg \
     -e POSTGRES_USER=lanvory -e POSTGRES_PASSWORD=lanvory -e POSTGRES_DB=lanvory \
     -p 5432:5432 postgres:17-alpine
   ```

2. **Backend** — runs migrations automatically on boot:

   ```bash
   cd backend
   LV_ENV=development go run ./cmd/server
   ```

   The default `LV_DATABASE_URL` is `postgres://lanvory:lanvory@localhost:5432/lanvory?sslmode=disable`
   (override with `LV_DATABASE_URL` or `DATABASE_URL`). The API defaults to `:8090` (8080 often
   collides with other local tools). Taken? Use another `LV_ADDR` and set `LV_API_TARGET` for the
   frontend proxy (step 3).

3. **Frontend** — Vite dev server proxies `/api` and `/healthz` to the backend, and binds
   `0.0.0.0` so phones/other machines on the LAN can open it too:

   ```bash
   cd frontend
   npm install
   LV_API_TARGET=http://127.0.0.1:8090 npm run dev
   ```

   Open the printed URL (default `http://localhost:5173`, or `http://<host-ip>:5173` remotely).
   First visit → setup wizard.

## Release builds (embedded web UI)

The standalone binary embeds the built SPA with `go:embed` (`backend/internal/web`):

- Plain builds (`go build ./...`, tests) carry no assets and serve a fallback page — the embed tag is never required for development.
- `make release` builds the UI, copies `frontend/dist` to `backend/internal/web/dist` (gitignored), and produces the standalone binary in `dist-release/`.
- Configuration: `./lanvory.toml` (or `-config <path>`, TOML, unknown keys rejected — see `deploy/lanvory.toml.example`), with `LV_*` environment variables winning over the file. Docker deployments use environment variables alone.
- Pushing a `v*` tag runs [.github/workflows/release.yml](../.github/workflows/release.yml), which builds the same binary for linux/darwin × amd64/arm64 and publishes archives (binary + LICENSE + `lanvory.toml.example`) + checksums to GitHub Releases.
- `api.Version` is stamped via `-ldflags -X github.com/aancw/lanvory/internal/api.Version=<tag>`; locally it reports `dev`.

## Tests

```bash
# Backend: build, vet, unit tests (no DB needed)
cd backend
go build ./... && go vet ./... && go test ./...

# API integration test (end-to-end workflow against real PostgreSQL)
docker run -d --name lanvory-test-pg \
  -e POSTGRES_USER=gwtest -e POSTGRES_PASSWORD=gwtest -e POSTGRES_DB=gwtest \
  -p 55432:5432 postgres:17-alpine
TEST_DATABASE_URL="postgres://gwtest:gwtest@localhost:55432/gwtest?sslmode=disable" \
  go test ./internal/api/... -v

# Frontend: strict typecheck + production build
cd ../frontend
npm run build
```

The integration test truncates the target database — only point it at a disposable one.

## Repository layout

```
backend/    Go API (cmd/server + internal/{api,auth,config,db,httpx,store,web})
frontend/   React SPA (src/{api,components,pages})
deploy/     docker-compose.yml + .env.example
docs/       operations & development guides, OpenAPI spec, MCP agent guide
```

## Conventions

- Go: layered — `api` (transport) → `auth`/services → `store` (SQL). Errors use the uniform
  `{error:{code,message}}` envelope. Every mutation writes an audit event.
- TypeScript is strict (`exactOptionalPropertyTypes` on); API types live in `src/api/types.ts` and
  mirror the backend JSON exactly; server state goes through TanStack Query.
- Migrations are embedded SQL in `backend/internal/db/migrations` — never edit an applied migration;
  add a new one.
- Sidebar items for unbuilt modules carry a `phase` marker in `src/components/nav.ts` and render
  explicit "Phase N" placeholders. No fake features.
