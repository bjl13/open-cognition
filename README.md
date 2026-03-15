# Open-Cognition

Open-Cognition is a reference substrate for governed shared memory between autonomous and human-directed agents.  
It separates **immutable canonical objects** (facts) from **agent-scoped references** (meaning), and provides a minimal control layer for attribution, auditability, and safe system halt.

This repository is a **reference architecture**, not a product.

---

## Purpose

Modern AI systems can write to shared state without leaving a verifiable trail of:

- who acted  
- what changed  
- when it changed  
- under whose authority  

Open-Cognition defines a minimal, reproducible substrate that ensures:

- immutable canonical records  
- agent-attributed meaning  
- append-only history  
- human-enforceable stop control  

The goal is not to control AI behavior, but to make system state **inspectable and attributable**.

---

## Core Concepts

### Canonical Objects

Immutable, content-addressed records.

Properties:

- hash = identity  
- payload stored in object storage  
- never modified in place  
- represent observations, documents, tool outputs, or policies  

Canonical objects are the **truth layer**.

---

### Agent References

Agent-scoped pointers to canonical objects.

They express:

- relevance  
- context  
- trust weighting  
- time horizon  

Agents do **not** own objects.  
They own their interpretation.

References are the **meaning layer**.

---

### Event Ledger

All mutations are recorded as append-only events:

- actor (agent or human)  
- action  
- target object  
- timestamp  

History is never rewritten.

---

### System Lifecycle

The control plane enforces a global mode:

- `RUNNING`  
- `STOPPED`  

When stopped:

- no new writes are accepted  
- agents must flush and cease mutation  
- existing state remains readable  

This provides a universal, human-invocable brake.

---

## Architecture Overview

Open-Cognition consists of four minimal components:

- **Object Store** (S3-compatible, e.g., R2)  
  Stores immutable payload blobs.

- **Reference Ledger** (Postgres)  
  Stores canonical object records, agent references, and audit events.

- **Control Plane** (Go)  
  Validates schemas, enforces policy, and manages system lifecycle.

- **Agents** (Python)  
  Read canonical objects and emit signed references.

An optional static dashboard provides read-only visibility.

---

## Quick Start

Prerequisites:

- Docker  
- Docker Compose  

Run:

```bash
git clone https://github.com/bjl13/open-cognition
cd open-cognition
make up
```

After startup you should be able to:

- create a canonical object  
- attach an agent reference  
- view records in the dashboard  
- trigger a system stop  

---

## Repository Structure

```
open-cognition/
│
├── schemas/        # Canonical object, reference, and policy schemas
├── examples/       # Minimal example records
├── cmd/            # Control plane entrypoint (Go)
├── internal/       # API, DB, models, lifecycle
├── agents/         # Sample agent implementations
├── dashboard/      # Compiled static UI
├── migrations/     # Database schema
└── docs/           # Architecture and governance notes
```

---

## Governance Model

Open-Cognition enforces three separations:

1. **Fact vs Interpretation**  
   Canonical objects are immutable. References carry meaning.

2. **Actor vs System**  
   All state mutations are attributable to specific agents or humans.

3. **Execution vs Memory**  
   Agents compute locally but cannot directly mutate shared truth.

---

## Design Principles

- Immutable records over mutable state  
- Append-only history over silent edits  
- Attribution over aggregate “system” behavior  
- Portability across storage providers  
- Minimal runtime dependencies  

---

## License

This project is licensed under the **Mozilla Public License 2.0 (MPL-2.0)**.

The core substrate remains open while allowing independent extensions and commercial implementations.
