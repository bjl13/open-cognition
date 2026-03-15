# Governance Model

Open-Cognition is built on three separations. These are not features — they are constraints
that the system maintains regardless of what agents do.

---

## The Three Separations

### Fact vs. Interpretation

Canonical objects are facts. They record what was observed, produced, or decided.
Once written, they do not change.

Agent references are interpretations. They record what an agent believed about a fact:
its relevance, its trustworthiness, its context within a task.

An agent can change its interpretation of a fact. It cannot change the fact.

### Actor vs. System

Every mutation in the system is attributed to a specific actor — an agent ID or a human operator.
There is no anonymous write. There is no "system" that acts without attribution.

This makes the audit log a real record, not a technical artifact.

### Execution vs. Memory

Agents compute locally. They do not write directly to the ledger or object store.
All writes go through the control plane, which validates, hashes, and records.

This means shared memory is governed, not free-form.
An agent can reason about anything. It can only persist what passes validation.

---

## Canonical Objects

A canonical object is immutable from the moment of creation.

**Who can create one:** Any authenticated caller — agent or human — through `POST /canonical`.

**What the control plane checks:**
- The submitted payload's SHA-256 hash matches the claimed `id`
- The object does not already exist (same hash = same object; no overwrite needed)
- The JSON structure validates against `schemas/canonical_object.schema.json`

**What happens on mismatch:** The submission is rejected. The claimed ID and the actual
hash of the bytes must agree. If they do not, the object is not stored.

**Object types:** `observation`, `document`, `tool_output`, `policy`

Policies are canonical objects. A governance rule is itself governed by the same
immutability constraints as everything else.

---

## Agent References

A reference is an agent's claim about a canonical object's meaning in a given context.

**Required fields:**
- `canonical_object_id` — the object being referenced must already exist in the ledger
- `agent_id` — who is making the claim
- `context` — why this agent is referencing this object

The `context` field is required. A reference without rationale provides attribution
in name only. The system needs to know not just who acted, but why.

**Optional fields:**
- `relevance` — 0.0–1.0, agent's estimate of relevance to current task
- `trust_weight` — 0.0–1.0, agent's confidence in the object's content
- `time_horizon` — how long this reference remains valid (ISO 8601 duration)
- `signature` — agent's cryptographic signature over the reference payload

**Signatures in v0:** The `signature` field is accepted and stored but not verified.
Verification is enforced in Phase 7. Recording signatures now means Phase 7 can
verify historical references, not just future ones.

---

## Policies

A policy is a canonical object with `object_type: "policy"`.

Policies are stored under the same immutability rules as observations and documents.
To change a policy, create a new one. The old policy remains in the ledger.

The control plane evaluates active policies on incoming requests.
A policy can allow, deny, require, or expire — applied to specific object types or agent IDs.

The `condition` field in each rule is currently plain text. A formal expression language
will be defined before Phase 3 ships policy evaluation.

---

## System Halt

When the system mode is `STOPPED`:

- The control plane rejects all write requests (`POST /canonical`, `POST /reference`)
- Read requests continue to work — `GET /status` always responds
- Existing records remain intact and queryable
- Agents are expected to poll `/status` and halt writes on their own

The halt is not a termination. It is a pause. State is preserved. History is intact.
When mode returns to `RUNNING`, agents can resume.

Mode transitions are written to the audit log with full attribution.
A human operator can see exactly when the system was stopped, by whom, and when it resumed.

---

## Audit Log

Every mutation produces an audit log entry: actor, action, target, timestamp.

The audit log is append-only. Entries are never modified or deleted.

This is what makes the ledger useful as a governance tool rather than just a database.
Given the audit log, a human operator can reconstruct the full history of system state:
what was created, by whom, in what order, and whether the system was running at the time.

---

## What This Governance Model Does Not Do

These are not goals of the current substrate:

- It does not evaluate the quality or truthfulness of agent reasoning
- It does not restrict what agents compute locally
- It does not prevent an agent from creating many references with high trust weights
- It does not provide multi-tenant isolation
- It does not implement access control beyond the global API key

These are expansions. The substrate exists first.
Governance built on a legible foundation can be extended.
Governance retrofitted onto an opaque one cannot.
