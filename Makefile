.PHONY: up down logs build fmt test validate migrate lint smoke dashboard

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

# Apply the initial migration. Idempotent only if tables don't exist yet;
# re-running against a migrated DB will error on duplicate object creation.
migrate:
	docker compose exec -T postgres psql -U cognition -d cognition -f /dev/stdin < migrations/001_initial.sql

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
