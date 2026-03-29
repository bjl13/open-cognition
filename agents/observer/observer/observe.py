"""Observation collection.

Dispatch logic:
  OBSERVE_TARGET starts with "http://" or "https://"  → fetch URL
  OBSERVE_TARGET is a non-empty string (not a URL)     → read file path
  OBSERVE_TARGET is unset or empty                     → environment snapshot

Each function returns raw bytes that become the canonical object payload.
Content is always JSON so content_type is always "application/json".
"""

from __future__ import annotations

import json
import os
import platform
import socket
import time
from typing import Any

import httpx
import structlog

logger = structlog.get_logger(__name__)


async def collect(target: str) -> tuple[bytes, str]:
    """Collect an observation.

    Returns ``(payload_bytes, content_type)``.
    """
    if target.startswith("http://") or target.startswith("https://"):
        return await _fetch_url(target)
    if target:
        return _read_file(target)
    return _environment_snapshot()


# ---------------------------------------------------------------------------
# URL fetch
# ---------------------------------------------------------------------------


async def _fetch_url(url: str) -> tuple[bytes, str]:
    """GET the URL; store status, headers, and body as a JSON observation."""
    logger.info("observe.fetch_url", url=url)
    async with httpx.AsyncClient(
        follow_redirects=True,
        timeout=httpx.Timeout(connect=10.0, read=30.0, write=10.0, pool=5.0),
    ) as client:
        resp = await client.get(url)

    # Attempt to decode the body as text; fall back to base64 for binary.
    try:
        body_text: str | None = resp.text
        body_b64: str | None = None
    except Exception:
        import base64
        body_text = None
        body_b64 = base64.b64encode(resp.content).decode()

    observation: dict[str, Any] = {
        "observed_at": _utcnow(),
        "source": "url",
        "url": str(resp.url),
        "status_code": resp.status_code,
        "headers": dict(resp.headers),
        "body": body_text,
        "body_b64": body_b64,
        "elapsed_ms": round(resp.elapsed.total_seconds() * 1000, 1),
    }
    payload = _encode(observation)
    content_type = "application/json"
    logger.info(
        "observe.fetch_url.done",
        url=url,
        status=resp.status_code,
        bytes=len(payload),
    )
    return payload, content_type


# ---------------------------------------------------------------------------
# File read
# ---------------------------------------------------------------------------


def _read_file(path: str) -> tuple[bytes, str]:
    """Read a file and store it as a JSON observation with metadata."""
    logger.info("observe.read_file", path=path)
    stat = os.stat(path)
    with open(path, "rb") as fh:
        raw = fh.read()

    # Attempt UTF-8 decode for text content.
    try:
        content_text: str | None = raw.decode("utf-8")
        content_b64: str | None = None
    except UnicodeDecodeError:
        import base64
        content_text = None
        content_b64 = base64.b64encode(raw).decode()

    observation: dict[str, Any] = {
        "observed_at": _utcnow(),
        "source": "file",
        "path": os.path.abspath(path),
        "size_bytes": stat.st_size,
        "mtime": stat.st_mtime,
        "content": content_text,
        "content_b64": content_b64,
    }
    payload = _encode(observation)
    logger.info("observe.read_file.done", path=path, bytes=len(payload))
    return payload, "application/json"


# ---------------------------------------------------------------------------
# Environment snapshot
# ---------------------------------------------------------------------------

# Env var prefixes to include.  All others are redacted to protect secrets.
_SAFE_ENV_PREFIXES = (
    "PATH",
    "HOME",
    "USER",
    "SHELL",
    "LANG",
    "LC_",
    "TZ",
    "HOSTNAME",
    "PYTHONPATH",
    "VIRTUAL_ENV",
    "CONTROL_PLANE",
    "AGENT_ID",
    "OBSERVE_TARGET",
    "POLL_INTERVAL",
)


