# LabVory — local development tasks.
#
#   make db        start the PostgreSQL dev database only (idempotent)
#   make run       start API + built web UI (production mode, no hot reload)
#   make dev       start API + Vite web UI with hot reload
#   make db-stop   stop the dev database
#   make build     build backend binary + frontend dist
#   make test      backend unit tests + frontend typecheck
#   make help      list targets

SHELL := /bin/bash
.DEFAULT_GOAL := help

PG_CONTAINER ?= labvory-pg
PG_PORT      ?= 5432
# Where `make release` drops the standalone binary.
RELEASE_DIR  ?= dist-release
# 8080 is deliberately avoided: it commonly collides with other local tools.
API_PORT     ?= 8090
WEB_PORT     ?= 4173
DB_USER      ?= labvory
DB_PASSWORD  ?= labvory
DB_NAME      ?= labvory
# Demo: a disposable database, freshly seeded with sample homelab data on each run.
DEMO_CONTAINER ?= labvory-pg-demo
DEMO_PORT      ?= 5433
DEMO_URL       := postgres://$(DB_USER):$(DB_PASSWORD)@localhost:$(DEMO_PORT)/$(DB_NAME)?sslmode=disable


help:
	@echo "LabVory — local development"
	@echo "  make db        start the PostgreSQL dev database only (idempotent)"
	@echo "  make db-stop   stop the dev database"
	@echo "  make run       API + built web UI on http://localhost:$(WEB_PORT) (production mode)"
	@echo "  make dev       API + Vite web UI on http://localhost:5173 (hot reload)"
	@echo "  make demo      dev stack on a FRESH seeded sample database (:$(DEMO_PORT), rebuilt each run)"
	@echo "  make demo-stop stop the demo copy database"
	@echo "  make build     build backend binary + frontend dist"
	@echo "  make test      backend unit tests + frontend typecheck"
	@echo "  make secret-key  print a fresh LV_SECRET_KEY value (add to deploy/.env)"
	@echo ""
	@echo "Local runs source deploy/.env (from deploy/.env.example) if present."
	@echo ""
	@echo "Overrides: PG_PORT=$(PG_PORT) API_PORT=$(API_PORT) WEB_PORT=$(WEB_PORT)"

# --- Database -------------------------------------------------------------

## Start the PostgreSQL dev database only (creates, starts, or reuses the container).
db:
	@if docker inspect -f '{{.State.Running}}' $(PG_CONTAINER) 2>/dev/null | grep -q running; then \
		echo "▶ $(PG_CONTAINER) already running"; \
	elif docker inspect $(PG_CONTAINER) >/dev/null 2>&1; then \
		echo "▶ starting existing $(PG_CONTAINER)"; \
		docker start $(PG_CONTAINER); \
	else \
		echo "▶ creating $(PG_CONTAINER)"; \
		docker run -d --name $(PG_CONTAINER) \
			-e POSTGRES_USER=$(DB_USER) \
			-e POSTGRES_PASSWORD=$(DB_PASSWORD) \
			-e POSTGRES_DB=$(DB_NAME) \
			-p $(PG_PORT):5432 \
			postgres:17-alpine; \
	fi
	@i=0; \
	until docker exec $(PG_CONTAINER) pg_isready -U $(DB_USER) >/dev/null 2>&1; do \
		i=$$((i+1)); \
		if [ $$i -gt 60 ]; then echo "✗ database did not become ready" >&2; exit 1; fi; \
		sleep 0.5; \
	done
	@echo "✔ Postgres ready on localhost:$(PG_PORT) (db: $(DB_NAME))"

## Stop the dev database.
db-stop:
	@docker stop $(PG_CONTAINER) 2>/dev/null || echo "$(PG_CONTAINER) not running"
	@echo "✔ database stopped"

# --- Demo (fresh seeded database) ------------------------------------------
# Runs the dev stack against a disposable database that is rebuilt empty and
# seeded with the built-in sample homelab data (backend/internal/db/seed.go)
# on every invocation. Your real dev database on :$(PG_PORT) is never touched.

