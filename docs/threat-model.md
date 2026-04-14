# Threat Model

Open-Cognition is a substrate, not a product. Its threat model is therefore
narrow: the attacker is modeled against the substrate's own guarantees,
not against the broader system it may be embedded in.

This document describes **what the substrate defends**, **what it does not**,
and the residual risks an operator must handle out-of-band.

---

## Assets

1. **Canonical objects** — the shared, immutable payload bytes and their
   metadata.
2. **Agent references** — per-agent interpretations, cryptographically
   bound to the submitting agent.
3. **Audit log** — the append-only record of who did what.
4. **System mode** — the halt switch.

In decreasing order of consequence if compromised:

- Rewriting the audit log → worst case: history is no longer attributable.
- Inserting a forged canonical object → pollutes shared truth.
- Forging a reference → impersonates an agent's interpretation.
- Flipping system mode → denies service (or permits writes during a halt).

---

## Adversary Model

### In scope

**Network attacker**: sees traffic between agents and the control plane.
The substrate assumes TLS is terminated at an operator-managed boundary
(reverse proxy, mesh). In-cluster traffic is plaintext HTTP by default.

**Compromised agent (post-registration)**: has the Ed25519 private key of
an already-registered `agent_id`. Can sign valid references on its behalf.

**Malicious but authenticated caller**: possesses `CONTROL_API_KEY` (once
enforced) and can reach the control plane, but does not have other secrets.

**Honest-but-buggy agent**: misbehaves accidentally — submits nonsense
`context`, floods references, retries after errors.

### Out of scope

- **Operator-level compromise.** Anyone with direct Postgres or MinIO
  credentials can rewrite ledger entries or delete canonical objects.
  The substrate does not defend against its own database administrator.
  This is intentional — a substrate that tried to defend against its
  operator would need external anchoring (blockchain, notary, etc.),
  which is out of scope for v0.
- **Side-channel attacks** on Ed25519 signing. Agents handle their own
  key material; the substrate only validates signatures.
- **Denial of service at the infrastructure layer**. Kubernetes eviction,
  disk full, network partition, etc. are operator concerns.
- **Malicious schema migrations.** An operator who runs a custom migration
  can rewrite any table. Schemas are trusted.

---

## Defenses Actually Implemented

### Content-addressed immutability

- Every canonical object's ID is `sha256:<hex>` of its payload.
- The control plane recomputes the hash on receipt and rejects mismatches.
- Duplicate submissions are rejected at both the ledger (409) and the
  storage layer (409 — orphaned-object detection).

**Protects against**: payload substitution in transit, silent edits.
**Does not protect against**: submission of *new* forged content under a
new hash — that is indistinguishable from a legitimate observation and
must be evaluated at the content level by consumers.

### Signed references + TOFU key registry

- Every reference requires `signature` (Ed25519) and `public_key`.
- The signed message is `{ref_id}:{canonical_object_id}:{agent_id}:{created_at}`.
- First valid signature from a new `agent_id` registers the public key in
  `agent_keys`. Subsequent signatures from that agent must match.
- Mismatches are rejected with 422 and an operator hint.

**Protects against**: agent impersonation after first contact; tampering
with the four signed fields in transit.
**Does not protect against**: first-contact race (attacker registers first),
signing of unsigned fields (`context`, `relevance`, `trust_weight`),
compromised private keys.

### Append-only enforcement in the application

- No handler emits `UPDATE` or `DELETE` on `canonical_objects`,
  `agent_references`, or `audit_log`.
- Primary key / uniqueness constraints prevent duplicate canonical object
  rows.
- The `system_state` table is a single row with a `CHECK (id = 1)` constraint.

**Protects against**: well-intentioned code accidentally mutating history.
**Does not protect against**: direct SQL, rogue migrations, disk-level edits.

### System halt

- `POST /stop` flips mode to `STOPPED`.
- All write endpoints call `guardStopped` before any work; they return 503.
- Mode transitions are themselves audit-logged.

**Protects against**: continued writes during operator-declared incident.
**Does not protect against**: an agent that ignores `/status` and attempts
writes anyway (it will be rejected, but not silenced); operator-level
bypass via direct DB access.