def _environment_snapshot() -> tuple[bytes, str]:
    """Collect a rich snapshot of the current runtime environment."""
    logger.info("observe.environment_snapshot")

    # Disk usage for all mounted filesystems (best-effort).
    disk_info: list[dict[str, Any]] = []
    try:
        import shutil

        for partition in _mounted_partitions():
            try:
                usage = shutil.disk_usage(partition)
                disk_info.append(
                    {
                        "mount": partition,
                        "total_bytes": usage.total,
                        "used_bytes": usage.used,
                        "free_bytes": usage.free,
                    }
                )
            except (OSError, PermissionError):
                pass
    except Exception:
        pass

    # Load average (Unix only).
    load: list[float] | None = None
    try:
        load = list(os.getloadavg())
    except AttributeError:
        pass  # Windows

    # CPU count.
    cpu_count: int | None = os.cpu_count()

    # Memory (read from /proc/meminfo on Linux; best-effort elsewhere).
    memory: dict[str, int] | None = _read_meminfo()

    # Network interfaces.
    net_interfaces = _net_interfaces()

    # Sanitised environment variables.
    safe_env: dict[str, str] = {}
    for k, v in os.environ.items():
        if any(k.startswith(prefix) for prefix in _SAFE_ENV_PREFIXES):
            safe_env[k] = v
        else:
            safe_env[k] = "<redacted>"

    observation: dict[str, Any] = {
        "observed_at": _utcnow(),
        "source": "environment",
        "platform": {
            "system": platform.system(),
            "release": platform.release(),
            "version": platform.version(),
            "machine": platform.machine(),
            "processor": platform.processor(),
            "python_version": platform.python_version(),
            "node": platform.node(),
        },
        "hostname": _hostname(),
        "cpu_count": cpu_count,
        "load_avg_1_5_15": load,
        "memory": memory,
        "disk": disk_info,
        "network_interfaces": net_interfaces,
        "env": safe_env,
        "pid": os.getpid(),
        "uptime_seconds": _uptime(),
    }

    payload = _encode(observation)
    logger.info("observe.environment_snapshot.done", bytes=len(payload))
    return payload, "application/json"


def _mounted_partitions() -> list[str]:
    """Return mount points by reading /proc/mounts (Linux) or falling back."""
    try:
        with open("/proc/mounts") as fh:
            return [
                line.split()[1]
                for line in fh
                if not line.startswith("#") and len(line.split()) >= 2
            ]
    except OSError:
        return ["/"]


def _read_meminfo() -> dict[str, int] | None:
    try:
        info: dict[str, int] = {}
        with open("/proc/meminfo") as fh:
            for line in fh:
                parts = line.split()
                if len(parts) >= 2:
                    # Value is in kB; convert to bytes.
                    key = parts[0].rstrip(":")
                    info[key] = int(parts[1]) * 1024
        return info or None
    except OSError:
        return None


def _net_interfaces() -> dict[str, Any]:
    """Best-effort list of non-loopback IP addresses per interface."""
    ifaces: dict[str, list[str]] = {}
    try:
        for iface, addrs in socket.getaddrinfo(socket.gethostname(), None):
            pass
    except Exception:
        pass
    # Read from /proc/net/if_inet6 and /proc/net/fib_trie for a fuller picture
    # without needing psutil.  Fall back gracefully.
    try:
        import subprocess
        result = subprocess.run(
            ["ip", "-j", "addr"],
            capture_output=True,
            text=True,
            timeout=5,
        )
        if result.returncode == 0:
            import json as _json
            return _json.loads(result.stdout)
    except Exception:
        pass
    return ifaces


def _hostname() -> str:
    try:
        return socket.getfqdn()
    except Exception:
        return platform.node()


def _uptime() -> float | None:
    try:
        with open("/proc/uptime") as fh:
            return float(fh.read().split()[0])
    except OSError:
        return None


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _utcnow() -> str:
    """ISO 8601 UTC timestamp."""
    from datetime import datetime, timezone
    return datetime.now(tz=timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def _encode(obj: Any) -> bytes:
    return json.dumps(obj, separators=(",", ":"), default=str).encode("utf-8")
