"""Ed25519 signing for agent references.

Key lifecycle:
- On startup the agent looks for AGENT_PRIVATE_KEY in the environment.
  If present, it must be a standard base64-encoded raw Ed25519 private key seed
  (32 bytes → 44 base64 chars, no PEM wrapping).
- If the env var is absent, a new key pair is generated for this process lifetime
  and the public key is logged at INFO level so the operator can record it.
  Ephemeral keys are fine in v0 — signatures are advisory, not enforced.

Signature payload (mirrors the schema description):
    {ref_id}:{canonical_object_id}:{agent_id}:{created_at}

The signature is returned as standard base64.
"""

from __future__ import annotations

import base64
import os

import structlog
from cryptography.hazmat.primitives.asymmetric.ed25519 import (
    Ed25519PrivateKey,
    Ed25519PublicKey,
)

logger = structlog.get_logger(__name__)


def _load_or_generate() -> Ed25519PrivateKey:
    raw_env = os.environ.get("AGENT_PRIVATE_KEY", "").strip()
    if raw_env:
        seed = base64.b64decode(raw_env)
        if len(seed) != 32:
            raise ValueError(
                f"AGENT_PRIVATE_KEY must be a base64-encoded 32-byte Ed25519 seed "
                f"(got {len(seed)} bytes after decoding)"
            )
        key = Ed25519PrivateKey.from_private_bytes(seed)
        pub = key.public_key()
        pub_b64 = _pub_b64(pub)
        logger.info("signing_key_loaded", public_key=pub_b64)
        return key

    key = Ed25519PrivateKey.generate()
    pub = key.public_key()
    pub_b64 = _pub_b64(pub)
    logger.info(
        "signing_key_generated",
        public_key=pub_b64,
        note="ephemeral key; set AGENT_PRIVATE_KEY env to persist identity",
    )
    return key


def _pub_b64(pub: Ed25519PublicKey) -> str:
    raw = pub.public_bytes_raw()
    return base64.b64encode(raw).decode()


# Module-level singleton — loaded once per process.
_private_key: Ed25519PrivateKey | None = None


def _key() -> Ed25519PrivateKey:
    global _private_key
    if _private_key is None:
        _private_key = _load_or_generate()
    return _private_key


def public_key_b64() -> str:
    """Return the base64-encoded public key (32 raw bytes)."""
    return _pub_b64(_key().public_key())


def sign_reference(
    ref_id: str,
    canonical_object_id: str,
    agent_id: str,
    created_at: str,
) -> str:
    """Sign the canonical reference payload and return standard base64.

    Payload: ``{ref_id}:{canonical_object_id}:{agent_id}:{created_at}``
    """
    message = f"{ref_id}:{canonical_object_id}:{agent_id}:{created_at}".encode()
    sig = _key().sign(message)
    return base64.b64encode(sig).decode()
