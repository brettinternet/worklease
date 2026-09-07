"""Opaque-resource lease lifecycle backed by SQLite and POSIX locks."""

from __future__ import annotations

import hashlib
import json
import secrets
import sqlite3
import time
from collections.abc import Callable, Iterator
from contextlib import closing, contextmanager
from contextvars import ContextVar
from pathlib import Path
from typing import Any

from . import garbage_collection as _garbage_collection
from .acquisition import AcquisitionMixin
from .claims import ClaimStoreMixin
from .garbage_collection import GarbageCollectionMixin
from .lifecycle import LifecycleMixin
from .locking import resource_lock, resource_locks
from .models import (
    Claim,
    LeaseError,
    MutationRequest,
    claim_from_row,
    require_bundle_resources,
    require_resource,
    require_text,
)
from .operations import OperationLedgerMixin
from .projections import ProjectionMixin
from .reconciliation import ReconciliationMixin
from .sqlite import connect, lease_home, transaction

DEFAULT_GC_RETENTION_DAYS = _garbage_collection.DEFAULT_GC_RETENTION_DAYS
_ACTIVE_CONNECTION: ContextVar[sqlite3.Connection | None] = ContextVar(
    "worklease_active_connection", default=None
)
_INTERNAL_HEARTBEATS: ContextVar[bool] = ContextVar(
    "worklease_internal_heartbeats", default=False
)


class LeaseStore(
    ClaimStoreMixin,
    OperationLedgerMixin,
    ReconciliationMixin,
    ProjectionMixin,
    GarbageCollectionMixin,
    AcquisitionMixin,
    LifecycleMixin,
):
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

    @contextmanager
    def _acquire_transaction(
        self, connection: sqlite3.Connection, resource: str
    ) -> Iterator[None]:
        bundle = self._bundle_for_resource(connection, resource)
        if bundle is not None:
            raise LeaseError(
                "bundle-operation-required",
                resource=resource,
                claim=self._bundle_claim(connection, bundle).to_dict(
                    include_token=False
                ),
            )
        with resource_lock(resource, self.home), transaction(connection):
            yield

    @contextmanager
    def _connection_context(
        self, connection: sqlite3.Connection, *, internal_heartbeats: bool = False
    ) -> Iterator[None]:
        connection_token = _ACTIVE_CONNECTION.set(connection)
        heartbeat_token = _INTERNAL_HEARTBEATS.set(internal_heartbeats)
        try:
            yield
        finally:
            _INTERNAL_HEARTBEATS.reset(heartbeat_token)
            _ACTIVE_CONNECTION.reset(connection_token)

    @staticmethod
    def _active_connection() -> sqlite3.Connection | None:
        return _ACTIVE_CONNECTION.get()

    @staticmethod
    def _internal_heartbeats_enabled() -> bool:
        return _INTERNAL_HEARTBEATS.get()

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

    def _inspect_operation(
        self,
        db: sqlite3.Connection,
        operation_resource: str,
        operation_id: str,
        identity: dict[str, Any],
        operation: str,
    ) -> dict[str, Any]:
        rows = db.execute(
            """
                SELECT * FROM operations
                WHERE resource = ? AND operation_id = ?
                ORDER BY claim_id, kind
                """,
            (operation_resource, operation_id),
        ).fetchall()
        if not rows:
            raise LeaseError(
                "operation-not-found",
                code=3,
                operationId=operation_id,
            )
        if len(rows) != 1:
            raise LeaseError(
                "operation-id-ambiguous",
                code=3,
                operationId=operation_id,
                **identity,
            )
        row = rows[0]
        reconciliation = db.execute(
            """
                SELECT outcome, reconciliation_operation_id, reconciled_at
                FROM reconciliations
                WHERE resource = ? AND operation_id = ?
                """,
            (operation_resource, operation_id),
        ).fetchone()
        request_json = str(row["request"])
        projection: dict[str, Any] = {
            "ok": True,
            "operation": operation,
            **identity,
            "operationId": operation_id,
            "kind": str(row["kind"]),
            "state": (
                "unknown-outcome"
                if str(row["state"]) == "started"
                else str(row["state"])
            ),
            "expectedRevision": int(row["expected_revision"]),
            "requestSha256": hashlib.sha256(request_json.encode("utf-8")).hexdigest(),
            "createdAt": self._timestamp(float(row["created_at"])),
        }
        if reconciliation is not None:
            projection["state"] = "reconciled"
            projection["outcome"] = str(reconciliation["outcome"])
            projection["reconciliationOperationId"] = str(
                reconciliation["reconciliation_operation_id"]
            )
            projection["reconciledAt"] = self._timestamp(
                float(reconciliation["reconciled_at"])
            )
        return projection

    def inspect_operation(self, resource: str, operation_id: str) -> dict[str, Any]:
        """Read one singleton operation without exposing secrets."""

        require_resource(resource)
        require_text(operation_id, "operation-id")
        with resource_lock(resource, self.home), closing(self._connect()) as db:
            return self._inspect_operation(
                db,
                resource,
                operation_id,
                {"resource": resource},
                "inspect-operation",
            )

    def inspect_bundle_operation(
        self, resources: tuple[str, ...], operation_id: str
    ) -> dict[str, Any]:
        """Read one exact ordered bundle operation without exposing secrets."""

        resources = require_bundle_resources(resources)
        require_text(operation_id, "operation-id")
        with resource_locks(resources, self.home), closing(self._connect()) as db:
            return self._inspect_operation(
                db,
                self._bundle_operation_resource(resources),
                operation_id,
                {"resources": list(resources)},
                "inspect-operation-bundle",
            )

    @staticmethod
    def _timestamp(value: float) -> str:
        from .models import iso8601

        return iso8601(value)
