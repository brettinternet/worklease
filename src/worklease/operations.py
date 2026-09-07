"""Durable operation intents, receipts, and shared claim advancement."""

from __future__ import annotations

import json
import sqlite3
from contextlib import closing, nullcontext
from dataclasses import dataclass
from typing import Any

from .locking import resource_lock, resource_locks
from .models import (
    BundleMutationRequest,
    LeaseError,
    MutationRequest,
    lease_is_active,
    require_ttl,
)
from .sqlite import transaction

_OperationRequest = MutationRequest | BundleMutationRequest


@dataclass(frozen=True, slots=True)
class _OperationResource:
    """The resource-specific behavior needed by one ledger operation."""

    key: str
    label: str
    bundle: bool
    lock: Any
    owner: Any
    current: Any
    row: Any
    claim: Any


class OperationLedgerMixin:
    """Implement operation-ledger behavior once for singleton and bundle claims."""

    def _operation_resource(
        self: Any,
        request: _OperationRequest,
        *,
        lock_held: bool = False,
    ) -> _OperationResource:
        if isinstance(request, BundleMutationRequest):
            return _OperationResource(
                key=request.resource,
                label=",".join(request.resources),
                bundle=True,
                lock=(
                    lambda: (
                        nullcontext()
                        if lock_held
                        else resource_locks(request.resources, self.home)
                    )
                ),
                owner=lambda db: self._require_bundle_owner(db, request),
                current=lambda db: self._require_bundle_current(db, request),
                row=lambda db: self._bundle_row(db, request.claim_id),
                claim=lambda db, row, include_token: self._bundle_claim(
                    db, row
                ).to_dict(include_token=include_token),
            )
        return _OperationResource(
            key=request.resource,
            label=request.resource,
            bundle=False,
            lock=(
                lambda: (
                    nullcontext()
                    if lock_held
                    else resource_lock(request.resource, self.home)
                )
            ),
            owner=lambda db: self._require_owner(db, request),
            current=lambda db: self._require_current(db, request),
            row=lambda db: self._current(db, request.resource),
            claim=lambda db, row, include_token: self._claim(row).to_dict(
                include_token=include_token
            ),
        )

    @staticmethod
    def _without_revision(value: dict[str, Any]) -> dict[str, Any]:
        return {key: item for key, item in value.items() if key != "revision"}

    def _operation_row(
        self: Any, connection: sqlite3.Connection, request: _OperationRequest
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

    @staticmethod
    def _restore_owner_token(
        request: _OperationRequest,
        kind: str,
        receipt: dict[str, Any],
    ) -> None:
        """Keep the established owner response while storing redacted receipts."""

        if isinstance(request, BundleMutationRequest):
            if kind in {"exec-bundle", "reconcile-operation-bundle"}:
                return
        elif kind not in {"heartbeat", "checkpoint"}:
            return
        claim = receipt.get("claim")
        if isinstance(claim, dict):
            claim["token"] = request.token

    def _cached_operation(
        self: Any,
        connection: sqlite3.Connection,
        request: _OperationRequest,
        kind: str,
        expected: dict[str, Any],
    ) -> dict[str, Any] | None:
        row = self._operation_row(connection, request)
        if row is None:
            return None
        recorded = json.loads(str(row["request"]))
        if str(row["kind"]) != kind or self._without_revision(
            recorded
        ) != self._without_revision(expected):
            raise LeaseError(
                "operation-id-request-mismatch",
                code=3,
                operationId=request.operation_id,
            )
        expected_revision = int(row["expected_revision"])
        if request.revision != expected_revision:
            raise LeaseError(
                "stale-revision",
                resource=(
                    ",".join(request.resources)
                    if isinstance(request, BundleMutationRequest)
                    else request.resource
                ),
                expectedRevision=expected_revision,
                suppliedRevision=request.revision,
            )
        if str(row["state"]) == "started":
            raise LeaseError(
                "unknown-outcome",
                code=3,
                operationId=request.operation_id,
                operation=kind,
            )
        receipt = json.loads(str(row["receipt"]))
        self._restore_owner_token(request, kind, receipt)
        receipt["idempotent"] = True
        return receipt

    def _receipt_includes_token(
        self: Any,
        kind: str,
        request: _OperationRequest,
        *,
        complete_operation: bool = False,
        include_token: bool | None = None,
    ) -> bool:
        if include_token is not None:
            return include_token
        return not (
            complete_operation
            or kind
            in {
                "exec",
                "exec-bundle",
                "reconcile-operation",
                "reconcile-operation-bundle",
            }
        )

    def _advance_operation(
        self: Any,
        connection: sqlite3.Connection,
        row: sqlite3.Row,
        request: _OperationRequest,
        kind: str,
        operation_request: dict[str, Any],
        receipt: dict[str, Any],
        *,
        checkpoint: str | None = None,
        complete_operation: bool = False,
        record_operation: bool = True,
        include_token: bool | None = None,
    ) -> dict[str, Any]:
        """Advance either claim shape and optionally write its ledger record."""

        behavior = self._operation_resource(request, lock_held=True)
        now = self.clock()
        revision = int(row["revision"]) + 1
        ttl = require_ttl(request.ttl)
        if behavior.bundle:
            assert isinstance(request, BundleMutationRequest)
            updated = connection.execute(
                """
                UPDATE bundles
                SET revision = ?, heartbeat_at = ?, expires_at = ?
                WHERE claim_id = ? AND token = ? AND revision = ?
                """,
                (
                    revision,
                    now,
                    now + ttl,
                    request.claim_id,
                    request.token,
                    request.revision,
                ),
            )
            if updated.rowcount != 1:
                raise LeaseError(
                    "claim-update-conflict", code=3, resource=behavior.label
                )
            for resource in request.resources:
                member = connection.execute(
                    """
                    UPDATE claims
                    SET revision = ?, heartbeat_at = ?, expires_at = ?
                    WHERE resource = ? AND claim_id = ? AND token = ? AND revision = ?
                    """,
                    (
                        revision,
                        now,
                        now + ttl,
                        resource,
                        request.claim_id,
                        request.token,
                        request.revision,
                    ),
                )
                if member.rowcount != 1:
                    raise LeaseError(
                        "claim-update-conflict", code=3, resource=behavior.label
                    )
                connection.execute(
                    """
                    INSERT INTO resources(resource, revision) VALUES (?, ?)
                    ON CONFLICT(resource) DO UPDATE SET revision = excluded.revision
                    """,
                    (resource, revision),
                )
        else:
            updated = connection.execute(
                """
                UPDATE claims
                SET revision = ?, heartbeat_at = ?, expires_at = ?,
                    checkpoint = COALESCE(?, checkpoint)
                WHERE resource = ? AND claim_id = ? AND token = ? AND revision = ?
                """,
                (
                    revision,
                    now,
                    now + ttl,
                    checkpoint,
                    request.resource,
                    request.claim_id,
                    request.token,
                    request.revision,
                ),
            )
            if updated.rowcount != 1:
                raise LeaseError(
                    "claim-update-conflict", code=3, resource=behavior.label
                )
            connection.execute(
                """
                INSERT INTO resources(resource, revision) VALUES (?, ?)
                ON CONFLICT(resource) DO UPDATE SET revision = excluded.revision
                """,
                (request.resource, revision),
            )

        current = behavior.row(connection)
        if current is None:
            raise LeaseError("claim-update-conflict", code=3, resource=behavior.label)
        receipt["claim"] = behavior.claim(
            connection,
            current,
            self._receipt_includes_token(
                kind,
                request,
                complete_operation=complete_operation,
                include_token=include_token,
            ),
        )
        if not record_operation:
            return receipt

        persisted_receipt = dict(receipt)
        persisted_receipt["claim"] = behavior.claim(connection, current, False)
        encoded_receipt = json.dumps(
            persisted_receipt, sort_keys=True, separators=(",", ":")
        )
        if complete_operation:
            changed = connection.execute(
                """
                UPDATE operations
                SET state = 'completed', receipt = ?
                WHERE resource = ? AND claim_id = ? AND operation_id = ?
                    AND kind = ? AND state = 'started'
                """,
                (
                    encoded_receipt,
                    behavior.key,
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
        else:
            connection.execute(
                """
                INSERT INTO operations(
                    resource, claim_id, operation_id, kind, request,
                    expected_revision, receipt, created_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    behavior.key,
                    request.claim_id,
                    request.operation_id,
                    kind,
                    json.dumps(
                        operation_request, sort_keys=True, separators=(",", ":")
                    ),
                    request.revision,
                    encoded_receipt,
                    now,
                ),
            )
        return receipt

    def _begin_operation(
        self: Any,
        request: _OperationRequest,
        kind: str,
        operation_request: dict[str, Any],
        *,
        lock_held: bool = False,
        connection: sqlite3.Connection | None = None,
    ) -> dict[str, Any] | None:
        behavior = self._operation_resource(request, lock_held=lock_held)
        connection = connection or self._active_connection()
        db_context = (
            closing(self._connect()) if connection is None else nullcontext(connection)
        )
        with behavior.lock(), db_context as db, transaction(db):
            behavior.owner(db)
            cached = self._cached_operation(db, request, kind, operation_request)
            if cached is not None:
                return cached
            behavior.current(db)
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
                    behavior.key,
                    request.claim_id,
                    request.operation_id,
                    kind,
                    json.dumps(
                        operation_request, sort_keys=True, separators=(",", ":")
                    ),
                    request.revision,
                    json.dumps(intent, sort_keys=True, separators=(",", ":")),
                    self.clock(),
                ),
            )
            return None

    def _complete_operation(
        self: Any,
        request: _OperationRequest,
        kind: str,
        operation_request: dict[str, Any],
        receipt: dict[str, Any],
        *,
        lock_held: bool = False,
        connection: sqlite3.Connection | None = None,
    ) -> dict[str, Any]:
        behavior = self._operation_resource(request, lock_held=lock_held)
        connection = connection or self._active_connection()
        db_context = (
            closing(self._connect()) if connection is None else nullcontext(connection)
        )
        with behavior.lock(), db_context as db, transaction(db):
            row = behavior.owner(db)
            if not behavior.bundle:
                self._validate_current_row(row, request, behavior.label)
            operation = self._operation_row(db, request)
            if behavior.bundle and (
                operation is None or str(operation["kind"]) != kind
            ):
                operation = None
            if operation is None:
                raise LeaseError(
                    "operation-not-found", code=3, operationId=request.operation_id
                )
            recorded = json.loads(str(operation["request"]))
            if self._without_revision(recorded) != self._without_revision(
                operation_request
            ):
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
                        resource=behavior.label,
                        expectedRevision=expected_revision,
                        suppliedRevision=request.revision,
                    )
                result = json.loads(str(operation["receipt"]))
                self._restore_owner_token(request, kind, result)
                result["idempotent"] = True
                return result
            if state != "started":
                raise LeaseError(
                    "invalid-operation-state",
                    code=3,
                    operationId=request.operation_id,
                )
            if behavior.bundle:
                behavior.current(db)
            return self._advance_operation(
                db,
                row,
                request,
                kind,
                operation_request,
                receipt,
                complete_operation=True,
                include_token=(
                    behavior.bundle
                    and kind not in {"exec-bundle", "reconcile-operation-bundle"}
                ),
            )

    def _validate_current_row(
        self: Any,
        row: sqlite3.Row,
        request: _OperationRequest,
        resource: str,
    ) -> None:
        if int(row["revision"]) != request.revision:
            raise LeaseError(
                "stale-revision",
                resource=resource,
                expectedRevision=int(row["revision"]),
                suppliedRevision=request.revision,
            )
        if not lease_is_active(row, self.clock()):
            raise LeaseError(
                "claim-expired",
                resource=resource,
                claim=self._claim(row).to_dict(include_token=False),
            )

    def _heartbeat_operation(
        self: Any,
        request: _OperationRequest,
        *,
        lock_held: bool = False,
        internal_renewal: bool = False,
        record_operation: bool = True,
        connection: sqlite3.Connection | None = None,
    ) -> dict[str, Any]:
        kind = (
            "heartbeat-bundle"
            if isinstance(request, BundleMutationRequest)
            else "heartbeat"
        )
        ttl = require_ttl(request.ttl)
        behavior = self._operation_resource(request, lock_held=lock_held)
        connection = connection or self._active_connection()
        record_operation = record_operation and not self._internal_heartbeats_enabled()
        db_context = (
            closing(self._connect()) if connection is None else nullcontext(connection)
        )
        with behavior.lock(), db_context as db, transaction(db):
            row = behavior.owner(db)
            operation_request = request.request_dict()
            if record_operation:
                cached = self._cached_operation(db, request, kind, operation_request)
                if cached is not None:
                    return cached
            behavior.current(db)
            receipt: dict[str, Any] = {
                "ok": True,
                "operation": kind,
                "operationId": request.operation_id,
                "idempotent": False,
                "ttl": ttl,
            }
            if behavior.bundle:
                assert isinstance(request, BundleMutationRequest)
                receipt["resources"] = list(request.resources)
            result = self._advance_operation(
                db,
                row,
                request,
                kind,
                operation_request,
                receipt,
                record_operation=record_operation,
                include_token=not internal_renewal,
            )
            self._restore_owner_token(request, kind, result)
            return result

    def begin_operation(
        self: Any,
        request: MutationRequest,
        kind: str,
        operation_request: dict[str, Any],
        *,
        lock_held: bool = False,
        connection: sqlite3.Connection | None = None,
    ) -> dict[str, Any] | None:
        """Durably record a singleton operation intent before its side effect."""

        return self._begin_operation(
            request,
            kind,
            operation_request,
            lock_held=lock_held,
            connection=connection,
        )

    def complete_operation(
        self: Any,
        request: MutationRequest,
        kind: str,
        operation_request: dict[str, Any],
        receipt: dict[str, Any],
        *,
        lock_held: bool = False,
        connection: sqlite3.Connection | None = None,
    ) -> dict[str, Any]:
        """Persist a started singleton operation's receipt and advance its claim."""

        return self._complete_operation(
            request,
            kind,
            operation_request,
            receipt,
            lock_held=lock_held,
            connection=connection,
        )

    def begin_bundle_operation(
        self: Any,
        request: BundleMutationRequest,
        kind: str,
        operation_request: dict[str, Any],
        *,
        lock_held: bool = False,
        connection: sqlite3.Connection | None = None,
    ) -> dict[str, Any] | None:
        """Durably record a bundle operation intent before its side effect."""

        return self._begin_operation(
            request,
            kind,
            operation_request,
            lock_held=lock_held,
            connection=connection,
        )

    def complete_bundle_operation(
        self: Any,
        request: BundleMutationRequest,
        kind: str,
        operation_request: dict[str, Any],
        receipt: dict[str, Any],
        *,
        lock_held: bool = False,
        connection: sqlite3.Connection | None = None,
    ) -> dict[str, Any]:
        """Persist a started bundle operation's receipt and advance its claim."""

        return self._complete_operation(
            request,
            kind,
            operation_request,
            receipt,
            lock_held=lock_held,
            connection=connection,
        )

    def _heartbeat_for_exec(
        self: Any,
        request: _OperationRequest,
        *,
        lock_held: bool = False,
    ) -> dict[str, Any]:
        """Renew for guarded execution without persisting the bearer token."""

        return self._heartbeat_operation(
            request,
            lock_held=lock_held,
            internal_renewal=True,
        )

    def heartbeat(
        self: Any,
        request: MutationRequest,
        *,
        lock_held: bool = False,
        record_operation: bool = True,
        connection: sqlite3.Connection | None = None,
    ) -> dict[str, Any]:
        """Renew a singleton claim and advance its revision."""

        return self._heartbeat_operation(
            request,
            lock_held=lock_held,
            record_operation=record_operation,
            connection=connection,
        )

    def heartbeat_bundle(
        self: Any,
        request: BundleMutationRequest,
        *,
        lock_held: bool = False,
        record_operation: bool = True,
        connection: sqlite3.Connection | None = None,
    ) -> dict[str, Any]:
        """Renew every member and advance one shared bundle revision."""

        return self._heartbeat_operation(
            request,
            lock_held=lock_held,
            record_operation=record_operation,
            connection=connection,
        )

    def checkpoint(
        self: Any,
        request: MutationRequest,
        value: Any,
        *,
        lock_held: bool = False,
    ) -> dict[str, Any]:
        """Persist a bounded checkpoint while renewing singleton ownership."""

        serialized = self._serialize_checkpoint(value)
        operation_request = self._receipt_request(
            request, checkpoint=json.loads(serialized)
        )
        behavior = self._operation_resource(request, lock_held=lock_held)
        with behavior.lock(), closing(self._connect()) as db, transaction(db):
            row = behavior.owner(db)
            cached = self._cached_operation(
                db, request, "checkpoint", operation_request
            )
            if cached is not None:
                return cached
            behavior.current(db)
            receipt: dict[str, Any] = {
                "ok": True,
                "operation": "checkpoint",
                "operationId": request.operation_id,
                "idempotent": False,
                "checkpoint": json.loads(serialized),
                "checkpointBytes": len(serialized.encode("utf-8")),
            }
            result = self._advance_operation(
                db,
                row,
                request,
                "checkpoint",
                operation_request,
                receipt,
                checkpoint=serialized,
            )
            self._restore_owner_token(request, "checkpoint", result)
            return result

    @staticmethod
    def _serialize_checkpoint(value: Any) -> str:
        from .models import serialize_checkpoint

        return serialize_checkpoint(value)


__all__ = ["OperationLedgerMixin"]
