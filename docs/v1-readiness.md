# v1.0 Readiness

Phase 8 closed the documentation gap. This document is the bridge from
"Phase 8 complete" to "v1.0 frozen and deployable by a stranger."

It serves two purposes:

1. A checklist of known gaps between the substrate's current state and a
   releasable v1.0.
2. A specification for what "self-installing, self-congruent executable"
   means for this project.

---

## The Alignment Audit (snapshot at Phase 8 close)

Before this doc was written, an audit was performed against the
precedence order:

1. Work done / repo structure
2. README
3. Documentation
4. Roadmap

### Incongruencies found

| # | Issue | Source of truth | Resolution |
|---|-------|-----------------|------------|
| 1 | Merge commit `d0e92df` dropped Phase 6/7 code: read endpoints, dashboard serving, observer compose service, `AuditEntry` model, DB list methods, `make dashboard` target | Historical commits + scripts/dashboard depending on missing endpoints — the "work done" had been lost | Restored from `8f09a7b` |
| 2 | `.env.example` used `MINIO_*` env vars; control plane reads `STORAGE_*` | Code in `cmd/control-plane/main.go` | `.env.example` rewritten |
| 3 | `Makefile` `make up` echoed "Control plane: ... (after: make migrate && make build)" but compose already starts it | Compose file and actual behaviour | Echo corrected |
| 4 | README quickstart buried under concepts; no table of contents; some section links stale | README | README restructured |
| 5 | `CONTROL_API_KEY` declared in `.env.example` and referenced in `docs/architecture.md` but never enforced in handlers | Code in `internal/api/handlers.go` | Called out in debt register and threat model; scheduled for Phase 9 |
| 6 | `docs/trust-model.md`, `docs/lifecycle.md`, `docs/threat-model.md`, architecture & data-flow diagrams: roadmap declared but not written | Roadmap vs `docs/` directory | All five written in Phase 8 |
| 7 | Dashboard read endpoints called by `dashboard/src/main.ts` and `scripts/*.sh` did not exist in HEAD handlers | Consumer code | Fixed via #1 restoration |

### Summary

Post-audit, the repo, README, and docs agree on what exists. The roadmap
still acknowledges technical debt, but no roadmap claim is unbacked by code.

---

## What "v1.0" Means

A v1.0 Open-Cognition release is a substrate that satisfies:

1. **Reproducible cold start.** A stranger can `git clone` into an empty
   directory and reach a working, tested stack with no manual setup beyond
   Docker being installed.
2. **Enforced auth on writes.** `CONTROL_API_KEY` is checked by middleware
   on every write endpoint. An unauthenticated write returns 401 before
   touching any handler logic.
3. **Tamper-evident audit log.** Either hash-chained rows or an external
   log shipper (syslog / OTEL) configured out-of-the-box.
4. **No stdlib Postgres driver.** The `internal/pg` temporary driver is
   retired, `pgx/v5` takes its place, and the docker-compose MD5 auth flag
   is removed.
5. **Schema version frozen at `1.0.0`.** Any future change to the schemas
   bumps `schema_version` and triggers migration guidance.
6. **CI green on every commit** covering: build, vet, schema validate,
   unit tests, and an end-to-end docker-compose smoke + reconcile run.
7. **Signed releases.** Tagged, signed container images and a signed
   binary for the control plane. Operators verify before running.
8. **Written upgrade path.** A "v0.x → v1.0" doc explaining what schema
   fields move and how operators migrate existing data.

Nothing in the above adds new *features*. v1.0 is about closing the last
gap between "works on my machine" and "works on yours, reliably, without me."

---

## Gap List (pre-v1.0)

### Security

- [ ] **API key middleware.** Add `internal/api/middleware/auth.go`; wrap
      write routes in `RegisterRoutes`. Constant-time comparison. Return
      401 without leaking which header is malformed.
- [ ] **Auth for read endpoints** (opt-in via env). Default-open today
      for dashboard use; v1.0 should allow `READ_AUTH=required`.
- [ ] **mTLS documentation.** One page showing how to terminate TLS at
      Caddy / nginx / Envoy in front of the control plane.
