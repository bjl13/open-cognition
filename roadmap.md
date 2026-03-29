This is a live-tracking working document, changes will be updated at least per-task if not annotated further. The end is a rolling commentary of abstract thought related to the project.

Please do not consider directives or invectives in here directed at anyone but myself, talking to myself!
---

# Open-Cognition — Day 0 Roadmap

This roadmap reflects the current state:
**Phases 0–6 complete. Phase 7 in scope.**

The goal is to establish a **functional, minimal reference substrate** before adding any sophistication.

---

# Phase 0 — Establish the Substrate Skeleton ✓

**Objective:** Make the repo legible and structurally real.

## Tasks

- Create base directory structure:

```
schemas/
examples/
docs/
migrations/
cmd/control-plane/
internal/
agents/
dashboard/static/
storage/
```

- Add placeholder files:

```
schemas/canonical_object.schema.json
schemas/agent_reference.schema.json
schemas/policy.schema.json
```

- Add:

```
docs/architecture.md
docs/governance-model.md
```

- Add:

```
Makefile
docker-compose.yml (empty or stub)
```

**Exit condition:**

- Repo communicates intent by structure alone.
- Another engineer can infer system shape without explanation.

**Status:** Complete. Docs written. Schemas and examples in place.

---

# Phase 1 — Define the Source of Truth (Schemas First) ✓

**Objective:** Lock the data model before writing behavior.

## Tasks

- Finalize JSON schemas:

  - Canonical Object
  - Agent Reference
  - Policy

- Add schema version field:

```
"schema_version": "0.1.0"
```

- Create example instances:

```
examples/canonical_object_example.json
examples/agent_reference_example.json
```

- Validate schemas locally.

**Exit condition:**

- `/schemas` folder is understandable without reading code.
- Example objects pass validation.

**Status:** Complete. `make validate` passes. Policy schema included beyond original scope.

---

# Phase 2 — Minimal Ledger (Postgres) ✓

**Objective:** Establish durable, queryable history.

## Tasks

Create initial migration:

```
migrations/001_initial.sql
```

Tables:

- canonical_objects
- agent_references
- system_state
- audit_log

Add docker service:

- postgres

**Exit condition:**

- Database boots via docker compose.
- Tables exist.
- Can manually insert and query rows.

**Status:** Complete. `make migrate` is idempotent for a fresh database. All four tables with correct indices.

---

# Phase 3 — Control Plane (Go) ✓

**Objective:** Create the smallest possible governance layer.

## Initial Endpoints

```
GET  /status
POST /stop
POST /resume
POST /canonical
POST /reference
```

## Tasks

- Implement schema validation.
- Verify object hash.
- Enforce append-only behavior.
- Write audit log entries.

**Exit condition:**

- Can create canonical object via API.
- Can create agent reference.
- Can stop system mode.

**Status:** Complete. All five endpoints live. Full hash verification, size verification, and storage-path verification on every POST /canonical. Referential integrity enforced on POST /reference.

**Known debt:**

- `internal/pg` is a temporary stdlib-only PostgreSQL wire-protocol driver. Written because pgx/v5 requires Go 1.25, which was unavailable in the offline build environment. Migration path documented in `internal/pg/pg.go`. When Go 1.25 + network are available: `go get github.com/jackc/pgx/v5@latest`, rewrite `internal/db`, delete `internal/pg`, remove `POSTGRES_HOST_AUTH_METHOD=md5`, bump Dockerfile to `golang:1.25-alpine`.
- MD5 auth only (no SCRAM-SHA-256). Required by the temp driver; removed when pgx replaces it.

---

# Phase 4 — Object Storage Integration ✓

**Objective:** Establish immutable payload storage.

## Tasks

- Create storage bucket.
- Define deterministic path structure:

```
canonical/{object_type}/{yyyy}/{mm}/{dd}/{hash}.json
```

- Implement upload + existence check.
- Ensure no overwrite behavior.

**Exit condition:**

- Payload stored in object storage.
- Hash matches stored content.
- Control plane rejects mismatched hash.

**Status:** Complete. Implemented against MinIO (S3-compatible). Switch to Cloudflare R2 or any S3-compatible store by setting `STORAGE_ENDPOINT`, `STORAGE_ACCESS_KEY_ID`, `STORAGE_SECRET_ACCESS_KEY`, `STORAGE_REGION`. No code change required. SigV4 signing implemented from scratch; no SDK dependency.

**Known debt:**

- Write ordering is storage-first. If the Postgres insert fails after a successful `PutObject`, an orphaned object remains in storage. The subsequent attempt returns 409 (storage duplicate check catches it). A reconciliation process to re-drive the DB insert from storage is deferred to Phase 7.
- `make smoke` verifies the full round-trip including duplicate rejection (409).

---

# Phase 5 — First Agent (Python) ✓

**Objective:** Prove multi-actor interaction.

## Tasks

- Create minimal agent:

  - Poll `/status`
  - Read object
  - Emit reference

- Implement signing stub (even if local key).

**Exit condition:**

- Agent creates valid reference.
- Ledger records actor attribution.
- Agent halts when system STOPPED.

**Status:** Complete. `agents/observer/` is a production-quality async Python agent (httpx, cryptography, structlog).

