.PHONY: up down logs fmt test validate lint

up:
	docker compose up -d
	@echo "Waiting for services to be healthy..."
	@until docker compose ps | grep -E "(postgres|minio)" | grep -v "healthy" | grep -qv "^$$"; do \
		sleep 2; \
		if docker compose ps | grep -E "(postgres|minio)" | grep -q "unhealthy"; then \
			echo "A service entered unhealthy state. Check logs with: make logs"; \
			exit 1; \
		fi; \
	done || true
	@echo "Waiting for postgres..."
	@until docker compose exec -T postgres pg_isready -U cognition -d cognition > /dev/null 2>&1; do sleep 1; done
	@echo "Waiting for minio..."
	@until docker compose exec -T minio curl -sf http://localhost:9000/minio/health/live > /dev/null 2>&1; do sleep 1; done
	@echo "All services healthy."
	@echo "  Postgres: localhost:5432"
	@echo "  MinIO API: localhost:9000"
	@echo "  MinIO Console: http://localhost:9001 (minioadmin / minioadmin)"

down:
	docker compose down

logs:
	docker compose logs -f

fmt:
	@if command -v gofmt > /dev/null 2>&1; then \
		gofmt -w ./...; \
	else \
		echo "gofmt not found; skipping"; \
	fi

validate:
	python3 scripts/validate_schemas.py

test: validate

lint:
	@echo "No linter configured yet."
	@exit 0
