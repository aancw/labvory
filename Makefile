# Lanvory — local development tasks.
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

PG_CONTAINER ?= lanvory-pg
PG_PORT      ?= 5432
# 8080 is deliberately avoided: it commonly collides with other local tools.
API_PORT     ?= 8090
WEB_PORT     ?= 4173
DB_USER      ?= lanvory
DB_PASSWORD  ?= lanvory
DB_NAME      ?= lanvory


help:
	@echo "Lanvory — local development"
	@echo "  make db        start the PostgreSQL dev database only (idempotent)"
	@echo "  make db-stop   stop the dev database"
	@echo "  make run       API + built web UI on http://localhost:$(WEB_PORT) (production mode)"
	@echo "  make dev       API + Vite web UI on http://localhost:5173 (hot reload)"
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

.PHONY: help db db-stop install backend web run dev build test secret-key