### Storage/ledger reconciliation

- `GET /reconcile` iterates every ledger row and HEAD-checks object
  storage. Missing payloads are reported.
- Orphan-in-storage case is caught by the 409 duplicate check on the
  next submission with the same hash.

**Protects against**: silent divergence between ledger and storage.
**Does not protect against**: simultaneous tampering of both layers by the
same operator.

---

## Known Residual Risks

| # | Risk | Mitigation | Tracked |
|---|------|------------|---------|
| 1 | `CONTROL_API_KEY` not yet enforced in the router | Run the substrate only on an operator-controlled network until auth middleware ships | roadmap / v1.0 |
| 2 | Audit log not hash-chained | Back up the log via `make backup`; post-hoc tamper detection requires external diff | roadmap |
| 3 | TOFU first-contact race | Preregister keys manually for high-stakes agents before first submission | docs/trust-model.md |
| 4 | Signatures cover identity only, not `context`/`relevance` | Treat unsigned fields as unverified metadata; enhance signing format in a later schema bump | roadmap |
| 5 | No rate limiting | Front with a reverse proxy that enforces limits | operational |
| 6 | Dashboard + read endpoints are currently unauthenticated | Expose only on trusted networks; fronted by auth proxy in production | roadmap |
| 7 | Plaintext HTTP between agents and control plane in-cluster | Terminate TLS at a service mesh or reverse proxy | operational |
| 8 | Direct SQL access equivalents to full trust | Restrict Postgres roles; audit database users separately | operational |
| 9 | Agent private keys stored per-agent | Agents are responsible for their own key hygiene; substrate never sees the private key | agent design |
| 10 | No revocation workflow for compromised keys | Operator edits `agent_keys` directly; substrate does not mediate | docs/trust-model.md |

---

## What Attacks Look Like

Concrete failure modes an operator should recognise:

### "The audit log doesn't match what I remember"

Symptom: rows missing, timestamps edited. Cause: operator-level write to
Postgres (intentional or accidental), or migration that touched audit_log.
Substrate: no defense. External controls (WAL archiving, DB role separation,
external log shippers) are the answer.

### "An agent's references changed their weights overnight"

Impossible through the substrate — references are immutable. If you see
this, someone mutated `agent_references` directly. Treat as an operator
compromise.

### "A canonical object has the 'wrong' content"

Impossible through the substrate — the content hash *is* the ID. If the
content seems wrong, the ID would also be different. If the ID is the
same as before and the content differs, someone has overwritten the object
in the underlying bucket; the storage backend lost durability.

### "Reference submission returns 422: signature verification failed"

Expected. Either the agent signed a different message, uses a different
key than registered, or the request body was altered in transit.

### "Reference submission returns 422: public key mismatch"

Expected. The agent's key has changed since its first registration. If
legitimate (key rotation), an operator must update `agent_keys`.

### "Writes fail with 503 during normal operation"

Expected. The system is `STOPPED`. Either somebody ran `POST /stop`
intentionally, or an automated control did. Check the audit log for
the last `stop` / `resume` entry.

---

## Assumptions the Substrate Makes

These are trust assumptions baked into the design. If any of them do not
hold in your environment, add an external control:

- **The Postgres instance is durable and not concurrently writable by
  untrusted processes.**
- **The object store is durable and honors immutability (no object
  overwrites via versioning tricks).**
- **The operator's network boundary is a trust boundary.** Anyone on the
  network can reach the control plane today.
- **Agents manage their own private keys** and do not share them.
- **Clocks are roughly synchronized** (within seconds — used only for
  audit timestamps; not safety-critical).
- **The schema migrations have not been altered.** Each migration is
  small; review and vendor them if you are running in a hostile environment.

---

## Future Work

- API key middleware with constant-time comparison.
- Hash-chained audit log (Merkle tree or simple prev-hash chaining).
- Full-payload signatures (sign the entire reference JSON, not just four
  fields).
- mTLS between agents and the control plane.
- Revocation workflow for agent keys with explicit audit events.
- External log shipping (syslog, OTEL) so the audit log is also recorded
  outside the substrate.

These are not v0 goals. They are the natural extension paths once the
substrate has shipped and real adversaries are identified.
