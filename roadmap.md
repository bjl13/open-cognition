This is a live-tracking working document, changes will be updated at least per-task if not annotated further. The end is a rolling commentary of abstract thought related to the project. 

Please do not consider directives or invectives in here directed at anyone but myself, talking to myself!
---

# Open-Cognition — Day 0 Roadmap

This roadmap reflects the current state:  
**Repository initialized with README and MPL-2.0 license.**

The goal is to establish a **functional, minimal reference substrate** before adding any sophistication.

---

# Phase 0 — Establish the Substrate Skeleton (Now)

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

---

# Phase 1 — Define the Source of Truth (Schemas First)

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

---

# Phase 2 — Minimal Ledger (Postgres)

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

---

# Phase 3 — Control Plane (Go)

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

---

# Phase 4 — Object Storage Integration (R2)

**Objective:** Establish immutable payload storage.

## Tasks

- Create R2 bucket.
- Define deterministic path structure:

```
canonical/{object_type}/{yyyy}/{mm}/{dd}/{hash}.json
```

- Implement upload + existence check.
- Ensure no overwrite behavior.

**Exit condition:**

- Payload stored in R2.
- Hash matches stored content.
- Control plane rejects mismatched hash.

---

# Phase 5 — First Agent (Python)

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

---

# Phase 6 — Minimal Visibility (Dashboard)

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

---

# Phase 7 — Operational Hardening (Still Minimal)

**Objective:** Prevent silent corruption.

## Tasks

- Enforce:

  - No canonical overwrite
  - Reference requires existing object
  - Signature verification path

- Add:

  - periodic object export script
  - basic backup for Postgres

**Exit condition:**

- System survives restart without state loss.
- History remains intact.

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