## Rebuild the demo database fresh and run the dev stack against it.
demo: install
	@bash -c 'set -e; \
	docker rm -f $(DEMO_CONTAINER) >/dev/null 2>&1 || true; \
	echo "▶ creating fresh $(DEMO_CONTAINER) on :$(DEMO_PORT)"; \
	docker run -d --name $(DEMO_CONTAINER) \
		-e POSTGRES_USER=$(DB_USER) \
		-e POSTGRES_PASSWORD=$(DB_PASSWORD) \
		-e POSTGRES_DB=$(DB_NAME) \
		-p $(DEMO_PORT):5432 \
		postgres:17-alpine; \
	until docker exec $(DEMO_CONTAINER) pg_isready -U $(DB_USER) >/dev/null 2>&1; do \
		sleep 0.5; \
	done; \
	echo "✔ fresh demo db ready on localhost:$(DEMO_PORT)"'
	@echo "▶ demo mode: API on :$(API_PORT), Vite on :5173 (sample seed, Ctrl+C stops both)"
	@bash -c '$(ENV_DOT) trap "kill 0" EXIT; \
		(cd backend && LV_ENV=development LV_ADDR=:$(API_PORT) LV_DATABASE_URL=$(DEMO_URL) LV_SEED_MODE=dev go run ./cmd/server) & \
		(cd frontend && LV_API_TARGET=http://127.0.0.1:$(API_PORT) npm run dev) & \
		wait'

## Stop the demo database.
demo-stop:
	@docker stop $(DEMO_CONTAINER) 2>/dev/null || echo "$(DEMO_CONTAINER) not running"
	@echo "✔ demo database stopped"

# --- Prerequisites --------------------------------------------------------

## Install frontend dependencies if missing.
install:
	@if [ ! -d frontend/node_modules ]; then echo "▶ npm install"; (cd frontend && npm install); fi

# --- Environment ----------------------------------------------------------
# Local runs source deploy/.env (copied from deploy/.env.example) so LV_SECRET_KEY
# and friends configured for the compose stack apply to `make` runs as well.
ENV_DOT := if [ -f deploy/.env ]; then set -a; . ./deploy/.env; set +a; fi;

# --- Individual services --------------------------------------------------

## Run the Go API only.
backend:
	@bash -c '$(ENV_DOT) cd backend && LV_ENV=development exec go run ./cmd/server'

## Run the Vite dev server only (hot reload, proxies /api to the API).
web: install
	cd frontend && LV_API_TARGET=http://127.0.0.1:$(API_PORT) npm run dev

# --- Full stack -----------------------------------------------------------

## Start API + built web UI (production mode, no hot reload).
run: db install
	@echo "▶ building web UI..."
	@cd frontend && npm run build
	@echo "▶ starting API on :$(API_PORT) and web UI on :$(WEB_PORT) (Ctrl+C stops both)..."
	@bash -c '$(ENV_DOT) trap "kill 0" EXIT; \
		(cd backend && LV_ADDR=:$(API_PORT) go run ./cmd/server) & \
		(cd frontend && LV_API_TARGET=http://127.0.0.1:$(API_PORT) npm run preview -- --port $(WEB_PORT) --host) & \
		wait'

## Start API + Vite web UI with hot reload.
dev: db install
	@echo "▶ dev mode: API on :$(API_PORT), Vite on :5173 (hot reload, Ctrl+C stops both)"
	@bash -c '$(ENV_DOT) trap "kill 0" EXIT; \
		(cd backend && LV_ENV=development LV_ADDR=:$(API_PORT) go run ./cmd/server) & \
		(cd frontend && LV_API_TARGET=http://127.0.0.1:$(API_PORT) npm run dev) & \
		wait'

# --- Build & test ---------------------------------------------------------

## Build backend binary and frontend dist.
build:
	cd backend && go build ./...
	cd frontend && npm run build

## Print a fresh 32-byte hex key for LV_SECRET_KEY (put it in deploy/.env).
secret-key:
	@openssl rand -hex 32

## Backend unit tests + frontend typecheck.
test:
	cd backend && go build ./... && go vet ./... && go test ./...
	cd frontend && npm run typecheck

# --- Release ---------------------------------------------------------------
# Builds the standalone binary: web UI embedded via -tags embed, PostgreSQL is
# the only external dependency. The release workflow (.github/workflows/
# release.yml) does the same for every supported platform on a `v*` tag push.

## Build the standalone binary (web UI embedded) for this platform.
release: install
	@echo "▶ building web UI..."
	@cd frontend && npm run build
	@echo "▶ embedding web UI into the binary..."
	@rm -rf backend/internal/web/dist
	@cp -R frontend/dist backend/internal/web/dist
	@mkdir -p $(RELEASE_DIR)
	@cd backend && go build -trimpath -tags embed \
		-ldflags "-s -w" -o ../$(RELEASE_DIR)/labvory ./cmd/server
	@echo "✔ standalone binary: $(RELEASE_DIR)/labvory (UI embedded; configure via labvory.toml or LV_* env)"

.PHONY: help db db-stop demo demo-stop install backend web run dev build test secret-key release
