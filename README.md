<p align="center">
  <a href=".">
    <img src="internal/repo-header.svg?v=20263103191500R" alt="Open-Cognition Logo" width="175"/>
  </a>
</p>

<h1 align="center">Open-Cognition</h1>

Open-Cognition is a reference substrate for governed shared memory between autonomous and human-directed agents.

It separates **immutable canonical objects** (facts) from **agent-scoped references** (meaning), and provides a minimal control layer for _attribution_, _auditability_, and a safe _system halt_.

This repository defines a **reference architecture**, not a product.

---

## 📋 Contents

- [🧭 Purpose](#-purpose)
  - [The Problem](#the-problem)
  - [The Solution](#the-solution)
  - [What this is not](#what-this-is-not)
- [🚀 Quick Start](#-quick-start)
  - [Prerequisites](#prerequisites)
  - [Run](#run)
  - [Verify](#verify)
  - [Local Development](#local-development)
- [📦 Repository Structure](#-repository-structure)
- [🧱 Core Concepts](#-core-concepts)
  - [Canonical Objects (Fact Layer)](#canonical-objects-fact-layer)
  - [Agent References (Meaning Layer)](#agent-references-meaning-layer)
  - [Event Ledger](#event-ledger)
  - [System Lifecycle](#system-lifecycle)
- [🧠 Mental Model](#-mental-model)
- [🏗️ Architecture Overview](#️-architecture-overview)
- [⚙️ Example Use Case](#️-example-use-case)
- [🛡️ Governance Model](#️-governance-model)
- [⚜️ Design Principles](#️-design-principles)
- [🗺️ Roadmap](#️-roadmap)
- [📜 License](#-license)

---

## 🧭 Purpose

Most shared AI systems, like agent networks and systems of agents, lose attribution and history as they evolve.

Open-Cognition ensures that:
- nothing is silently overwritten  
- every action is attributable  
- system state remains inspectable over time  

### The Problem
Modern AI systems can write to shared state without leaving a reliable trail of:

- who acted  
- what changed  
- when it changed  
- under whose authority

### The Solution
Open-Cognition addresses this by defining a minimal, reproducible substrate with:

- immutable canonical records  
- agent-attributed meaning  
- append-only history  
- human-enforceable stop control  

The goal is not to constrain agent behavior, but to make the shared system state **inspectable, attributable, and reversible at the level of human control**.

### What this is not

Open-Cognition is not:

- a database or storage engine  
- a full agent framework  
- a consensus or truth-resolution system  
- a replacement for application logic  

It is a **base layer substrate for recording and attributing changes to a system**, not a system that decides what is true.

> [!IMPORTANT]
> Open-Cognition records system state; it does not determine what is true or resolve conflicts.

---

## 🚀 Quick Start

### Prerequisites

- [Docker](https://docs.docker.com/get-started/get-docker/)
- [Docker Compose](https://docs.docker.com/compose/install/)

### Run

```bash
git clone https://github.com/bjl13/open-cognition
cd open-cognition
make up       # starts Postgres, MinIO, and the control plane
make migrate  # applies the database schema
```

After startup:

| Service | Address |
|---|---|
| Control Plane + Dashboard | `http://localhost:8080` |
| MinIO Console | `http://localhost:9001` (minioadmin / minioadmin) |
| MinIO API | `http://localhost:9000` |
| Postgres | `localhost:5432` |

### Verify

```bash
make smoke    # full round-trip: create object → verify storage → test immutability
```

`make smoke` creates a canonical object, verifies the returned ID matches the SHA-256 of the submitted payload, and confirms duplicate rejection (409). Exit 0 means the substrate is working.

From the dashboard at `http://localhost:8080` you can inspect system mode, canonical objects, agent references, and the audit log in real time.

### Local Development

To build and run the control plane outside Docker:

```bash
cp .env.example .env   # set CONTROL_API_KEY and storage credentials
make build             # compiles ./control-plane
./control-plane
```

Other useful targets:

```bash
make logs       # tail all service logs
make down       # stop all services
make validate   # validate schemas against example files
make export     # export canonical objects to backups/ (NDJSON)
make backup     # dump Postgres to backups/ (gzip SQL)
make reconcile  # verify every ledger object exists in storage
```

---

## 📦 Repository Structure

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
├── scripts/        # Operational scripts (smoke test, backup, export, reconcile)
├── docs/           # Architecture and governance notes
└── roadmap.md      # Live development intent document (see Roadmap)
```

---

## 🧱 Core Concepts

### Canonical Objects (Fact Layer)

Unchanging, content-addressed records.

Properties:

- hash defines identity  
- payload stored in object storage  
- never modified in place  
- represent observations, documents, tool outputs, or policies  

Canonical objects form the system's **shared source of truth**.

---

### Agent References (Meaning Layer)

Agent-scoped pointers to canonical objects.

They express:

- relevance  
- context  
- trust weighting  
- time horizon  

Agents **cannot** own objects.  
They own their interpretation of them.

---

### Event Ledger

All state changes are recorded as append-only events:

- actor (which agent or human)  
- action  
- target object  
- timestamp  

History is never rewritten.  
Attribution is always preserved.

---

### System Lifecycle

A global control mode governs whether the system can mutate state:

- `RUNNING`  
- `STOPPED`  

When stopped:

- no new writes are accepted  
- agents must flush and cease mutation  
- existing state remains readable  

This provides a universal, independently-invocable halt mechanism.

---

## 🧠 Mental Model

Open-Cognition separates system memory into two layers:

- **_Facts_** → immutable, content-addressed objects  
- **_Meaning_** → agent-specific references to those objects  

Agents do not modify shared truth.  
They interpret it.

```mermaid

---
config:
  elk:
    mergeEdges: true

  look: neo
  theme: neo
  layout: dagre
---

flowchart TB
 subgraph AP["Agentic Plane"]
    direction LR
        A["Agent"]
  end
 subgraph DL["Data Layer"]
    direction LR
        REF["Reference"]
        J[" "]
        O["Canonical Object Store"]
        E["Event Ledger"]
  end
 subgraph CP["Control Plane"]
    direction LR
        C["Control Plane"]
  end
    A ~~~ REF
    REF ~~~ C
    REF --> J
    J -- *records in* ------> E
    A -- *creates* --> REF
    J -- *points to* --> O
    C -. *governs* .-> REF
    C -. *enforces* .-> E
    E -- *describes* ---> O

    A@{ shape: rounded}
    REF@{ shape: notch-rect}
    J@{ shape: f-circ}
    O@{ shape: cyl}
    E@{ shape: bow-rect}
    C@{ shape: hex}
     A:::core
     REF:::core
     O:::core
     E:::core
     C:::control
    classDef core fill:#BDAECD,stroke:#CDA433,stroke-width:2px
    classDef control stroke:#6EC8C2,fill:#EA8A7C,stroke-width:2px
    style A fill:#6EC8C2,stroke:#BDAECD
```

Open-Cognition separates system memory into two layers:
- Facts → immutable, content-addressed objects
- Meaning → agent-specific references to those objects

---

## 🏗️ Architecture Overview

Open-Cognition consists of four minimal components:

- **Object Store** (S3-compatible, e.g., R2)  
  Stores immutable payload blobs.

- **Reference Ledger** (Postgres)  
  Stores canonical object records, agent references, and audit events.

- **Control Plane** (Go)  
  Validates schemas, enforces policy, and manages lifecycle state.

- **Agents** (Python)  
  Read canonical objects and emit signed references.

An optional static dashboard provides read-only visibility into system state.

→ See [`docs/architecture.md`](docs/architecture.md) for full component detail, data flow, content-addressing mechanics, and local development notes.

---

## ⚙️ Example Use Case

An agent analyzes a document:

1. A report is stored as a canonical object  
   → `obj:sha256:9f3c…`

2. The agent emits a reference  
   → `ref:agent-A:001 → obj:9f3c…`

3. The reference encodes interpretation  
   → `{ tag: "financial", relevance: 0.8, horizon: "short-term" }`

4. An event is appended to the ledger  
   → `{ actor: "agent-A", action: "reference.create", object: "obj:9f3c…" }`

5. A second agent emits a conflicting reference  
   → `{ tag: "incomplete", relevance: 0.3 }`

The object is immutable; disagreement is expressed through references, not mutation.

No interpretation overwrites another and all perspectives remain attributable.

Because objects are immutable and references are isolated, interacting agents cannot overwrite or compound each other's errors.

> [!TIP]
> When agents can observe each other's references, disagreement becomes visible—enabling comparison, correction, and potential convergence over time.

---

## 🛡️ Governance Model

Open-Cognition enforces three key separations:

1. **Fact vs Interpretation**  
   Canonical objects are immutable. References carry meaning.

2. **Actor vs System**  
   All mutations are attributable to specific agents or humans.

3. **Execution vs Memory**  
   Agents compute locally but cannot directly mutate shared truth.

→ See [`docs/governance-model.md`](docs/governance-model.md) for full detail on canonical object rules, reference requirements, the TOFU key model, policy objects, system halt behavior, and audit log semantics.

---

## ⚜️ Design Principles

- Immutable records over mutable state
- Append-only history over silent edits
- Attribution over aggregate "system" behavior
- Portability across storage providers
- Minimal runtime dependencies

---

## 🗺️ Roadmap

[`roadmap.md`](roadmap.md) is a live working document tracking development phases, current status, known technical debt, and design rationale. It is **not** a governance document — it reflects development intent and internal reasoning, not system contracts.

It is intentionally surfaced here as a reference point for contributors and agentic coding tools. Reading it before working on the codebase will give you accurate context on what is complete, what is deferred, and why specific tradeoffs were made. It is the right place to look before proposing changes or extensions.

**Current state:** Phases 0–7 complete. Phase 8 (documentation for external adoption) is next.

---

## 📜 License

This project is licensed under the [Mozilla Public License 2.0 (MPL-2.0)](https://www.mozilla.org/en-US/MPL/2.0/).

The core substrate remains open while allowing independent extensions and commercial implementations.