Observation dispatch (OBSERVE_TARGET env var):
- `https?://...` — fetch URL, record status/headers/body
- file path — read file, record content + stat metadata
- unset — collect environment snapshot (platform, memory, disk, load, network, sanitised env vars)

Ed25519 key loaded from `AGENT_PRIVATE_KEY` env (base64 raw seed) or generated ephemerally with public key logged at startup. Signatures are advisory; control plane does not yet verify them (Phase 7).

Agent checks `/status` before each cycle and skips writes when STOPPED. Resumes automatically.

---

# Phase 6 — Minimal Visibility (Dashboard) ✓

**Objective:** Human-readable state inspection.

## Tasks

- Static TypeScript dashboard.
- Read-only views:

  - System mode
  - Canonical objects
  - Recent references

- Ship compiled assets only.

**Exit condition:**

- No Node required at runtime.
- Operator can see system state in browser.

**Status:** Complete. TypeScript source in `dashboard/src/`. Compiled output committed to `dashboard/static/`. Served by the control plane at `http://localhost:8080/`.

Three new read endpoints added to the control plane:
- `GET /canonicals?limit=N&offset=M`
- `GET /references?limit=N&offset=M`
- `GET /audit?limit=N`

Dashboard shows: system mode (live status indicator), canonical objects table, agent references table, audit log. Auto-refreshes every 30 seconds. Rebuild with `make dashboard` (requires `tsc`). No Node required at runtime.

---

# Phase 7 — Operational Hardening (Next)

**Objective:** Prevent silent corruption.

## Tasks

- Enforce:

  - No canonical overwrite ✓ (already enforced since Phase 4)
  - Reference requires existing object ✓ (already enforced since Phase 3)
  - Signature verification path

- Add:

  - periodic object export script
  - basic backup for Postgres

**Exit condition:**

- System survives restart without state loss.
- History remains intact.

**Remaining work:**

1. **Signature verification** — POST /reference currently accepts any signature string without verifying it. Add Ed25519 public-key registry (agent_id → public_key) and verify `signature` against `{ref_id}:{canonical_object_id}:{agent_id}:{created_at}` before inserting. Enforcement is the Phase 7 gate; advisory period ends here.

2. **Object export script** — `scripts/export_canonicals.sh` or Go binary that reads all canonical objects from the ledger and writes them to a local archive. Run periodically (cron or docker-compose scheduled task).

3. **Postgres backup** — `scripts/backup_pg.sh` wrapping `pg_dump`. Writes timestamped SQL dumps to a local volume. Documented restore procedure.

4. **Storage/DB orphan reconciliation** — scan storage for objects not in the ledger and re-drive the DB insert. Cleans up from any storage-first write failures.

---

# Phase 8 — Documentation for External Adoption

**Objective:** Make the substrate understandable without you.

## Tasks

Write:

- docs/trust-model.md
- docs/lifecycle.md
- docs/threat-model.md

Add:

- architecture diagram
- data flow diagram

**Exit condition:**

- A new reader can understand the system by reading `/docs` + `/schemas`.

---

# Non-Goals (Until After Phase 8)

Do **not** add yet:

- Vector databases
- Agent planners
- Streaming/event buses
- Complex trust algorithms
- Multi-tenant features
- gRPC or service mesh

These do not improve the substrate.

---

# Success Definition (v0)

The project is functionally real when someone can:

1. Start the stack with:

```
make up
```

2. Create a canonical object.
3. Attach an agent reference.
4. Trigger system stop.
5. Observe that writes cease.
6. Query ledger and reconstruct what happened.

At that point, the substrate exists.

Everything beyond that is expansion, not foundation.

**Current state:** All six steps work today. The observer agent runs step 2–3 automatically. The dashboard shows steps 4–6 without a terminal.

---

# Technical Debt Register

| Item | Phase introduced | Blocking | Resolution |
|------|-----------------|----------|------------|
| Temporary stdlib Postgres driver (`internal/pg`) | 3 | No | Replace with pgx when Go 1.25 + network available. 5-step migration documented in `internal/pg/pg.go`. |
| MD5 auth only (no SCRAM-SHA-256) | 3 | No | Removed when pgx replaces `internal/pg`. |
| Ed25519 signatures advisory (not verified) | 5 | Phase 7 | Add public-key registry and signature verification to POST /reference. |
| Storage-first write ordering (orphan risk) | 4 | No | Orphans detectable via storage duplicate check. Reconciliation script in Phase 7. |

---

# Notes to Self

Boring systems survive.

The control plane is the invariant. Everything else — agents, dashboards, policies — is pluggable. The invariant is: a canonical object, once written, is never changed. That's the only promise that has to hold.

The stdlib Postgres driver was a deliberate tradeoff. Staying offline and dependency-free during early phases meant the system could be built and verified without a network, without module mirrors, without version negotiation. The cost is MD5 auth and no prepared statements. Both are acceptable for a substrate that hasn't reached production yet. When the build environment has Go 1.25 and a network, the replacement is straightforward.

The observer agent does more than the spec required — URL fetch, file read, and environment snapshot. That's because "observe whatever it's pointed at" is the right abstraction. A more constrained agent would need rewriting the moment the use case changed.

The dashboard is served by the control plane intentionally. One fewer moving part. One fewer port to configure. The operator opens a browser to the same address they already use for the API.
