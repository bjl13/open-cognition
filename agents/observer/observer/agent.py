"""Observer agent entry point.

Environment variables:
    CONTROL_PLANE       Base URL of the control plane (default: http://localhost:8080)
    AGENT_ID            Agent identifier used in X-Actor and created_by fields
                        (default: agent:observer:<hostname>)
    OBSERVE_TARGET      What to observe each cycle:
                          - https?://...  → fetch URL
                          - /path/...     → read file
                          - (empty)       → collect environment snapshot
    POLL_INTERVAL       Seconds between observation cycles (default: 60)
    AGENT_PRIVATE_KEY   Base64-encoded 32-byte Ed25519 seed (generated if absent)
"""

from __future__ import annotations

import asyncio
import dataclasses
import hashlib
import os
import platform
import socket
import uuid
from datetime import datetime, timezone

import structlog

from .client import ConflictError, ControlPlaneClient, SystemStoppedError
from .models import AgentReference, CreateCanonicalRequest
from .observe import collect
from .signing import public_key_b64, sign_reference

logger = structlog.get_logger(__name__)


@dataclasses.dataclass(frozen=True)
class Config:
    control_plane: str
    agent_id: str
    observe_target: str
    poll_interval: float


def _load_config() -> Config:
    hostname = socket.getfqdn() or platform.node() or "unknown"
    default_agent_id = f"agent:observer:{hostname}"
    return Config(
        control_plane=os.environ.get("CONTROL_PLANE", "http://localhost:8080"),
        agent_id=os.environ.get("AGENT_ID", default_agent_id),
        observe_target=os.environ.get("OBSERVE_TARGET", ""),
        poll_interval=float(os.environ.get("POLL_INTERVAL", "60")),
    )


def _configure_logging() -> None:
    structlog.configure(
        processors=[
            structlog.stdlib.add_log_level,
            structlog.stdlib.add_logger_name,
            structlog.processors.TimeStamper(fmt="iso"),
            structlog.processors.StackInfoRenderer(),
            structlog.processors.format_exc_info,
            structlog.processors.JSONRenderer(),
        ],
        wrapper_class=structlog.make_filtering_bound_logger(0),
        context_class=dict,
        logger_factory=structlog.PrintLoggerFactory(),
    )


def _utcnow() -> str:
    return datetime.now(tz=timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _storage_path(object_type: str, object_id: str, created_at: str) -> str:
    dt = datetime.strptime(created_at, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc)
    date_path = dt.strftime("%Y/%m/%d")
    return f"canonical/{object_type}/{date_path}/{object_id}.json"


async def _run_cycle(client: ControlPlaneClient, cfg: Config) -> None:
    """Execute one observation cycle: collect → post canonical → post reference."""
    log = logger.bind(agent_id=cfg.agent_id, target=cfg.observe_target or "<env>")

    # --- Check system state before collecting ---
    try:
        state = await client.get_status()
    except Exception as exc:
        log.warning("cycle.status_check_failed", error=str(exc))
        return

    if state.mode != "RUNNING":
        log.info("cycle.skipped", reason="system_not_running", mode=state.mode)
        return

    # --- Collect observation ---
    try:
        payload, content_type = await collect(cfg.observe_target)
    except Exception as exc:
        log.error("cycle.collect_failed", error=str(exc), exc_info=True)
        return

    # --- Build canonical object fields ---
    digest = hashlib.sha256(payload).hexdigest()
    object_id = f"sha256:{digest}"
    now = _utcnow()
    storage_path = _storage_path("observation", object_id, now)

    canonical_req = CreateCanonicalRequest(
        schema_version="0.1.0",
        id=object_id,
        object_type="observation",
        content_type=content_type,
        size_bytes=len(payload),
        created_at=now,
        created_by=cfg.agent_id,
        storage_path=storage_path,
        payload=payload,
        metadata={"source": cfg.observe_target or "environment"},
    )

    # --- Submit canonical object ---
    try:
        canonical = await client.post_canonical(canonical_req)
        log.info(
            "cycle.canonical_created",
            id=canonical.id,
            size_bytes=canonical.size_bytes,
            storage_path=canonical.storage_path,
        )
    except ConflictError:
        # Same content submitted before — fully expected for slow-changing targets.
        log.info("cycle.canonical_exists", id=object_id)
        # Still post a reference so the agent's interest is recorded.
    except SystemStoppedError:
        log.info("cycle.system_stopped")
        return
    except Exception as exc:
        log.error("cycle.canonical_failed", error=str(exc), exc_info=True)
        return

    # --- Submit agent reference ---
    ref_id = str(uuid.uuid4())
    ref_now = _utcnow()
    signature = sign_reference(ref_id, object_id, cfg.agent_id, ref_now)

    ref = AgentReference(
        schema_version="0.1.0",
        id=ref_id,
        canonical_object_id=object_id,
        agent_id=cfg.agent_id,
        created_at=ref_now,
        context=(
            f"Periodic observation from {cfg.agent_id}. "
            f"Target: {cfg.observe_target or 'environment snapshot'}."
        ),
        relevance=1.0,
        trust_weight=1.0,
        signature=signature,
        public_key=public_key_b64(),
        metadata={"observer_public_key": public_key_b64()},
    )

    try:
        await client.post_reference(ref)
        log.info("cycle.reference_created", ref_id=ref_id, canonical_object_id=object_id)
    except ConflictError:
        log.info("cycle.reference_exists", ref_id=ref_id)
    except SystemStoppedError:
        log.info("cycle.system_stopped")
    except Exception as exc:
        log.error("cycle.reference_failed", error=str(exc), exc_info=True)


async def _run(cfg: Config) -> None:
    log = logger.bind(agent_id=cfg.agent_id, control_plane=cfg.control_plane)
    log.info(
        "agent.starting",
        observe_target=cfg.observe_target or "<environment snapshot>",
        poll_interval=cfg.poll_interval,
        public_key=public_key_b64(),
    )

    async with ControlPlaneClient(cfg.control_plane, cfg.agent_id) as client:
        while True:
            try:
                await _run_cycle(client, cfg)
            except Exception as exc:
                log.error("agent.cycle_error", error=str(exc), exc_info=True)

            log.debug("agent.sleeping", seconds=cfg.poll_interval)
            await asyncio.sleep(cfg.poll_interval)


def main() -> None:
    _configure_logging()
    cfg = _load_config()
    try:
        asyncio.run(_run(cfg))
    except KeyboardInterrupt:
        logger.info("agent.stopped", reason="keyboard_interrupt")


if __name__ == "__main__":
    main()
