"""Wire models for the open-cognition control plane API.

These dataclasses mirror the Go models in internal/models/types.go and the
JSON schemas in schemas/. Any field name change there must be reflected here.
"""

from __future__ import annotations

import dataclasses
from typing import Any


@dataclasses.dataclass
class SystemState:
    mode: str
    changed_by: str
    changed_at: str

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> "SystemState":
        return cls(
            mode=d["mode"],
            changed_by=d.get("changed_by", ""),
            changed_at=d.get("changed_at", ""),
        )


@dataclasses.dataclass
class CanonicalObject:
    schema_version: str
    id: str
    object_type: str
    content_type: str
    size_bytes: int
    created_at: str
    created_by: str
    storage_path: str
    metadata: dict[str, Any] | None = None

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {
            "schema_version": self.schema_version,
            "id": self.id,
            "object_type": self.object_type,
            "content_type": self.content_type,
            "size_bytes": self.size_bytes,
            "created_at": self.created_at,
            "created_by": self.created_by,
            "storage_path": self.storage_path,
        }
        if self.metadata:
            d["metadata"] = self.metadata
        return d

    @classmethod
    def from_dict(cls, d: dict[str, Any]) -> "CanonicalObject":
        return cls(
            schema_version=d["schema_version"],
            id=d["id"],
            object_type=d["object_type"],
            content_type=d["content_type"],
            size_bytes=d["size_bytes"],
            created_at=d["created_at"],
            created_by=d["created_by"],
            storage_path=d["storage_path"],
            metadata=d.get("metadata"),
        )


@dataclasses.dataclass
class CreateCanonicalRequest:
    """POST /canonical request body.

    payload must be the raw bytes of the object; this class base64-encodes it
    for the wire format automatically via to_dict().
    """

    schema_version: str
    id: str
    object_type: str
    content_type: str
    size_bytes: int
    created_at: str
    created_by: str
    storage_path: str
    payload: bytes
    metadata: dict[str, Any] | None = None

    def to_dict(self) -> dict[str, Any]:
        import base64

        d: dict[str, Any] = {
            "schema_version": self.schema_version,
            "id": self.id,
            "object_type": self.object_type,
            "content_type": self.content_type,
            "size_bytes": self.size_bytes,
            "created_at": self.created_at,
            "created_by": self.created_by,
            "storage_path": self.storage_path,
            # Go's encoding/json marshals []byte as standard base64.
            "payload": base64.b64encode(self.payload).decode(),
        }
        if self.metadata:
            d["metadata"] = self.metadata
        return d


@dataclasses.dataclass
class AgentReference:
    schema_version: str
    id: str
    canonical_object_id: str
    agent_id: str
    created_at: str
    context: str
    relevance: float | None = None
    trust_weight: float | None = None
    time_horizon: str | None = None
    signature: str | None = None
    public_key: str | None = None
    metadata: dict[str, Any] | None = None

    def to_dict(self) -> dict[str, Any]:
        d: dict[str, Any] = {
            "schema_version": self.schema_version,
            "id": self.id,
            "canonical_object_id": self.canonical_object_id,
            "agent_id": self.agent_id,
            "created_at": self.created_at,
            "context": self.context,
        }
        if self.relevance is not None:
            d["relevance"] = self.relevance
        if self.trust_weight is not None:
            d["trust_weight"] = self.trust_weight
        if self.time_horizon is not None:
            d["time_horizon"] = self.time_horizon
        if self.signature is not None:
            d["signature"] = self.signature
        if self.public_key is not None:
            d["public_key"] = self.public_key
        if self.metadata:
            d["metadata"] = self.metadata
        return d
