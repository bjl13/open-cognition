# Architecture Diagram

High-level component view. For the full text description, see
[`architecture.md`](architecture.md).

```mermaid
flowchart TB
  subgraph Clients["Clients"]
    direction LR
    H["Human Operator"]
    A1["Agent A"]
    A2["Agent B"]
    DASH["Dashboard<br/>(static, served by CP)"]
  end

  subgraph CP["Control Plane — Go"]
    direction TB
    ROUTE["HTTP Router<br/>go 1.22 method+path"]
    VAL["Schema + Hash + Storage-Path<br/>Validation"]
    SIG["Ed25519 TOFU<br/>Signature Verifier"]
    HALT["Halt Guard<br/>(STOPPED → 503)"]
    ROUTE --> VAL
    ROUTE --> SIG
    ROUTE --> HALT
  end

  subgraph Persistence["Persistence"]
    direction LR
    subgraph Ledger["Reference Ledger — Postgres"]
      T1[("canonical_objects")]
      T2[("agent_references")]
      T3[("system_state")]
      T4[("audit_log")]
      T5[("agent_keys<br/>TOFU")]
    end
    subgraph Store["Object Store — S3 compatible"]
      B[("bucket: cognition<br/>canonical/{type}/yyyy/mm/dd/sha256:*.json")]
    end
  end

  H -->|API key<br/>X-Actor| ROUTE
  A1 -->|signed ref<br/>X-Actor| ROUTE
  A2 -->|signed ref<br/>X-Actor| ROUTE
  DASH -->|GET read-only| ROUTE

  VAL -->|metadata| Ledger
  VAL -->|bytes| Store
  SIG <-->|lookup/register| T5
  HALT <-->|read mode| T3
  VAL -->|append| T4

  classDef client fill:#BDAECD,stroke:#333,color:#111;
  classDef cp fill:#EA8A7C,stroke:#6EC8C2,color:#111;
  classDef store fill:#6EC8C2,stroke:#333,color:#111;
  class H,A1,A2,DASH client;
  class ROUTE,VAL,SIG,HALT cp;
  class T1,T2,T3,T4,T5,B store;
```

## Legend

- **Control Plane** is a single Go binary — one process, multiple
  responsibilities gated at the router level.
- **Reference Ledger** is the authoritative metadata store; five tables,
  all append-only by application convention.
- **Object Store** holds payload bytes under deterministic content-addressed
  paths. No overwrite is possible at the application layer.
- **Dashboard** is compiled TypeScript served directly by the control
  plane from `dashboard/static/` — no separate web server.

## Endpoint surface

| Endpoint | Method | Write? | Auth | Halt-gated |
|---|---|---|---|---|
| `/status` | GET | no | none | no |
| `/stop` | POST | yes | API key + X-Actor | no |
| `/resume` | POST | yes | API key + X-Actor | no |
| `/canonical` | POST | yes | API key + X-Actor | yes |
| `/reference` | POST | yes | API key + X-Actor + Ed25519 sig | yes |
| `/canonicals` | GET | no | none | no |
| `/references` | GET | no | none | no |
| `/audit` | GET | no | none | no |
| `/reconcile` | GET | no | none | no |
| `/` (dashboard) | GET | no | none | no |

API key enforcement is listed for completeness — the middleware ships with
the v1.0 freeze. Until then, network boundary is the auth boundary.
