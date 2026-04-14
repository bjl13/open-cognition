# Data Flow Diagrams

Four sequences cover every write path through the substrate. All reads are
straightforward (`SELECT` from Postgres, with the exception of `/reconcile`
which also HEAD-checks storage) and are not diagrammed.

---

## 1. Create a Canonical Object

```mermaid
sequenceDiagram
  autonumber
  participant C as Caller (agent or human)
  participant CP as Control Plane
  participant PG as Postgres (ledger)
  participant S3 as Object Store

  C->>CP: POST /canonical<br/>{schema_version, id, object_type,<br/>  size_bytes, created_at, created_by,<br/>  storage_path, payload(base64)}
  CP->>CP: guardStopped()<br/>(403 if STOPPED)
  CP->>CP: validate schema, regex, sizes
  CP->>CP: sha256(payload) == id ?
  CP->>CP: storage_path == canonical/{type}/{yyyy/mm/dd}/{id}.json ?
  CP->>PG: SELECT 1 FROM canonical_objects WHERE id=$1
  PG-->>CP: not found
  CP->>S3: HEAD storage_path
  S3-->>CP: 404
  CP->>S3: PUT storage_path (payload bytes)
  S3-->>CP: 200
  CP->>PG: INSERT INTO canonical_objects ...
  PG-->>CP: ok
  CP->>PG: INSERT INTO audit_log (actor, create_canonical, id)
  CP-->>C: 201 Created<br/>CanonicalObject (no payload)
```

**Failure branch**: if the PG insert fails *after* the S3 PUT, the object
is orphaned in storage. The next submission with the same hash hits the
409 in the HEAD check and surfaces an "inspect and reconcile manually"
error. `GET /reconcile` walks the ledger and reports storage mismatches.

---

## 2. Create an Agent Reference

```mermaid
sequenceDiagram
  autonumber
  participant A as Agent
  participant CP as Control Plane
  participant PG as Postgres (ledger)

  A->>A: sign({id}:{coid}:{agent_id}:{created_at})<br/>with Ed25519 private key
  A->>CP: POST /reference<br/>{ref fields, signature, public_key}
  CP->>CP: guardStopped()
  CP->>CP: validate schema (UUID v4, sha256:*, context required)
  CP->>PG: SELECT 1 FROM canonical_objects WHERE id=$1
  PG-->>CP: found
  CP->>CP: base64-decode sig + pubkey; verify Ed25519
  CP->>PG: SELECT public_key FROM agent_keys WHERE agent_id=$1
  alt new agent
    PG-->>CP: not found
    CP->>PG: INSERT agent_keys (agent_id, public_key, first_ref_id)<br/>TOFU registration
  else existing agent
    PG-->>CP: stored_key
    CP->>CP: submitted_key == stored_key ?<br/>(reject 422 if not)
  end
  CP->>PG: INSERT INTO agent_references ...
  CP->>PG: INSERT INTO audit_log (actor=agent_id, create_reference, ref_id)
  CP-->>A: 201 Created<br/>AgentReference
```

**Key property**: signature verification happens *before* the database
writes. A valid signature is a prerequisite for any row to exist.

---

## 3. System Halt

```mermaid
sequenceDiagram
  autonumber
  participant Op as Operator
  participant CP as Control Plane
  participant PG as Postgres

  Op->>CP: POST /stop<br/>X-Actor: human:<id>
  CP->>PG: UPDATE system_state SET mode='STOPPED', changed_by, changed_at
  PG-->>CP: ok
  CP->>PG: INSERT INTO audit_log (actor, action=stop, target_type=system_state)
  CP-->>Op: 200 {mode: STOPPED, changed_by, changed_at}

  Note over CP: Subsequent POST /canonical or POST /reference<br/>immediately returns 503.<br/>GET endpoints continue to respond.

  Op->>CP: POST /resume
  CP->>PG: UPDATE system_state SET mode='RUNNING'
  CP->>PG: INSERT INTO audit_log (action=resume)
  CP-->>Op: 200 {mode: RUNNING}
```

---

## 4. Reconciliation

```mermaid
sequenceDiagram
  autonumber
  participant Op as Operator (or cron)
  participant CP as Control Plane
  participant PG as Postgres
  participant S3 as Object Store

  Op->>CP: GET /reconcile
  CP->>PG: SELECT id, storage_path FROM canonical_objects
  PG-->>CP: rows
  loop each row
    CP->>S3: HEAD storage_path
    alt present
      S3-->>CP: 200
    else missing
      S3-->>CP: 404
      CP->>CP: collect into missing_in_storage[]
    end
  end
  CP-->>Op: 200 {checked: N, missing_in_storage: [...]}
  Note over Op: scripts/reconcile_storage.sh<br/>exits 1 if missing_in_storage is non-empty
```

The reverse direction (orphans in storage that are not in the ledger) is
caught by the HEAD check during `POST /canonical` rather than scanned by
`/reconcile`. This is asymmetric by design: the ledger is the source of
truth for what *should* exist.

---

## End-to-End: One Complete Agent Cycle

```mermaid
flowchart LR
  subgraph Cycle["One observer cycle"]
    direction TB
    P[Poll /status] -->|RUNNING| OBS[Observe target<br/>URL / file / env]
    OBS --> BL[Build canonical payload<br/>compute sha256]
    BL --> CO[POST /canonical]
    CO --> MK[Construct reference<br/>sign 4-field message]
    MK --> REF[POST /reference]
    REF --> SL[Sleep POLL_INTERVAL]
    SL --> P
    P -->|STOPPED| HALT[Skip cycle]
    HALT --> SL
  end
```

This is the loop in `agents/observer/`. It is the full happy-path write
surface exercised end-to-end in production.
