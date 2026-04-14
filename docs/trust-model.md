# Trust Model

Open-Cognition does not evaluate whether an agent is *right*. It records who
said what, and makes that record tamper-evident. Trust is something an external
reader decides after the fact — it is not a property the substrate computes.

This document describes the identity, signing, and verification primitives the
control plane actually enforces, and the human judgement calls it deliberately
does not make.

---

## Identities

Three kinds of actor touch the system.

### Human operators

- Authenticated by the pre-shared `CONTROL_API_KEY`.
- Identified by the `X-Actor` request header (e.g. `human:bjl13`).
- Can perform any operation, including `POST /stop` and `POST /resume`.
- No cryptographic identity in v0. The API key is the boundary.

### Agents

- Authenticated by the same `CONTROL_API_KEY` at the HTTP layer.
- Additionally identified by an `agent_id` string and bound to an Ed25519
  keypair via the TOFU registry (see below).
- Can create canonical objects and references. Cannot change system mode
  (there is no access control that prevents this in v0, but policy-level
  restriction is a Phase 9+ concern).

### The system itself

- The string `system:init` is used for the single seed row in the audit log
  and system state table.
- No action during normal runtime uses this identity. If a row is attributed
  to `system:init` after initialisation, something has been tampered with.

---

## Authentication Boundary

There is one secret today: `CONTROL_API_KEY`.

- Loaded from the environment by the control plane.
- Every write endpoint requires it in the `Authorization` header.
- Read endpoints (`GET /status`, `/canonicals`, `/references`, `/audit`,
  `/reconcile`) are currently open on the local network. The dashboard
  depends on this.

The API key is a **trust boundary, not an identity**. It proves the caller
is inside the operator's network. It does not prove *which* agent or human
is acting. That is what `X-Actor` and `agent_id` + signature are for.

Rotate the API key by editing the environment and restarting the control
plane. All running agents must receive the new key out-of-band.

---

## TOFU Key Registry for Agents

Every agent reference must be signed.

### The signed message

```
{ref_id}:{canonical_object_id}:{agent_id}:{created_at}
```

Four fields, colon-joined, UTF-8 bytes. Signed with Ed25519. The signature
and the public key (both base64, raw 64-byte and 32-byte respectively)
travel with the reference in the request body.

### First contact (registration)

When a previously unseen `agent_id` submits a valid reference:

1. The control plane verifies the signature against the submitted public key.
2. The `agent_keys` row is created: `(agent_id, public_key, registered_at, first_ref_id)`.
3. The reference is accepted.

This is deliberate: no out-of-band registration step. An agent proves it owns
a keypair by using it.

### Subsequent submissions

- The control plane looks up the registered public key for `agent_id`.
- If the submitted key matches: signature is verified, reference is accepted.
- If the submitted key does not match: 422 with a hint directing the operator
  to either resubmit with the original key or update `agent_keys` manually.

### Trust on First Use — the tradeoff

TOFU means the first submission defines the identity. An attacker who reaches
the API **before the legitimate agent does** can register a key under that
`agent_id`. This is acceptable for a substrate for three reasons:

1. The API key still gates reaching the endpoint at all.
2. The first reference is itself part of the audit log. A stolen identity
   leaves a trace at the moment of theft.
3. Operators rotate or revoke by editing the `agent_keys` table directly.
   The control plane does not mediate this — intentional sharp edge.

If a use case needs stronger guarantees, register keys through a signed
migration before first contact.

### What signatures protect

A valid signature proves:

- The holder of the registered private key authored this reference.
- The four signed fields have not been altered in transit.

A valid signature does **not** prove:

- The `context`, `relevance`, `trust_weight`, or any other unsigned field
  was not substituted. v0 signs identity + timestamp only. Full-payload
  signing is future work.
- The agent is honest. It only proves the agent is itself.

---

## Canonical Object Integrity

Canonical objects have no signature. They do not need one.

- The ID is the SHA-256 of the payload.
- The control plane recomputes the hash on every submission.
- If the claimed ID and the actual hash disagree, the submission is rejected.

An object is its content. Tampering produces a different object, not a
corrupted one. There is nothing to sign.

The creator of the object is recorded in `created_by` and the audit log.
That attribution is covered by the API key trust boundary, not by a
cryptographic proof. Upgrading `created_by` to a signed field is future
work if needed.

---

## Audit Log

The audit log is append-only at the application level:

- No `UPDATE` or `DELETE` statements touch it.
- Every write endpoint emits an entry.
- Every entry carries `actor`, `action`, `target_id`, `target_type`, and a
  JSON `detail` blob.

The log is **not currently cryptographically chained** (no Merkle tree,
no hash-linked entries). A sufficiently privileged database user can still
rewrite history. The append-only property is a convention enforced by the
control plane, not by the database engine.

Adding hash chaining is tracked as future work in the roadmap. It is the
obvious next step for making the log tamper-evident rather than just
tamper-discouraging.

---

## What This Model Does Not Do

- No multi-party key ceremony or HSM integration.
- No revocation list. A compromised agent key is handled by editing
  `agent_keys` directly.
- No quorum or consensus on canonical object creation. One actor, one write.
- No agent-to-agent authentication. Agents talk to the control plane, not
  to each other.
- No trust score computed from reference weights. `trust_weight` is a
  field agents write; the system never reads or aggregates it.

These are expansions. Trust in v0 is: strong identity for agents, strong
content integrity for objects, attribution for everything, and a human
operator in the loop.
