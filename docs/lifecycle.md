# Lifecycle

Three lifecycles coexist in Open-Cognition. They are separate on purpose.

1. **System mode** — is the substrate accepting writes?
2. **Canonical objects** — created, then frozen.
3. **Agent references** — created, then frozen. But superseded freely.

Understanding these separately is how you reason about the system without
reaching for a more complicated model.

---

## System Mode

The `system_state` table holds exactly one row. Its `mode` column is either
`RUNNING` or `STOPPED`.

### Transitions

```
        ┌──────────────┐   POST /stop     ┌──────────────┐
        │              │ ───────────────► │              │
        │   RUNNING    │                  │   STOPPED    │
        │   (default)  │ ◄─────────────── │              │
        └──────────────┘   POST /resume   └──────────────┘
```

- Seeded `RUNNING` by `migrations/001_initial.sql`.
- `POST /stop` flips to `STOPPED`, writes an audit log entry with the actor.
- `POST /resume` flips back to `RUNNING`, writes an audit log entry.
- Both transitions are idempotent: stopping a stopped system is a no-op that
  still produces an audit entry, which is intentional (it records the intent).

### What each mode guarantees

| Guarantee | RUNNING | STOPPED |
|---|---|---|
| `POST /canonical` accepted | ✓ | ✗ (503) |
| `POST /reference` accepted | ✓ | ✗ (503) |
| `GET /status` responds | ✓ | ✓ |
| `GET /canonicals`, `/references`, `/audit`, `/reconcile` respond | ✓ | ✓ |
| Existing data readable | ✓ | ✓ |
| Audit log appends (mode transitions) | ✓ | ✓ |

`STOPPED` is a write halt, not a freeze. Reads continue. Operators and
inspectors can do their work while the system is paused.

### What agents are expected to do

Agents poll `GET /status` before each write. If they see `STOPPED`:

1. Finish any in-memory reasoning safely.
2. Do not attempt to `POST /reference` or `POST /canonical` — the server
   will reject with 503 anyway, but polite agents don't retry until they
   observe `RUNNING`.
3. Resume normal polling when mode flips back.

This is a convention, not a technical constraint. The control plane cannot
stop a misbehaving agent from retrying; it can only reject the writes.

---

## Canonical Object Lifecycle

```
   submitted ──►  validated  ──►  stored  ──►  recorded
                      │                           │
                      │    (immutable forever)    │
                      └───────────────────────────┘
```

### Phases

1. **Submitted**: Caller POSTs JSON to `/canonical` with `id` (sha256:…),
   `storage_path`, `payload` (base64), metadata.
2. **Validated**: Control plane checks schema version, ID format, object
   type, size, storage path format, hash-matches-payload, and storage path
   determinism.
3. **Stored**: Payload written to the object store at `storage_path`. If
   the write succeeds but the ledger insert fails, the object is orphaned
   in storage. `GET /reconcile` / `make reconcile` detects this.
4. **Recorded**: Row inserted into `canonical_objects`; audit log appends
   a `create_canonical` entry.

### Post-creation

Canonical objects never change. There is no `PUT /canonical`, no
`DELETE /canonical`, and no operator workflow for editing one.

**What you do instead:** If the content was wrong, submit a new canonical
object with the corrected bytes. Emit references pointing to the new ID.
The old object remains — deleting it would silently rewrite history.

If the original content must actually be removed (GDPR, security incident),
operate at the database level. That is an intentional sharp edge: it
requires a human with direct SQL access and leaves no audit trail inside
the substrate. External operational controls are responsible for recording
the reason.

---

## Agent Reference Lifecycle

```
   emitted ──►  signed  ──►  verified  ──►  recorded
                                               │
                  (immutable; superseded by    │
                  newer references)            ▼
                                       referenced / ignored
```

### Phases

1. **Emitted**: Agent constructs a reference object locally — UUID v4,
   target `canonical_object_id`, `agent_id`, `context`, optional
   `relevance`, `trust_weight`, `time_horizon`.
2. **Signed**: Agent signs `{id}:{canonical_object_id}:{agent_id}:{created_at}`
   with its Ed25519 private key. Attaches `signature` and `public_key`
   (both base64).
3. **Verified**: Control plane validates schema, confirms the canonical
   object exists, verifies signature against the submitted public key,
   checks/registers the key against `agent_keys` (TOFU).
4. **Recorded**: Row inserted into `agent_references`; audit log appends
   a `create_reference` entry.

### Supersession, not update

References are immutable like canonical objects. An agent that changes
its mind emits a **new** reference to the same object with different
`relevance` / `trust_weight` / `context`.

Consumers reading references must decide how to resolve multiple
references from the same agent to the same object. The substrate does
not pick a winner. Typical strategies:

- "Latest wins": sort by `created_at` descending, take the first.
- "Weighted": aggregate using `trust_weight`, `relevance`.
- "All visible": present every reference and let the human choose.

None of these are encoded in the control plane. They are consumer logic.

### Time horizon and expiry

A reference may carry `time_horizon` (ISO 8601 duration or RFC 3339
timestamp). The substrate stores it. It does **not** automatically expire
references or hide stale ones. Expiry is consumer logic.

This is intentional: hiding data based on a timestamp is a form of silent
mutation. The substrate refuses to do it. A reader sees all references
and applies their own horizon policy.

---

## Audit Log Lifecycle

Every write to the substrate appends one row to `audit_log`:

| Write action | Audit entry |
|---|---|
| `POST /stop` | `actor`, `action=stop`, `target_type=system_state` |
| `POST /resume` | `actor`, `action=resume`, `target_type=system_state` |
| `POST /canonical` | `actor=created_by`, `action=create_canonical`, `target_id=<hash>` |
| `POST /reference` | `actor=agent_id`, `action=create_reference`, `target_id=<uuid>` |

Rows are append-only at the application level: no handler calls `UPDATE`
or `DELETE` on `audit_log`. A DBA with direct SQL access could still
tamper with the table — the substrate does not protect against that in v0.
See `docs/threat-model.md`.

### Reading the log

`GET /audit?limit=N` returns recent entries, newest first. The dashboard
and operational tools consume this endpoint. There is no filter DSL —
filtering happens client-side or via direct SQL access.

---

## Operational Lifecycle

A deployed Open-Cognition instance goes through a predictable cycle:

1. **Bring up**: `make up` (starts Postgres, MinIO, control plane).
2. **Migrate**: `make migrate` (applies 001 and 002 SQL).
3. **Smoke test**: `make smoke` (round-trip + duplicate rejection).
4. **Run**: agents and humans operate.
5. **Back up**: `make backup` (pg_dump), `make export` (NDJSON canonicals).
6. **Reconcile**: `make reconcile` (every ledger entry exists in storage).
7. **Halt**: `POST /stop` before destructive operational work.
8. **Resume**: `POST /resume` once confirmed safe.
9. **Tear down**: `make down`.

Steps 5–6 should run on a schedule. The substrate does not schedule them —
cron, systemd timers, or a CI job is how an operator wires this up.
Scheduling is deliberately out of scope for v0.
