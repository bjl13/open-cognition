-- 001_initial.sql
-- Creates the four core tables for the Open-Cognition reference ledger.
-- Run once against a fresh database. All tables are append-only by convention;
-- UPDATE and DELETE are not used in normal operation.

-- ---------------------------------------------------------------------------
-- System state
-- ---------------------------------------------------------------------------
-- Single-row table. Tracks the current global mode: RUNNING or STOPPED.
-- Seeded with RUNNING on first migration.

CREATE TABLE system_state (
    id            SMALLINT PRIMARY KEY DEFAULT 1,
    mode          TEXT        NOT NULL CHECK (mode IN ('RUNNING', 'STOPPED')),
    changed_by    TEXT        NOT NULL,
    changed_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT single_row CHECK (id = 1)
);

INSERT INTO system_state (id, mode, changed_by, changed_at)
VALUES (1, 'RUNNING', 'system:init', NOW());

-- ---------------------------------------------------------------------------
-- Canonical objects
-- ---------------------------------------------------------------------------
-- One row per canonical object. The id is the sha256:<hex> digest of the
-- stored payload bytes. Records are never updated after insertion.

CREATE TABLE canonical_objects (
    id            TEXT        PRIMARY KEY,   -- sha256:<hex>
    object_type   TEXT        NOT NULL CHECK (object_type IN ('observation', 'document', 'tool_output', 'policy')),
    content_type  TEXT        NOT NULL,
    size_bytes    INTEGER     NOT NULL CHECK (size_bytes >= 0),
    created_by    TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    storage_path  TEXT        NOT NULL UNIQUE,
    metadata      JSONB
);

CREATE INDEX idx_canonical_objects_type       ON canonical_objects (object_type);
CREATE INDEX idx_canonical_objects_created_at ON canonical_objects (created_at);
CREATE INDEX idx_canonical_objects_created_by ON canonical_objects (created_by);

-- ---------------------------------------------------------------------------
-- Agent references
-- ---------------------------------------------------------------------------
-- One row per agent reference. Each reference points to exactly one canonical
-- object. The canonical_object_id must exist in canonical_objects.

CREATE TABLE agent_references (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    canonical_object_id  TEXT        NOT NULL REFERENCES canonical_objects(id),
    agent_id             TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    context              TEXT        NOT NULL,   -- required; attribution without rationale is not attribution
    relevance            NUMERIC(4,3) CHECK (relevance >= 0 AND relevance <= 1),
    trust_weight         NUMERIC(4,3) CHECK (trust_weight >= 0 AND trust_weight <= 1),
    time_horizon         TEXT,                   -- ISO 8601 duration or datetime
    signature            TEXT,                   -- base64-encoded; advisory in v0, enforced in Phase 7
    metadata             JSONB
);

CREATE INDEX idx_agent_references_object_id ON agent_references (canonical_object_id);
CREATE INDEX idx_agent_references_agent_id  ON agent_references (agent_id);
CREATE INDEX idx_agent_references_created_at ON agent_references (created_at);

-- ---------------------------------------------------------------------------
-- Audit log
-- ---------------------------------------------------------------------------
-- Append-only record of every mutation in the system. Never updated or deleted.
-- Provides full reconstruction of system history.

CREATE TABLE audit_log (
    id           BIGSERIAL   PRIMARY KEY,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actor        TEXT        NOT NULL,   -- agent_id or human operator identifier
    action       TEXT        NOT NULL,   -- e.g. 'create_canonical', 'create_reference', 'stop', 'resume'
    target_id    TEXT,                   -- id of the affected object or reference (null for lifecycle events)
    target_type  TEXT,                   -- 'canonical_object' | 'agent_reference' | 'system_state'
    detail       JSONB                   -- any additional context (schema version, object_type, etc.)
);

CREATE INDEX idx_audit_log_actor       ON audit_log (actor);
CREATE INDEX idx_audit_log_occurred_at ON audit_log (occurred_at);
CREATE INDEX idx_audit_log_target_id   ON audit_log (target_id);

-- Seed: record the initialization event
INSERT INTO audit_log (actor, action, target_type, detail)
VALUES ('system:init', 'init', 'system_state', '{"mode": "RUNNING"}');
