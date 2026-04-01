#!/usr/bin/env bash
# export_canonicals.sh — export all canonical objects from the ledger to NDJSON.
#
# Each line of the output file is a JSON object representing one canonical object
# record (metadata only; payload bytes are in object storage at storage_path).
#
# Usage:
#   ./scripts/export_canonicals.sh
#   CONTROL_PLANE=http://my-host:8080 ./scripts/export_canonicals.sh
#
# Output:
#   backups/canonicals_<timestamp>.ndjson
#
# Requirements: curl, jq

set -euo pipefail

CONTROL_PLANE="${CONTROL_PLANE:-http://localhost:8080}"
LIMIT=200
OUTDIR="backups"
TIMESTAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ | tr ':' '-')
OUTFILE="${OUTDIR}/canonicals_${TIMESTAMP}.ndjson"

mkdir -p "${OUTDIR}"

echo "Exporting canonical objects from ${CONTROL_PLANE} → ${OUTFILE}"

offset=0
total=0

while true; do
    batch=$(curl -sf "${CONTROL_PLANE}/canonicals?limit=${LIMIT}&offset=${offset}")

    count=$(echo "${batch}" | jq 'length')

    if [ "${count}" -eq 0 ]; then
        break
    fi

    echo "${batch}" | jq -c '.[]' >> "${OUTFILE}"

    total=$((total + count))
    offset=$((offset + LIMIT))

    echo "  fetched ${total} objects…"

    if [ "${count}" -lt "${LIMIT}" ]; then
        break
    fi
done

echo "Export complete: ${total} objects written to ${OUTFILE}"
