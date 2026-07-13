"""Public data models and validation for same-host leases."""

from __future__ import annotations

from dataclasses import dataclass
import datetime as dt
import math
from typing import Any, Mapping

DEFAULT_TTL = 900.0
MAX_TTL = 3600.0


class LeaseError(Exception):
    """A deterministic lease operation failure."""

    def __init__(self, reason: str, *, code: int = 2, **details: Any) -> None:
        super().__init__(reason)
        self.reason = reason
        self.code = code
        self.details = details

    def as_dict(self) -> dict[str, Any]:
        return {"error": self.reason, **self.details}


ClaimError = LeaseError


def iso8601(value: float) -> str:
    """Render persisted epoch seconds as a UTC timestamp."""

    return (
        dt.datetime.fromtimestamp(value, tz=dt.timezone.utc)
        .isoformat()
        .replace("+00:00", "Z")
    )


def require_resource(value: str) -> str:
    """Validate without interpreting the caller-owned resource value."""

    if not isinstance(value, str) or not value.strip():
        raise LeaseError("invalid-resource", code=64)
    return value


def require_text(value: str, field: str) -> str:
    if not isinstance(value, str) or not value.strip():
        raise LeaseError(f"invalid-{field}", code=64, field=field)
    return value


def require_ttl(value: float) -> float:
    try:
        ttl = float(value)
    except (TypeError, ValueError) as error:
        raise LeaseError("invalid-ttl", code=64, ttl=value) from error
    if not math.isfinite(ttl) or ttl <= 0 or ttl > MAX_TTL:
        raise LeaseError(
            "invalid-ttl",
            code=64,
            ttl=ttl,
            minimumExclusive=0,
            maximumInclusive=MAX_TTL,
        )
    return ttl


@dataclass(frozen=True, slots=True)
class AcquireRequest:
    resource: str
    claim_id: str
    agent_id: str
    session_id: str
    owner_id: str
    work_key: str
    ttl: float = DEFAULT_TTL
    coordination_only: bool = False

    def __post_init__(self) -> None:
        require_resource(self.resource)
        for value, field in (
            (self.claim_id, "claim-id"),
            (self.agent_id, "agent-id"),
            (self.session_id, "session-id"),
            (self.owner_id, "owner-id"),
            (self.work_key, "work-key"),
        ):
            require_text(value, field)
        require_ttl(self.ttl)

    @property
    def identity(self) -> tuple[str, str, str, str, bool]:
        return (
            self.agent_id,
            self.session_id,
            self.owner_id,
            self.work_key,
            self.coordination_only,
        )

    def request_dict(self) -> dict[str, Any]:
        return {
            "agentId": self.agent_id,
            "sessionId": self.session_id,
            "ownerId": self.owner_id,
            "workKey": self.work_key,
            "ttl": float(self.ttl),
            "coordinationOnly": self.coordination_only,
        }


@dataclass(frozen=True, slots=True)
class MutationRequest:
    resource: str
    claim_id: str
    token: str
    revision: int
    operation_id: str
    ttl: float = DEFAULT_TTL

    def __post_init__(self) -> None:
        require_resource(self.resource)
        require_text(self.claim_id, "claim-id")
        require_text(self.token, "token")
        require_text(self.operation_id, "operation-id")
        if isinstance(self.revision, bool) or not isinstance(self.revision, int):
            raise LeaseError("invalid-revision", code=64, revision=self.revision)
        if self.revision < 1:
            raise LeaseError("invalid-revision", code=64, revision=self.revision)
        require_ttl(self.ttl)

    def request_dict(self, **extra: Any) -> dict[str, Any]:
        value: dict[str, Any] = {"revision": self.revision, "ttl": float(self.ttl)}
        value.update(extra)
        return value


@dataclass(frozen=True, slots=True)
class Claim:
    resource: str
    claim_id: str
    token: str
    revision: int
    agent_id: str
    session_id: str
    owner_id: str
    work_key: str
    guarantee: str
    acquired_at: float
    acquire_ttl: float
    heartbeat_at: float
    expires_at: float
    active: bool

    def to_dict(self, *, include_token: bool = True) -> dict[str, Any]:
        result: dict[str, Any] = {
            "resource": self.resource,
            "claimId": self.claim_id,
            "revision": self.revision,
            "agentId": self.agent_id,
            "sessionId": self.session_id,
            "ownerId": self.owner_id,
            "workKey": self.work_key,
            "guarantee": self.guarantee,
            "acquiredAt": iso8601(self.acquired_at),
            "acquireTtl": self.acquire_ttl,
            "heartbeatAt": iso8601(self.heartbeat_at),
            "expiresAt": iso8601(self.expires_at),
            "expiresAtEpoch": self.expires_at,
            "active": self.active,
        }
        if include_token:
            result["token"] = self.token
        return result


def claim_from_row(row: Any, now: float) -> Claim:
    return Claim(
        resource=str(row["resource"]),
        claim_id=str(row["claim_id"]),
        token=str(row["token"]),
        revision=int(row["revision"]),
        agent_id=str(row["agent_id"]),
        session_id=str(row["session_id"]),
        owner_id=str(row["owner_id"]),
        work_key=str(row["work_key"]),
        guarantee=(
            "local-coordination" if bool(row["coordination_only"]) else "fenced"
        ),
        acquired_at=float(row["acquired_at"]),
        acquire_ttl=float(row["acquire_ttl"]),
        heartbeat_at=float(row["heartbeat_at"]),
        expires_at=float(row["expires_at"]),
        active=now < float(row["expires_at"]),
    )
