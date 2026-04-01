#!/usr/bin/env bash
# reconcile_storage.sh — check that every ledger-recorded canonical object
# exists in object storage.
#
# Calls GET /reconcile on the control plane, which iterates all canonical
# objects in the ledger and verifies each storage path via HEAD request.
# Returns a JSON summary. Exit code 1 if any objects are missing in storage.
#
# Usage:
#   ./scripts/reconcile_storage.sh
#   CONTROL_PLANE=http://my-host:8080 ./scripts/reconcile_storage.sh
#
# Requirements: curl, jq

set -euo pipefail

CONTROL_PLANE="${CONTROL_PLANE:-http://localhost:8080}"

result=$(curl -sf "${CONTROL_PLANE}/reconcile")
echo "${result}" | jq .

missing=$(echo "${result}" | jq '.missing_in_storage | length')
if [ "${missing}" -gt 0 ]; then
    echo ""
    echo "WARNING: ${missing} object(s) missing from storage. Manual reconciliation required." >&2
    exit 1
fi
