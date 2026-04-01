.PHONY: up down logs build fmt test validate migrate lint smoke dashboard export backup reconcile

# ---------------------------------------------------------------------------
# Local dev environment
# ---------------------------------------------------------------------------

up:
	docker compose up -d
	@echo "Waiting for postgres..."
	@until docker compose exec -T postgres pg_isready -U cognition -d cognition > /dev/null 2>&1; do sleep 1; done
	@echo "Waiting for minio..."
	@until docker compose exec -T minio curl -sf http://localhost:9000/minio/health/live > /dev/null 2>&1; do sleep 1; done
	@echo ""
	@echo "  Postgres:      localhost:5432"
	@echo "  MinIO API:     http://localhost:9000"
	@echo "  MinIO Console: http://localhost:9001  (minioadmin / minioadmin)"
	@echo "  Control plane: http://localhost:8080  (after: make migrate && make build)"

down:
	docker compose down

logs:
	docker compose logs -f

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

build:
	go build ./cmd/control-plane

# ---------------------------------------------------------------------------
# Dashboard
# ---------------------------------------------------------------------------
# Compile TypeScript → dashboard/static/main.js.
# Requires: npm install -g typescript  (or tsc available in PATH)
# Compiled assets are committed so no Node is needed at runtime.

dashboard:
	cd dashboard && tsc

fmt:
	gofmt -w ./...

lint:
	go vet ./...

test: validate lint

# ---------------------------------------------------------------------------
# Database
# ---------------------------------------------------------------------------

# Apply all migrations in order.
# 001: core tables (not idempotent — do not re-run against a migrated DB)
# 002: agent_keys (idempotent — uses CREATE TABLE IF NOT EXISTS)
migrate:
	docker compose exec -T postgres psql -U cognition -d cognition -f /dev/stdin < migrations/001_initial.sql
	docker compose exec -T postgres psql -U cognition -d cognition -f /dev/stdin < migrations/002_agent_keys.sql

# ---------------------------------------------------------------------------
# Schema validation
# ---------------------------------------------------------------------------

validate:
	python3 scripts/validate_schemas.py

# ---------------------------------------------------------------------------
# Smoke test (requires: make up && make migrate, control plane running locally)
# ---------------------------------------------------------------------------
# Sends a minimal well-formed canonical object upload and verifies the round-trip.
# Set PAYLOAD to override the test document content.

PAYLOAD ?= {"smoke":"test","ts":"$(shell date -u +%Y-%m-%dT%H:%M:%SZ)"}

smoke:
	@./scripts/smoke_test.sh "$(PAYLOAD)"

# ---------------------------------------------------------------------------
# Operations (require: make up && make migrate, control plane running)
# ---------------------------------------------------------------------------

# Export all canonical objects from the ledger to backups/canonicals_<ts>.ndjson
# NDJSON: one JSON object per line. Includes storage_path for payload retrieval.
export:
	@./scripts/export_canonicals.sh

# Dump Postgres to backups/cognition_<ts>.sql.gz via pg_dump.
# Restore: gunzip -c <file> | docker compose exec -T postgres psql -U cognition -d cognition
backup:
	@./scripts/backup_pg.sh

# Verify every ledger-recorded canonical object exists in object storage.
# Exit 1 if any storage paths return 404.
reconcile:
	@./scripts/reconcile_storage.sh