- [ ] **Audit log integrity.** Either (a) hash-chain rows at insert time
      (`prev_hash` column, simple scheme), or (b) ship every audit row to
      an external log sink. Pick one and commit.

### Reliability

- [ ] **`pgx/v5` migration.** 5 steps documented in `internal/pg/pg.go`.
      Delete `internal/pg` entirely after swap. Remove
      `POSTGRES_HOST_AUTH_METHOD=md5`.
- [ ] **Transactional write path for `POST /canonical`.** Investigate
      compensating-action pattern (storage PUT, then DB insert with
      transactional outbox) so orphans in storage become self-healing
      rather than requiring operator action.
- [ ] **Health endpoint (`/healthz`, `/readyz`).** Kubernetes-style.
      Distinct from `/status`, which reports application mode.

### Operability

- [ ] **End-to-end CI.** GitHub Actions that stands up docker-compose,
      runs `make migrate`, `make smoke`, `make reconcile`. Blocks merges
      on failure. Would have caught the Phase 6/7 merge regression.
- [ ] **Reproducible container image.** Pin base images by digest; vendor
      the `tsc` build for the dashboard; use multi-stage Dockerfile that
      produces a scratch-based runtime image.
- [ ] **`make install`**: one command that provisions everything needed
      to run locally without Docker (Postgres via whatever's available,
      MinIO or filesystem-backed object store, the binary).
- [ ] **Signed releases.** `cosign` sign the Docker image and the binary.
      Publish public key in the repo root.

### Documentation

- [x] `docs/trust-model.md`
- [x] `docs/lifecycle.md`
- [x] `docs/threat-model.md`
- [x] `docs/architecture-diagram.md`
- [x] `docs/data-flow-diagram.md`
- [ ] `docs/deployment.md` — production-grade deployment (R2, managed
      Postgres, reverse proxy, mTLS).
- [ ] `docs/upgrade.md` — forward-compat guidance for schema bumps.
- [ ] `docs/runbook.md` — common operator tasks (rotating keys, stopping
      the system during incident, restoring from backup).

---

## Self-Installing, Self-Congruent Executable

The phrase has two parts. Both are real requirements.

### "Self-installing"

Starting from a clean machine with only the repo cloned, a single command
brings up a working substrate:

```
make up && make migrate && make smoke
```

Requirements to qualify as "self-installing":

1. **No manual edits** to config files before running. Defaults in
   `.env.example` work against the compose stack out of the box.
2. **No out-of-band downloads.** Every binary, image, and schema needed
   is either in the repo or pulled by compose itself.
3. **Deterministic success or deterministic failure.** `make smoke`
   exits 0 if the stack is healthy, nonzero with a clear message
   otherwise. No ambiguous warnings.
4. **Idempotent setup.** Re-running `make up && make migrate` on an
   already-running stack succeeds (migration 002 already is; 001 must
   become so or the Makefile must detect and skip).
5. **Documented uninstall.** `make down && docker volume rm …` reverses
   everything `make up` did.

### "Self-congruent"

Every claim the repo makes about itself is verifiable from within the repo:

1. **README examples execute.** Every command shown in the README is
   either runnable as shown or explicitly marked as "illustrative."
2. **Roadmap claims match code.** Phase N marked complete ⇒ the
   corresponding endpoint / file / behaviour exists in HEAD. CI enforces.
3. **Docs link targets exist.** Every `docs/foo.md` linked from README
   or another doc is present and renders.
4. **Schemas match code.** `examples/*.json` validate against the JSON
   schemas. `make validate` enforces this; CI runs it.
5. **`make smoke` exercises the whole write path.** Create, verify 409,
   verify storage existence (not just indirect via 409), emit a signed
   reference, verify signature enforcement rejects unsigned.
6. **`make reconcile` passes on a freshly populated system.** No storage
   divergence.

### Exit test

The substrate is self-installing and self-congruent when this dialogue
succeeds, unassisted, on a machine the author has never touched:

```
$ git clone https://github.com/bjl13/open-cognition
$ cd open-cognition
$ make up && make migrate && make smoke && make reconcile
# … all green …
$ open http://localhost:8080
# dashboard renders the smoke-test object, reference, and audit rows
```

No README re-reading. No Slack message to the author. No "oh, you need
to also run X." At that point, v1.0 is shippable.
