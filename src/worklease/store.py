"""Opaque-resource lease lifecycle backed by SQLite and POSIX locks."""

from __future__ import annotations

import json
import secrets
import sqlite3
import time
from collections.abc import Callable
from contextlib import closing, nullcontext
from pathlib import Path
from typing import Any

from .locking import resource_lock
from .models import (
    AcquireRequest,
    Claim,
    LeaseError,
    MutationRequest,
    claim_from_row,
    require_resource,
    require_text,
    require_ttl,
)
from .sqlite import connect, lease_home, transaction


class LeaseStore:
    """Coordinate leases for opaque caller-supplied resources on one host."""

    def __init__(
        self,
        home: str | Path | None = None,
        *,
        clock: Callable[[], float] = time.time,
        token_factory: Callable[[], str] = lambda: secrets.token_hex(32),
    ) -> None:
        self.home = lease_home(home)
        self.clock = clock
        self.token_factory = token_factory

    def _connect(self) -> sqlite3.Connection:
        return connect(self.home)

    @staticmethod
    def _current(connection: sqlite3.Connection, resource: str) -> sqlite3.Row | None:
        return connection.execute(
            "SELECT * FROM claims WHERE resource = ?", (resource,)
        ).fetchone()

    def _claim(self, row: Any) -> Claim:
        return claim_from_row(row, self.clock())

    @staticmethod
    def _receipt_request(request: MutationRequest, **extra: Any) -> dict[str, Any]:
        return request.request_dict(**extra)

    @staticmethod
    def _operation_row(
        connection: sqlite3.Connection, request: MutationRequest
    ) -> sqlite3.Row | None:
        return connection.execute(
            """
            SELECT * FROM operations
            WHERE resource = ? AND claim_id = ? AND operation_id = ?
            ORDER BY kind
            LIMIT 1
            """,
            (request.resource, request.claim_id, request.operation_id),
        ).fetchone()

    def _cached_operation(
        self,
        connection: sqlite3.Connection,
        request: MutationRequest,
        kind: str,
        expected: dict[str, Any],
    ) -> dict[str, Any] | None:
        row = self._operation_row(connection, request)
        if row is None:
            return None
        recorded = json.loads(str(row["request"]))
        recorded_without_revision = {
            key: value for key, value in recorded.items() if key != "revision"
        }
        expected_without_revision = {
            key: value for key, value in expected.items() if key != "revision"
        }
        if (
            row["kind"] != kind
            or recorded_without_revision != expected_without_revision
        ):
            raise LeaseError(
                "operation-id-request-mismatch",
                code=3,
                operationId=request.operation_id,
            )
        expected_revision = int(row["expected_revision"])
        if request.revision != expected_revision:
            raise LeaseError(
                "stale-revision",
                resource=request.resource,
                expectedRevision=expected_revision,
                suppliedRevision=request.revision,
            )
        receipt = json.loads(str(row["receipt"]))
        state = str(row["state"])
        if state == "started":
            raise LeaseError(
                "unknown-outcome",
                code=3,
                operationId=request.operation_id,
                operation=kind,
            )
        receipt["idempotent"] = True
        return receipt

    def read_operation(
        self, request: MutationRequest, kind: str
    ) -> dict[str, Any] | None:
        """Read one operation for safe idempotent replay before input loading."""

        with (
            resource_lock(request.resource, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            self._require_owner(db, request)
            row = self._operation_row(db, request)
            if row is None:
                return None
            expected_revision = int(row["expected_revision"])
            if request.revision != expected_revision:
                raise LeaseError(
                    "stale-revision",
                    resource=request.resource,
                    expectedRevision=expected_revision,
                    suppliedRevision=request.revision,
                )
            if row["kind"] != kind:
                raise LeaseError(
                    "operation-id-request-mismatch",
                    code=3,
                    operationId=request.operation_id,
                )
            state = str(row["state"])
            if state == "started":
                raise LeaseError(
                    "unknown-outcome",
                    code=3,
                    operationId=request.operation_id,
                    operation=kind,
                )
            if state != "completed":
                raise LeaseError(
                    "invalid-operation-state",
                    code=3,
                    operationId=request.operation_id,
                )
            receipt = json.loads(str(row["receipt"]))
            receipt["idempotent"] = True
            return {
                "request": json.loads(str(row["request"])),
                "receipt": receipt,
            }

    def _require_owner(
        self, connection: sqlite3.Connection, request: MutationRequest
    ) -> sqlite3.Row:
        row = self._current(connection, request.resource)
        if row is None:
            raise LeaseError("claim-not-found", resource=request.resource)
        if row["claim_id"] != request.claim_id or row["token"] != request.token:
            raise LeaseError(
                "stale-claim",
                resource=request.resource,
                claim=self._claim(row).to_dict(include_token=False),
            )
        return row

    def _require_current(
        self, connection: sqlite3.Connection, request: MutationRequest
    ) -> sqlite3.Row:
        row = self._require_owner(connection, request)
        if int(row["revision"]) != request.revision:
            raise LeaseError(
                "stale-revision",
                resource=request.resource,
                expectedRevision=int(row["revision"]),
                suppliedRevision=request.revision,
            )
        if self.clock() >= float(row["expires_at"]):
            raise LeaseError(
                "claim-expired",
                resource=request.resource,
                claim=self._claim(row).to_dict(include_token=False),
            )
        return row

    def _advance_claim(
        self,
        connection: sqlite3.Connection,
        row: sqlite3.Row,
        request: MutationRequest,
        kind: str,
        operation_request: dict[str, Any],
        receipt: dict[str, Any],
    ) -> dict[str, Any]:
        now = self.clock()
        revision = int(row["revision"]) + 1
        ttl = require_ttl(request.ttl)
        cursor = connection.execute(
            """
            UPDATE claims
            SET revision = ?, heartbeat_at = ?, expires_at = ?
            WHERE resource = ? AND claim_id = ? AND token = ? AND revision = ?
            """,
            (
                revision,
                now,
                now + ttl,
                request.resource,
                request.claim_id,
                request.token,
                request.revision,
            ),
        )
        if cursor.rowcount != 1:
            raise LeaseError("claim-update-conflict", code=3, resource=request.resource)
        connection.execute(
            """
            INSERT INTO resources(resource, revision) VALUES (?, ?)
            ON CONFLICT(resource) DO UPDATE SET revision = excluded.revision
            """,
            (request.resource, revision),
        )
        updated = self._current(connection, request.resource)
        if updated is None:
            raise LeaseError("claim-update-conflict", code=3, resource=request.resource)
        receipt["claim"] = self._claim(updated).to_dict()
        connection.execute(
            """
            INSERT INTO operations(
                resource, claim_id, operation_id, kind, request,
                expected_revision, receipt, created_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                request.resource,
                request.claim_id,
                request.operation_id,
                kind,
                json.dumps(operation_request, sort_keys=True, separators=(",", ":")),
                request.revision,
                json.dumps(receipt, sort_keys=True, separators=(",", ":")),
                now,
            ),
        )
        return receipt

    def begin_operation(
        self,
        request: MutationRequest,
        kind: str,
        operation_request: dict[str, Any],
        *,
        lock_held: bool = False,
    ) -> dict[str, Any] | None:
        """Durably record an operation intent before an external side effect."""

        lock = (
            nullcontext() if lock_held else resource_lock(request.resource, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            self._require_owner(db, request)
            cached = self._cached_operation(db, request, kind, operation_request)
            if cached is not None:
                return cached
            self._require_current(db, request)
            now = self.clock()
            intent = {
                "ok": True,
                "operation": kind,
                "operationId": request.operation_id,
                "state": "started",
                "idempotent": False,
            }
            db.execute(
                """
                    INSERT INTO operations(
                        resource, claim_id, operation_id, kind, state, request,
                        expected_revision, receipt, created_at
                    ) VALUES (?, ?, ?, ?, 'started', ?, ?, ?, ?)
                    """,
                (
                    request.resource,
                    request.claim_id,
                    request.operation_id,
                    kind,
                    json.dumps(
                        operation_request,
                        sort_keys=True,
                        separators=(",", ":"),
                    ),
                    request.revision,
                    json.dumps(intent, sort_keys=True, separators=(",", ":")),
                    now,
                ),
            )
            return None

    def complete_operation(
        self,
        request: MutationRequest,
        kind: str,
        operation_request: dict[str, Any],
        receipt: dict[str, Any],
        *,
        lock_held: bool = False,
    ) -> dict[str, Any]:
        """Persist a started operation's receipt and advance its claim."""

        lock = (
            nullcontext() if lock_held else resource_lock(request.resource, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            row = self._require_owner(db, request)
            if int(row["revision"]) != request.revision:
                raise LeaseError(
                    "stale-revision",
                    resource=request.resource,
                    expectedRevision=int(row["revision"]),
                    suppliedRevision=request.revision,
                )
            if self.clock() >= float(row["expires_at"]):
                raise LeaseError(
                    "claim-expired",
                    resource=request.resource,
                    claim=self._claim(row).to_dict(include_token=False),
                )
            operation = self._operation_row(db, request)
            if operation is None:
                raise LeaseError(
                    "operation-not-found", code=3, operationId=request.operation_id
                )
            recorded = json.loads(str(operation["request"]))
            if {key: value for key, value in recorded.items() if key != "revision"} != {
                key: value
                for key, value in operation_request.items()
                if key != "revision"
            }:
                raise LeaseError(
                    "operation-id-request-mismatch",
                    code=3,
                    operationId=request.operation_id,
                )
            state = str(operation["state"])
            if state == "completed":
                expected_revision = int(operation["expected_revision"])
                if request.revision != expected_revision:
                    raise LeaseError(
                        "stale-revision",
                        resource=request.resource,
                        expectedRevision=expected_revision,
                        suppliedRevision=request.revision,
                    )
                result = json.loads(str(operation["receipt"]))
                result["idempotent"] = True
                return result
            if state != "started":
                raise LeaseError(
                    "invalid-operation-state",
                    code=3,
                    operationId=request.operation_id,
                )

            now = self.clock()
            revision = int(row["revision"]) + 1
            ttl = require_ttl(request.ttl)
            updated = db.execute(
                """
                    UPDATE claims
                    SET revision = ?, heartbeat_at = ?, expires_at = ?
                    WHERE resource = ? AND claim_id = ? AND token = ? AND revision = ?
                    """,
                (
                    revision,
                    now,
                    now + ttl,
                    request.resource,
                    request.claim_id,
                    request.token,
                    request.revision,
                ),
            )
            if updated.rowcount != 1:
                raise LeaseError(
                    "claim-update-conflict", code=3, resource=request.resource
                )
            db.execute(
                """
                    INSERT INTO resources(resource, revision) VALUES (?, ?)
                    ON CONFLICT(resource) DO UPDATE SET revision = excluded.revision
                    """,
                (request.resource, revision),
            )
            current = self._current(db, request.resource)
            if current is None:
                raise LeaseError(
                    "claim-update-conflict", code=3, resource=request.resource
                )
            receipt["claim"] = self._claim(current).to_dict()
            changed = db.execute(
                """
                    UPDATE operations
                    SET state = 'completed', receipt = ?
                    WHERE resource = ? AND claim_id = ? AND operation_id = ?
                        AND kind = ? AND state = 'started'
                    """,
                (
                    json.dumps(receipt, sort_keys=True, separators=(",", ":")),
                    request.resource,
                    request.claim_id,
                    request.operation_id,
                    kind,
                ),
            )
            if changed.rowcount != 1:
                raise LeaseError(
                    "operation-completion-conflict",
                    code=3,
                    operationId=request.operation_id,
                )
            return receipt

    def validate_current(
        self, request: MutationRequest, *, lock_held: bool = False
    ) -> dict[str, Any]:
        """Validate ownership immediately before a guarded side effect."""

        lock = (
            nullcontext() if lock_held else resource_lock(request.resource, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            row = self._require_current(db, request)
            return self._claim(row).to_dict()

    def acquire(self, request: AcquireRequest) -> dict[str, Any]:
        """Acquire or idempotently retry one ownership epoch."""

        require_resource(request.resource)
        ttl = require_ttl(request.ttl)
        now = self.clock()
        with (
            resource_lock(request.resource, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            row = self._current(db, request.resource)
            if row is not None and row["claim_id"] == request.claim_id:
                recorded = (
                    str(row["agent_id"]),
                    str(row["session_id"]),
                    str(row["owner_id"]),
                    str(row["work_key"]),
                    bool(row["coordination_only"]),
                )
                if recorded != request.identity:
                    raise LeaseError(
                        "claim-id-identity-mismatch", resource=request.resource
                    )
                if abs(float(row["acquire_ttl"]) - ttl) > 1e-6:
                    raise LeaseError(
                        "claim-id-request-mismatch",
                        code=3,
                        resource=request.resource,
                        claimId=request.claim_id,
                    )
                if now >= float(row["expires_at"]):
                    raise LeaseError(
                        "claim-expired",
                        resource=request.resource,
                        claim=self._claim(row).to_dict(include_token=False),
                    )
                return {
                    "ok": True,
                    "operation": "acquire",
                    "idempotent": True,
                    "claim": self._claim(row).to_dict(),
                }
            if row is not None and now < float(row["expires_at"]):
                raise LeaseError(
                    "already-claimed",
                    resource=request.resource,
                    claim=self._claim(row).to_dict(include_token=False),
                )
            epoch = db.execute(
                "SELECT resource, acquired_at FROM epochs WHERE claim_id = ?",
                (request.claim_id,),
            ).fetchone()
            if epoch is not None:
                raise LeaseError(
                    "claim-id-reused",
                    resource=request.resource,
                    originalResource=str(epoch["resource"]),
                    originalAcquiredAt=str(epoch["acquired_at"]),
                )
            prior_revision = db.execute(
                "SELECT revision FROM resources WHERE resource = ?",
                (request.resource,),
            ).fetchone()
            revision = (
                max(
                    int(row["revision"]) if row is not None else 0,
                    int(prior_revision["revision"]) if prior_revision else 0,
                )
                + 1
            )
            token = self.token_factory()
            db.execute(
                """
                    INSERT INTO epochs(
                        claim_id, resource, agent_id, session_id, owner_id,
                        work_key, acquired_at
                    ) VALUES (?, ?, ?, ?, ?, ?, ?)
                    """,
                (
                    request.claim_id,
                    request.resource,
                    request.agent_id,
                    request.session_id,
                    request.owner_id,
                    request.work_key,
                    now,
                ),
            )
            db.execute(
                """
                    INSERT INTO resources(resource, revision) VALUES (?, ?)
                    ON CONFLICT(resource) DO UPDATE SET revision = excluded.revision
                    """,
                (request.resource, revision),
            )
            db.execute(
                """
                    INSERT INTO claims(
                        resource, claim_id, token, revision, agent_id, session_id,
                        owner_id, work_key, coordination_only, acquired_at,
                        acquire_ttl, heartbeat_at, expires_at
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    ON CONFLICT(resource) DO UPDATE SET
                        claim_id=excluded.claim_id, token=excluded.token,
                        revision=excluded.revision, agent_id=excluded.agent_id,
                        session_id=excluded.session_id, owner_id=excluded.owner_id,
                        work_key=excluded.work_key,
                        coordination_only=excluded.coordination_only,
                        acquired_at=excluded.acquired_at,
                        acquire_ttl=excluded.acquire_ttl,
                        heartbeat_at=excluded.heartbeat_at,
                        expires_at=excluded.expires_at
                    """,
                (
                    request.resource,
                    request.claim_id,
                    token,
                    revision,
                    request.agent_id,
                    request.session_id,
                    request.owner_id,
                    request.work_key,
                    int(request.coordination_only),
                    now,
                    ttl,
                    now,
                    now + ttl,
                ),
            )
            created = self._current(db, request.resource)
            if created is None:
                raise LeaseError("claim-create-conflict", code=3)
            return {
                "ok": True,
                "operation": "acquire",
                "idempotent": False,
                "reclaimed": row is not None,
                "claim": self._claim(created).to_dict(),
            }

    def heartbeat(
        self, request: MutationRequest, *, lock_held: bool = False
    ) -> dict[str, Any]:
        """Renew a claim and advance its revision."""

        ttl = require_ttl(request.ttl)
        lock = (
            nullcontext() if lock_held else resource_lock(request.resource, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            row = self._require_owner(db, request)
            operation_request = self._receipt_request(request)
            cached = self._cached_operation(db, request, "heartbeat", operation_request)
            if cached is not None:
                return cached
            if int(row["revision"]) != request.revision:
                raise LeaseError(
                    "stale-revision",
                    resource=request.resource,
                    expectedRevision=int(row["revision"]),
                    suppliedRevision=request.revision,
                )
            if self.clock() >= float(row["expires_at"]):
                raise LeaseError(
                    "claim-expired",
                    resource=request.resource,
                    claim=self._claim(row).to_dict(include_token=False),
                )
            receipt: dict[str, Any] = {
                "ok": True,
                "operation": "heartbeat",
                "operationId": request.operation_id,
                "idempotent": False,
                "ttl": ttl,
            }
            return self._advance_claim(
                db,
                row,
                request,
                "heartbeat",
                operation_request,
                receipt,
            )

    def release(self, request: MutationRequest, reason: str) -> dict[str, Any]:
        """Release one active ownership epoch after a durable checkpoint."""

        require_text(reason, "release-reason")
        with (
            resource_lock(request.resource, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            row = self._current(db, request.resource)
            operation_request = self._receipt_request(
                request, operationId=request.operation_id, reason=reason
            )
            if row is None:
                prior = db.execute(
                    "SELECT * FROM releases WHERE resource = ? AND claim_id = ?",
                    (request.resource, request.claim_id),
                ).fetchone()
                if prior is None:
                    raise LeaseError("claim-not-found", resource=request.resource)
                if str(prior["token"]) != request.token:
                    raise LeaseError("stale-claim", resource=request.resource)
                recorded = json.loads(str(prior["request"]))
                if {
                    key: value for key, value in recorded.items() if key != "revision"
                } != {
                    key: value
                    for key, value in operation_request.items()
                    if key != "revision"
                }:
                    raise LeaseError(
                        "operation-id-request-mismatch",
                        code=3,
                        operationId=request.operation_id,
                    )
                if int(prior["revision"]) != request.revision:
                    raise LeaseError(
                        "stale-revision",
                        resource=request.resource,
                        expectedRevision=int(prior["revision"]),
                        suppliedRevision=request.revision,
                    )
                receipt = json.loads(str(prior["receipt"]))
                receipt["idempotent"] = True
                return receipt
            if row["claim_id"] != request.claim_id or row["token"] != request.token:
                raise LeaseError(
                    "stale-claim",
                    resource=request.resource,
                    claim=self._claim(row).to_dict(include_token=False),
                )
            existing = db.execute(
                """
                    SELECT * FROM releases
                    WHERE resource = ? AND claim_id = ?
                    """,
                (request.resource, request.claim_id),
            ).fetchone()
            if existing is not None:
                recorded = json.loads(str(existing["request"]))
                if {
                    key: value for key, value in recorded.items() if key != "revision"
                } != {
                    key: value
                    for key, value in operation_request.items()
                    if key != "revision"
                }:
                    raise LeaseError(
                        "operation-id-request-mismatch",
                        code=3,
                        operationId=request.operation_id,
                    )
                if int(existing["revision"]) != request.revision:
                    raise LeaseError(
                        "stale-revision",
                        resource=request.resource,
                        expectedRevision=int(existing["revision"]),
                        suppliedRevision=request.revision,
                    )
                receipt = json.loads(str(existing["receipt"]))
                receipt["idempotent"] = True
                return receipt
            if int(row["revision"]) != request.revision:
                raise LeaseError(
                    "stale-revision",
                    resource=request.resource,
                    expectedRevision=int(row["revision"]),
                    suppliedRevision=request.revision,
                )
            if self.clock() >= float(row["expires_at"]):
                raise LeaseError(
                    "claim-expired",
                    resource=request.resource,
                    claim=self._claim(row).to_dict(include_token=False),
                )
            receipt: dict[str, Any] = {
                "ok": True,
                "operation": "release",
                "operationId": request.operation_id,
                "idempotent": False,
                "releasedClaimId": request.claim_id,
                "releasedRevision": request.revision,
                "releasedAt": self._timestamp(self.clock()),
                "reason": reason,
            }
            db.execute(
                """
                    INSERT INTO releases(
                        resource, claim_id, token, revision, operation_id,
                        request, released_at, receipt
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                (
                    request.resource,
                    request.claim_id,
                    request.token,
                    request.revision,
                    request.operation_id,
                    json.dumps(
                        operation_request, sort_keys=True, separators=(",", ":")
                    ),
                    self.clock(),
                    json.dumps(receipt, sort_keys=True, separators=(",", ":")),
                ),
            )
            deleted = db.execute(
                """
                    DELETE FROM claims
                    WHERE resource = ? AND claim_id = ? AND token = ? AND revision = ?
                    """,
                (
                    request.resource,
                    request.claim_id,
                    request.token,
                    request.revision,
                ),
            )
            if deleted.rowcount != 1:
                raise LeaseError(
                    "claim-release-conflict", code=3, resource=request.resource
                )
            return receipt

    def status(self, resource: str) -> dict[str, Any]:
        """Read one claim without exposing its bearer token."""

        require_resource(resource)
        with closing(self._connect()) as db:
            row = self._current(db, resource)
        if row is None:
            return {
                "ok": True,
                "operation": "status",
                "resource": resource,
                "state": "free",
            }
        claim = self._claim(row)
        return {
            "ok": True,
            "operation": "status",
            "resource": resource,
            "state": "active" if claim.active else "expired",
            "claim": claim.to_dict(include_token=False),
        }

    def list_claims(self, resource: str | None = None) -> dict[str, Any]:
        """List claims without exposing bearer tokens."""

        if resource is not None:
            require_resource(resource)
        with closing(self._connect()) as db:
            if resource is None:
                rows = db.execute("SELECT * FROM claims ORDER BY resource").fetchall()
            else:
                rows = db.execute(
                    "SELECT * FROM claims WHERE resource = ? ORDER BY resource",
                    (resource,),
                ).fetchall()
        return {
            "ok": True,
            "operation": "list",
            "claims": [self._claim(row).to_dict(include_token=False) for row in rows],
        }

    @staticmethod
    def _timestamp(value: float) -> str:
        from .models import iso8601

        return iso8601(value)
