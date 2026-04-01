-- 002_agent_keys.sql
-- TOFU public-key registry for agent Ed25519 keys.
-- Enforces that each agent_id maps to exactly one public key.
-- First signed reference from an agent auto-registers the key (trust on first use).
-- Subsequent references must present the same key; mismatch is rejected by the control plane.
--
-- Uses CREATE TABLE IF NOT EXISTS so this migration is safe to re-run.

CREATE TABLE IF NOT EXISTS agent_keys (
    agent_id      TEXT        PRIMARY KEY,
    public_key    TEXT        NOT NULL,   -- base64-encoded 32-byte raw Ed25519 public key
    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    first_ref_id  TEXT        NOT NULL    -- UUID of the first reference that triggered registration
);

CREATE INDEX IF NOT EXISTS idx_agent_keys_registered_at ON agent_keys (registered_at);
