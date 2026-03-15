# Architecture

Open-Cognition is four components. Each has a single job.

---

## Components

### Object Store
Stores payload bytes. S3-compatible (Cloudflare R2 in production, MinIO locally).

Objects are written once. They are never modified or deleted through normal operation.
The storage path is derived from the object's content hash:

```
canonical/{object_type}/{yyyy}/{mm}/{dd}/sha256:{hex}.json
```

If the same bytes are submitted twice, the path resolves to the same location.
No overwrite occurs. No duplicate is created.

### Reference Ledger
A Postgres database. Four tables:

| Table | Purpose |
|---|---|
| `canonical_objects` | One row per canonical object: id, type, path, who submitted it, when |
| `agent_references` | One row per agent reference: which object, which agent, context, weights |
| `system_state` | Current mode (RUNNING or STOPPED) and the event that last changed it |
| `audit_log` | Append-only record of every mutation: actor, action, target, timestamp |

The ledger is the ground truth for attribution and history.
Object storage holds the payloads. The ledger holds the record of what exists and why.

### Control Plane
A Go HTTP service. Five endpoints:

```
GET  /status      — current system mode and ledger summary
POST /stop        — set mode to STOPPED; all writes cease
POST /resume      — set mode to RUNNING
POST /canonical   — submit a new canonical object
POST /reference   — submit a new agent reference
```

The control plane validates schemas, verifies content hashes, and writes the audit log.
It rejects requests when the system is STOPPED.

Authentication uses a pre-shared API key (env: `CONTROL_API_KEY`).
The auth layer is a middleware interface — replacing it with mTLS in Phase 7 requires
no changes to route handlers.

### Agents
Python processes that read canonical objects and emit references.

An agent's responsibilities:
1. Poll `/status` before any write attempt
2. Halt mutation if the system is STOPPED
3. Submit references with a `context` field — attribution without rationale is not attribution

Agents do not write directly to the ledger or object store.
All writes go through the control plane.

---

## Data Flow

```
Agent
  │
  ├─ GET /status ──────────────────────── Control Plane
  │                                            │
  └─ POST /canonical  ────────────────────────►│
       payload bytes                           │── hash(payload) → id
                                               │── validate schema
                                               │── write to Object Store ──► R2 / MinIO
                                               │── write to Ledger ────────► Postgres
                                               │── write to audit_log
                                               │
  └─ POST /reference  ────────────────────────►│
       {canonical_object_id, context, ...}     │── verify object exists in Ledger
                                               │── validate schema
                                               │── write to Ledger
                                               │── write to audit_log
```

---

## Content Addressing

A canonical object's identity is its content.

The ID is computed as:

```
id = "sha256:" + hex(sha256(payload_bytes))
```

The control plane computes this on receipt and rejects any submission where
the claimed ID does not match the actual hash of the submitted bytes.

This means:
- The same content always produces the same ID
- Storing the ID is storing a verifiable claim about the content
- Any modification to the content produces a different ID — a new object, not an edit

---

## System Lifecycle

The system has two modes.

**RUNNING** — normal operation. Writes are accepted from authenticated callers.

**STOPPED** — mutation suspended. The control plane rejects all write requests
(`POST /canonical`, `POST /reference`). Read requests (`GET /status`) continue to work.
Agents are expected to poll status and halt writes on their own — the control plane
does not terminate agent processes.

Mode transitions are recorded in the audit log with actor identity and timestamp.

---

## Local Development

```
make up    # starts Postgres + MinIO, waits for both health checks
make down  # stops all services
make test  # validates schemas against example files
```

Services when running:

| Service | Address |
|---|---|
| Postgres | `localhost:5432` |
| MinIO API | `localhost:9000` |
| MinIO Console | `http://localhost:9001` |

Copy `.env.example` to `.env` and set `CONTROL_API_KEY` before running the control plane.
