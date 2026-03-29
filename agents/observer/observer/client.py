"""Async HTTP client for the open-cognition control plane.

All requests include an X-Actor header derived from the agent's identity.
Retry policy (applies to POST endpoints):
  - 3 attempts total
  - Retry on 5xx responses and transient network errors
  - Exponential backoff: 500ms, 1s (±25% jitter each)
  - No retry on 4xx (including 409 Conflict — that is expected behaviour)
"""

from __future__ import annotations

import asyncio
import random
from typing import Any

import httpx
import structlog

from .models import AgentReference, CanonicalObject, CreateCanonicalRequest, SystemState

logger = structlog.get_logger(__name__)

_MAX_ATTEMPTS = 3
_BASE_BACKOFF_S = 0.5


class ConflictError(Exception):
    """The server returned 409 — the object already exists."""


class SystemStoppedError(Exception):
    """The server returned 503 — the system is STOPPED."""


class ControlPlaneError(Exception):
    """Unrecoverable HTTP error from the control plane."""

    def __init__(self, status: int, body: str) -> None:
        super().__init__(f"HTTP {status}: {body}")
        self.status = status
        self.body = body


class ControlPlaneClient:
    """Async client for the open-cognition control plane."""

    def __init__(self, base_url: str, agent_id: str) -> None:
        self._base_url = base_url.rstrip("/")
        self._actor = agent_id
        # Single shared async client — caller owns lifecycle via async context manager.
        self._http = httpx.AsyncClient(
            base_url=self._base_url,
            headers={"X-Actor": self._actor},
            timeout=httpx.Timeout(connect=10.0, read=30.0, write=30.0, pool=5.0),
        )

    async def __aenter__(self) -> "ControlPlaneClient":
        return self

    async def __aexit__(self, *_: Any) -> None:
        await self._http.aclose()

    # ---------------------------------------------------------------------------
    # Public API
    # ---------------------------------------------------------------------------

    async def get_status(self) -> SystemState:
        resp = await self._http.get("/status")
        resp.raise_for_status()
        return SystemState.from_dict(resp.json())

    async def post_canonical(self, req: CreateCanonicalRequest) -> CanonicalObject:
        """Submit a canonical object. Raises ConflictError on 409."""
        resp = await self._post_with_retry("/canonical", req.to_dict())
        return CanonicalObject.from_dict(resp.json())

    async def post_reference(self, ref: AgentReference) -> AgentReference:
        """Submit an agent reference. Raises ConflictError on 409."""
        resp = await self._post_with_retry("/reference", ref.to_dict())
        data = resp.json()
        # Return a reconstructed AgentReference (the server echoes the record).
        return AgentReference(
            schema_version=data["schema_version"],
            id=data["id"],
            canonical_object_id=data["canonical_object_id"],
            agent_id=data["agent_id"],
            created_at=data["created_at"],
            context=data["context"],
            relevance=data.get("relevance"),
            trust_weight=data.get("trust_weight"),
            time_horizon=data.get("time_horizon"),
            signature=data.get("signature"),
            metadata=data.get("metadata"),
        )

    # ---------------------------------------------------------------------------
    # Internals
    # ---------------------------------------------------------------------------

    async def _post_with_retry(
        self, path: str, body: dict[str, Any]
    ) -> httpx.Response:
        last_exc: Exception | None = None

        for attempt in range(_MAX_ATTEMPTS):
            try:
                resp = await self._http.post(path, json=body)
            except (httpx.ConnectError, httpx.TimeoutException, httpx.NetworkError) as exc:
                last_exc = exc
                logger.warning(
                    "control_plane.transient_error",
                    path=path,
                    attempt=attempt + 1,
                    error=str(exc),
                )
                await _backoff(attempt)
                continue

            if resp.status_code == 409:
                raise ConflictError(resp.text)

            if resp.status_code == 503:
                raise SystemStoppedError(resp.text)

            if resp.status_code >= 500:
                last_exc = ControlPlaneError(resp.status_code, resp.text)
                logger.warning(
                    "control_plane.server_error",
                    path=path,
                    attempt=attempt + 1,
                    status=resp.status_code,
                )
                await _backoff(attempt)
                continue

            if resp.status_code >= 400:
                raise ControlPlaneError(resp.status_code, resp.text)

            return resp

        raise last_exc or ControlPlaneError(0, "all retry attempts exhausted")


async def _backoff(attempt: int) -> None:
    """Sleep with exponential backoff and ±25% jitter."""
    base = _BASE_BACKOFF_S * (2 ** attempt)
    jitter = base * 0.25 * (2 * random.random() - 1)
    delay = max(0.0, base + jitter)
    await asyncio.sleep(delay)
