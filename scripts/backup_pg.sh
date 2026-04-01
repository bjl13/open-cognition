#!/usr/bin/env bash
# backup_pg.sh — dump the Postgres database to a compressed SQL file.
#
# Creates a timestamped .sql.gz in backups/ suitable for disaster recovery.
# Does not stop the database; pg_dump takes a consistent snapshot.
#
# Usage:
#   ./scripts/backup_pg.sh
#   COMPOSE_FILE=docker-compose.yml ./scripts/backup_pg.sh
#
# Output:
#   backups/cognition_<timestamp>.sql.gz
#
# Restore:
#   gunzip -c backups/cognition_<timestamp>.sql.gz \
#     | docker compose exec -T postgres psql -U cognition -d cognition
#
# Requirements: docker compose

set -euo pipefail

OUTDIR="backups"
TIMESTAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ | tr ':' '-')
OUTFILE="${OUTDIR}/cognition_${TIMESTAMP}.sql.gz"

mkdir -p "${OUTDIR}"

echo "Backing up Postgres → ${OUTFILE}"

docker compose exec -T postgres \
    pg_dump -U cognition -d cognition --no-password \
    | gzip > "${OUTFILE}"

size=$(du -sh "${OUTFILE}" | cut -f1)
echo "Backup complete: ${OUTFILE} (${size})"
echo ""
echo "Restore with:"
echo "  gunzip -c ${OUTFILE} | docker compose exec -T postgres psql -U cognition -d cognition"
