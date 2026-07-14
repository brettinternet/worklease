"""Opaque-resource lease lifecycle backed by SQLite and POSIX locks."""

from __future__ import annotations

import hashlib
import json
import secrets
import sqlite3
import time
from collections.abc import Callable
from contextlib import closing, nullcontext
from pathlib import Path
from typing import Any

from .locking import resource_lock, resource_locks
from .models import (
    AcquireRequest,
    BundleAcquireRequest,
    BundleClaim,
    BundleMutationRequest,
    Claim,
    LeaseError,
    MutationRequest,
    TransferRequest,
    bundle_claim_from_row,
    claim_from_row,
    deserialize_checkpoint,
    require_bundle_resources,
    require_resource,
    require_text,
    require_ttl,
    serialize_checkpoint,
)
from .sqlite import connect, connect_readonly, lease_home, transaction


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

    def inspect_operation(self, resource: str, operation_id: str) -> dict[str, Any]:
        """Read one operation without requiring ownership or exposing secrets."""

        require_resource(resource)
        require_text(operation_id, "operation-id")
        with (
            resource_lock(resource, self.home),
            closing(self._connect()) as db,
        ):
            rows = db.execute(
                """
                SELECT * FROM operations
                WHERE resource = ? AND operation_id = ?
                ORDER BY claim_id, kind
                """,
                (resource, operation_id),
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
                    resource=resource,
                    operationId=operation_id,
                )
            row = rows[0]
            reconciliation = db.execute(
                """
                SELECT outcome, reconciliation_operation_id, reconciled_at
                FROM reconciliations
                WHERE resource = ? AND operation_id = ?
                """,
                (resource, operation_id),
            ).fetchone()
            request_json = str(row["request"])
            projection: dict[str, Any] = {
                "ok": True,
                "operation": "inspect-operation",
                "resource": resource,
                "operationId": operation_id,
                "kind": str(row["kind"]),
                "state": (
                    "unknown-outcome"
                    if str(row["state"]) == "started"
                    else str(row["state"])
                ),
                "expectedRevision": int(row["expected_revision"]),
                "requestSha256": hashlib.sha256(
                    request_json.encode("utf-8")
                ).hexdigest(),
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

    def reconcile_operation(
        self,
        request: MutationRequest,
        target_operation_id: str,
        expected_request_sha256: str,
        outcome: str,
        evidence: Any,
    ) -> dict[str, Any]:
        """Record an observed result for one started operation without replaying it."""

        require_text(target_operation_id, "target-operation-id")
        require_text(expected_request_sha256, "expected-request-sha256")
        if request.operation_id == target_operation_id:
            raise LeaseError(
                "operation-id-conflict",
                code=64,
                operationId=request.operation_id,
            )
        if len(expected_request_sha256) != 64 or any(
            value not in "0123456789abcdefABCDEF" for value in expected_request_sha256
        ):
            raise LeaseError("invalid-request-sha256", code=64)
        if outcome not in {"observed-success", "observed-failure"}:
            raise LeaseError(
                "invalid-reconciliation-outcome",
                code=64,
                outcome=outcome,
            )
        try:
            evidence_json = serialize_checkpoint(evidence)
        except LeaseError as error:
            raise LeaseError(
                "invalid-evidence", code=error.code, **error.details
            ) from error
        with (
            resource_lock(request.resource, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            owner = self._require_owner(db, request)
            if self.clock() >= float(owner["expires_at"]):
                raise LeaseError(
                    "claim-expired",
                    resource=request.resource,
                    claim=self._claim(owner).to_dict(include_token=False),
                )
            replay = db.execute(
                """
                SELECT * FROM reconciliations
                WHERE resource = ? AND reconciliation_operation_id = ?
                """,
                (request.resource, request.operation_id),
            ).fetchone()
            if replay is not None:
                if (
                    str(replay["operation_id"]) != target_operation_id
                    or str(replay["outcome"]) != outcome
                    or str(replay["request_sha256"]) != expected_request_sha256
                    or str(replay["evidence"]) != evidence_json
                ):
                    raise LeaseError(
                        "operation-id-request-mismatch",
                        code=3,
                        operationId=request.operation_id,
                    )
                receipt = json.loads(str(replay["receipt"]))
                claim = receipt.get("claim")
                if isinstance(claim, dict) and int(owner["revision"]) > int(
                    claim.get("revision", owner["revision"])
                ):
                    raise LeaseError(
                        "stale-revision",
                        resource=request.resource,
                        expectedRevision=int(owner["revision"]),
                        suppliedRevision=request.revision,
                    )
                receipt["idempotent"] = True
                return receipt
            self._require_current(db, request)
            target_rows = db.execute(
                """
                SELECT * FROM operations
                WHERE resource = ? AND operation_id = ?
                ORDER BY claim_id, kind
                """,
                (request.resource, target_operation_id),
            ).fetchall()
            if not target_rows:
                raise LeaseError(
                    "operation-not-found",
                    code=3,
                    operationId=target_operation_id,
                )
            if len(target_rows) != 1:
                raise LeaseError(
                    "operation-id-ambiguous",
                    code=3,
                    resource=request.resource,
                    operationId=target_operation_id,
                )
            target = target_rows[0]
            if str(target["state"]) != "started":
                raise LeaseError(
                    "operation-not-unknown",
                    code=3,
                    operationId=target_operation_id,
                    state=(
                        "unknown-outcome"
                        if str(target["state"]) == "started"
                        else str(target["state"])
                    ),
                )
            actual_sha256 = hashlib.sha256(
                str(target["request"]).encode("utf-8")
            ).hexdigest()
            if actual_sha256 != expected_request_sha256:
                raise LeaseError(
                    "request-fingerprint-mismatch",
                    code=3,
                    operationId=target_operation_id,
                    expectedRequestSha256=expected_request_sha256,
                )
            existing_target = db.execute(
                """
                SELECT 1 FROM reconciliations
                WHERE resource = ? AND operation_id = ?
                """,
                (request.resource, target_operation_id),
            ).fetchone()
            if existing_target is not None:
                raise LeaseError(
                    "operation-already-reconciled",
                    code=3,
                    operationId=target_operation_id,
                )
            now = self.clock()
            revision = int(owner["revision"]) + 1
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
            receipt: dict[str, Any] = {
                "ok": True,
                "operation": "reconcile-operation",
                "operationId": request.operation_id,
                "targetOperationId": target_operation_id,
                "resource": request.resource,
                "state": "reconciled",
                "outcome": outcome,
                "requestSha256": expected_request_sha256,
                "reconciledAt": self._timestamp(now),
                "idempotent": False,
                "claim": self._claim(current).to_dict(include_token=False),
            }
            db.execute(
                """
                INSERT INTO reconciliations(
                    resource, operation_id, kind, claim_id, outcome, evidence,
                    resolver_agent_id, resolver_session_id, resolver_owner_id,
                    resolver_work_key, request_sha256,
                    reconciliation_operation_id, reconciled_at, receipt
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    request.resource,
                    target_operation_id,
                    str(target["kind"]),
                    str(request.claim_id),
                    outcome,
                    evidence_json,
                    str(current["agent_id"]),
                    str(current["session_id"]),
                    str(current["owner_id"]),
                    str(current["work_key"]),
                    expected_request_sha256,
                    request.operation_id,
                    now,
                    json.dumps(receipt, sort_keys=True, separators=(",", ":")),
                ),
            )
            return receipt

    def _bundle_for_resource(
        self, connection: sqlite3.Connection, resource: str
    ) -> sqlite3.Row | None:
        return connection.execute(
            """
            SELECT b.*
            FROM bundles AS b
            JOIN bundle_members AS m ON m.claim_id = b.claim_id
            WHERE m.resource = ?
            """,
            (resource,),
        ).fetchone()

    def _require_owner(
        self, connection: sqlite3.Connection, request: MutationRequest
    ) -> sqlite3.Row:
        bundle = self._bundle_for_resource(connection, request.resource)
        if bundle is not None:
            raise LeaseError(
                "bundle-operation-required",
                resource=request.resource,
                claim=self._bundle_claim(connection, bundle).to_dict(
                    include_token=False
                ),
            )
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
        *,
        checkpoint: str | None = None,
    ) -> dict[str, Any]:
        now = self.clock()
        revision = int(row["revision"]) + 1
        ttl = require_ttl(request.ttl)
        cursor = connection.execute(
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
            # Guarded-operation callers already hold the bearer token. Return
            # the advanced revision without copying that secret into command
            # output or the durable operation receipt.
            receipt["claim"] = self._claim(current).to_dict(include_token=False)
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

    def owner_claim(
        self, request: MutationRequest, *, lock_held: bool = False
    ) -> dict[str, Any]:
        """Read the matching ownership epoch without requiring its revision."""

        lock = (
            nullcontext() if lock_held else resource_lock(request.resource, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            row = self._require_owner(db, request)
            return self._claim(row).to_dict(include_token=False)

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

    @staticmethod
    def _bundle_row(
        connection: sqlite3.Connection, claim_id: str
    ) -> sqlite3.Row | None:
        return connection.execute(
            "SELECT * FROM bundles WHERE claim_id = ?", (claim_id,)
        ).fetchone()

    @staticmethod
    def _bundle_resources(
        connection: sqlite3.Connection, claim_id: str
    ) -> tuple[str, ...]:
        row = connection.execute(
            "SELECT resources FROM bundle_epochs WHERE claim_id = ?", (claim_id,)
        ).fetchone()
        if row is None:
            raise LeaseError("bundle-not-found", code=3, claimId=claim_id)
        return tuple(str(value) for value in json.loads(str(row["resources"])))

    def _bundle_claim(
        self,
        connection: sqlite3.Connection,
        row: sqlite3.Row,
    ) -> BundleClaim:
        return bundle_claim_from_row(
            row,
            self._bundle_resources(connection, str(row["claim_id"])),
            self.clock(),
        )

    @staticmethod
    def _bundle_operation_resource(resources: tuple[str, ...]) -> str:
        return json.dumps(list(resources), separators=(",", ":"))

    def _bundle_current(
        self, connection: sqlite3.Connection, resources: tuple[str, ...]
    ) -> sqlite3.Row | None:
        placeholders = ",".join("?" for _ in resources)
        rows = connection.execute(
            f"""
            SELECT m.resource, m.claim_id
            FROM bundle_members AS m
            WHERE m.resource IN ({placeholders})
            """,
            resources,
        ).fetchall()
        if not rows:
            return None
        if len(rows) != len(resources):
            raise LeaseError(
                "bundle-membership-mismatch",
                code=3,
                resource=",".join(resources),
            )
        claim_ids = {str(row["claim_id"]) for row in rows}
        if len(claim_ids) != 1:
            raise LeaseError(
                "bundle-membership-mismatch",
                code=3,
                resource=",".join(resources),
            )
        claim_id = next(iter(claim_ids))
        stored = self._bundle_resources(connection, claim_id)
        if stored != resources:
            raise LeaseError(
                "bundle-membership-mismatch",
                code=3,
                resource=",".join(resources),
            )
        return self._bundle_row(connection, claim_id)

    def _require_bundle_owner(
        self, connection: sqlite3.Connection, request: BundleMutationRequest
    ) -> sqlite3.Row:
        row = self._bundle_current(connection, request.resources)
        if row is None:
            raise LeaseError(
                "claim-not-found",
                resource=",".join(request.resources),
            )
        if row["claim_id"] != request.claim_id or row["token"] != request.token:
            raise LeaseError(
                "stale-claim",
                resource=",".join(request.resources),
                claim=self._bundle_claim(connection, row).to_dict(include_token=False),
            )
        return row

    def _require_bundle_current(
        self, connection: sqlite3.Connection, request: BundleMutationRequest
    ) -> sqlite3.Row:
        row = self._require_bundle_owner(connection, request)
        if int(row["revision"]) != request.revision:
            raise LeaseError(
                "stale-revision",
                resource=",".join(request.resources),
                expectedRevision=int(row["revision"]),
                suppliedRevision=request.revision,
            )
        if self.clock() >= float(row["expires_at"]):
            raise LeaseError(
                "claim-expired",
                resource=",".join(request.resources),
                claim=self._bundle_claim(connection, row).to_dict(include_token=False),
            )
        return row

    def _bundle_cached_operation(
        self,
        connection: sqlite3.Connection,
        request: BundleMutationRequest,
        kind: str,
        expected: dict[str, Any],
    ) -> dict[str, Any] | None:
        row = connection.execute(
            """
            SELECT * FROM operations
            WHERE resource = ? AND claim_id = ? AND operation_id = ?
            ORDER BY kind
            LIMIT 1
            """,
            (
                self._bundle_operation_resource(request.resources),
                request.claim_id,
                request.operation_id,
            ),
        ).fetchone()
        if row is None:
            return None
        recorded = json.loads(str(row["request"]))
        if str(row["kind"]) != kind or {
            key: value for key, value in recorded.items() if key != "revision"
        } != {key: value for key, value in expected.items() if key != "revision"}:
            raise LeaseError(
                "operation-id-request-mismatch",
                code=3,
                operationId=request.operation_id,
            )
        expected_revision = int(row["expected_revision"])
        if request.revision != expected_revision:
            raise LeaseError(
                "stale-revision",
                resource=",".join(request.resources),
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
        receipt["idempotent"] = True
        return receipt

    def _advance_bundle(
        self,
        connection: sqlite3.Connection,
        row: sqlite3.Row,
        request: BundleMutationRequest,
        kind: str,
        operation_request: dict[str, Any],
        receipt: dict[str, Any],
        *,
        complete_operation: bool = False,
    ) -> dict[str, Any]:
        now = self.clock()
        revision = int(row["revision"]) + 1
        ttl = require_ttl(request.ttl)
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
                "claim-update-conflict",
                code=3,
                resource=",".join(request.resources),
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
                    "claim-update-conflict",
                    code=3,
                    resource=",".join(request.resources),
                )
            connection.execute(
                """
                INSERT INTO resources(resource, revision) VALUES (?, ?)
                ON CONFLICT(resource) DO UPDATE SET revision = excluded.revision
                """,
                (resource, revision),
            )
        current = self._bundle_row(connection, request.claim_id)
        if current is None:
            raise LeaseError(
                "claim-update-conflict",
                code=3,
                resource=",".join(request.resources),
            )
        receipt["claim"] = self._bundle_claim(connection, current).to_dict(
            include_token=kind != "exec-bundle"
        )
        encoded_receipt = json.dumps(receipt, sort_keys=True, separators=(",", ":"))
        operation_resource = self._bundle_operation_resource(request.resources)
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
                    operation_resource,
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
                    operation_resource,
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

    def bundle_status(self, resources: tuple[str, ...]) -> dict[str, Any]:
        """Inspect an exact ordered bundle without exposing its token."""

        resources = require_bundle_resources(resources)
        with (
            resource_locks(resources, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            row = self._bundle_current(db, resources)
            if row is None:
                claims = db.execute(
                    f"""
                    SELECT resource FROM claims
                    WHERE resource IN ({",".join("?" for _ in resources)})
                    """,
                    resources,
                ).fetchall()
                if claims:
                    raise LeaseError(
                        "bundle-membership-mismatch",
                        code=3,
                        resource=",".join(resources),
                    )
                return {
                    "ok": True,
                    "operation": "status-bundle",
                    "resources": list(resources),
                    "state": "free",
                }
            claim = self._bundle_claim(db, row)
            return {
                "ok": True,
                "operation": "status-bundle",
                "resources": list(resources),
                "state": "active" if claim.active else "expired",
                "claim": claim.to_dict(include_token=False),
            }

    def heartbeat_bundle(
        self, request: BundleMutationRequest, *, lock_held: bool = False
    ) -> dict[str, Any]:
        """Renew every member and advance one shared bundle revision."""

        ttl = require_ttl(request.ttl)
        lock = (
            nullcontext() if lock_held else resource_locks(request.resources, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            row = self._require_bundle_owner(db, request)
            operation_request = request.request_dict()
            cached = self._bundle_cached_operation(
                db, request, "heartbeat-bundle", operation_request
            )
            if cached is not None:
                return cached
            self._require_bundle_current(db, request)
            receipt: dict[str, Any] = {
                "ok": True,
                "operation": "heartbeat-bundle",
                "operationId": request.operation_id,
                "idempotent": False,
                "resources": list(request.resources),
                "ttl": ttl,
            }
            result = self._advance_bundle(
                db,
                row,
                request,
                "heartbeat-bundle",
                operation_request,
                receipt,
            )
            return result

    def begin_bundle_operation(
        self,
        request: BundleMutationRequest,
        kind: str,
        operation_request: dict[str, Any],
        *,
        lock_held: bool = False,
    ) -> dict[str, Any] | None:
        """Record a guarded bundle operation before its side effect."""

        lock = (
            nullcontext() if lock_held else resource_locks(request.resources, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            self._require_bundle_owner(db, request)
            cached = self._bundle_cached_operation(db, request, kind, operation_request)
            if cached is not None:
                return cached
            self._require_bundle_current(db, request)
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
                    self._bundle_operation_resource(request.resources),
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

    def complete_bundle_operation(
        self,
        request: BundleMutationRequest,
        kind: str,
        operation_request: dict[str, Any],
        receipt: dict[str, Any],
        *,
        lock_held: bool = False,
    ) -> dict[str, Any]:
        """Complete one started guarded bundle operation atomically."""

        lock = (
            nullcontext() if lock_held else resource_locks(request.resources, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            row = self._require_bundle_owner(db, request)
            operation = db.execute(
                """
                SELECT * FROM operations
                WHERE resource = ? AND claim_id = ? AND operation_id = ?
                    AND kind = ?
                """,
                (
                    self._bundle_operation_resource(request.resources),
                    request.claim_id,
                    request.operation_id,
                    kind,
                ),
            ).fetchone()
            if operation is None:
                raise LeaseError(
                    "operation-not-found",
                    code=3,
                    operationId=request.operation_id,
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
            expected_revision = int(operation["expected_revision"])
            state = str(operation["state"])
            if state == "completed":
                if request.revision != expected_revision:
                    raise LeaseError(
                        "stale-revision",
                        resource=",".join(request.resources),
                        expectedRevision=expected_revision,
                        suppliedRevision=request.revision,
                    )
                completed = json.loads(str(operation["receipt"]))
                completed["idempotent"] = True
                return completed
            if state != "started":
                raise LeaseError(
                    "invalid-operation-state",
                    code=3,
                    operationId=request.operation_id,
                )
            self._require_bundle_current(db, request)
            return self._advance_bundle(
                db,
                row,
                request,
                kind,
                operation_request,
                receipt,
                complete_operation=True,
            )

    def _bundle_operation_request(
        self, request: BundleMutationRequest, **extra: Any
    ) -> dict[str, Any]:
        value = request.request_dict(**extra)
        value["tokenHash"] = hashlib.sha256(request.token.encode("utf-8")).hexdigest()
        return value

    def release_bundle(
        self, request: BundleMutationRequest, reason: str
    ) -> dict[str, Any]:
        """Release every member of a bundle in one transaction."""

        require_text(reason, "release-reason")
        resources = request.resources
        with (
            resource_locks(resources, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            operation_request = self._bundle_operation_request(request, reason=reason)
            cached = self._bundle_cached_operation(
                db, request, "release-bundle", operation_request
            )
            if cached is not None:
                return cached
            self._require_bundle_current(db, request)
            receipt: dict[str, Any] = {
                "ok": True,
                "operation": "release-bundle",
                "operationId": request.operation_id,
                "idempotent": False,
                "resources": list(resources),
                "releasedClaimId": request.claim_id,
                "releasedRevision": request.revision,
                "releasedAt": self._timestamp(self.clock()),
                "reason": reason,
            }
            db.execute(
                """
                INSERT INTO operations(
                    resource, claim_id, operation_id, kind, request,
                    expected_revision, receipt, created_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    self._bundle_operation_resource(resources),
                    request.claim_id,
                    request.operation_id,
                    "release-bundle",
                    json.dumps(
                        operation_request, sort_keys=True, separators=(",", ":")
                    ),
                    request.revision,
                    json.dumps(receipt, sort_keys=True, separators=(",", ":")),
                    self.clock(),
                ),
            )
            deleted = db.execute(
                f"""
                DELETE FROM claims
                WHERE claim_id = ? AND token = ? AND revision = ?
                    AND resource IN ({",".join("?" for _ in resources)})
                """,
                (
                    request.claim_id,
                    request.token,
                    request.revision,
                    *resources,
                ),
            )
            if deleted.rowcount != len(resources):
                raise LeaseError(
                    "claim-release-conflict",
                    code=3,
                    resource=",".join(resources),
                )
            db.execute(
                "DELETE FROM bundle_members WHERE claim_id = ?", (request.claim_id,)
            )
            db.execute("DELETE FROM bundles WHERE claim_id = ?", (request.claim_id,))
            return receipt

    def acquire_bundle(self, request: BundleAcquireRequest) -> dict[str, Any]:
        """Acquire one shared ownership epoch for all bundle resources."""

        resources = require_bundle_resources(request.resources)
        ttl = require_ttl(request.ttl)
        with (
            resource_locks(resources, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            now = self.clock()
            bundle = self._bundle_row(db, request.claim_id)
            if bundle is not None:
                recorded_resources = self._bundle_resources(db, request.claim_id)
                recorded = (
                    recorded_resources,
                    str(bundle["agent_id"]),
                    str(bundle["session_id"]),
                    str(bundle["owner_id"]),
                    str(bundle["work_key"]),
                    bool(bundle["coordination_only"]),
                )
                if recorded != request.identity:
                    raise LeaseError(
                        "claim-id-identity-mismatch",
                        resource=",".join(resources),
                    )
                if float(bundle["acquire_ttl"]) != ttl:
                    raise LeaseError(
                        "claim-id-request-mismatch",
                        code=3,
                        resource=",".join(resources),
                        claimId=request.claim_id,
                    )
                if now >= float(bundle["expires_at"]):
                    raise LeaseError(
                        "claim-expired",
                        resource=",".join(resources),
                        claim=self._bundle_claim(db, bundle).to_dict(
                            include_token=False
                        ),
                    )
                return {
                    "ok": True,
                    "operation": "acquire-bundle",
                    "idempotent": True,
                    "resources": list(resources),
                    "claim": self._bundle_claim(db, bundle).to_dict(),
                }

            rows = db.execute(
                f"SELECT * FROM claims WHERE resource IN ({','.join('?' for _ in resources)})",
                resources,
            ).fetchall()
            for row in rows:
                if now < float(row["expires_at"]):
                    old_bundle = self._bundle_row(db, str(row["claim_id"]))
                    conflict = (
                        self._bundle_claim(db, old_bundle).to_dict(include_token=False)
                        if old_bundle is not None
                        else self._claim(row).to_dict(include_token=False)
                    )
                    raise LeaseError(
                        "already-claimed",
                        resource=str(row["resource"]),
                        claim=conflict,
                    )

            for old_bundle_id in {
                str(row["claim_id"])
                for row in db.execute(
                    f"SELECT claim_id FROM bundle_members WHERE resource IN ({','.join('?' for _ in resources)})",
                    resources,
                ).fetchall()
            }:
                old_bundle = self._bundle_row(db, old_bundle_id)
                if old_bundle is not None and now >= float(old_bundle["expires_at"]):
                    db.execute(
                        "DELETE FROM bundle_members WHERE claim_id = ?",
                        (old_bundle_id,),
                    )
                    db.execute(
                        "DELETE FROM bundles WHERE claim_id = ?", (old_bundle_id,)
                    )

            epoch = db.execute(
                """
                SELECT claim_id FROM epochs WHERE claim_id = ?
                UNION ALL
                SELECT claim_id FROM bundle_epochs WHERE claim_id = ?
                LIMIT 1
                """,
                (request.claim_id, request.claim_id),
            ).fetchone()
            if epoch is not None:
                raise LeaseError(
                    "claim-id-reused",
                    resource=",".join(resources),
                    claimId=request.claim_id,
                )

            revision_row = db.execute(
                f"""
                SELECT MAX(revision) AS revision FROM (
                    SELECT revision FROM resources
                    WHERE resource IN ({",".join("?" for _ in resources)})
                    UNION ALL
                    SELECT revision FROM claims
                    WHERE resource IN ({",".join("?" for _ in resources)})
                )
                """,
                (*resources, *resources),
            ).fetchone()
            revision = int(revision_row["revision"] or 0) + 1
            token = self.token_factory()
            db.execute(
                """
                INSERT INTO bundle_epochs(
                    claim_id, resources, agent_id, session_id, owner_id,
                    work_key, acquired_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    request.claim_id,
                    json.dumps(list(resources), separators=(",", ":")),
                    request.agent_id,
                    request.session_id,
                    request.owner_id,
                    request.work_key,
                    now,
                ),
            )
            db.execute(
                """
                INSERT INTO bundles(
                    claim_id, token, revision, agent_id, session_id, owner_id,
                    work_key, coordination_only, acquired_at, acquire_ttl,
                    heartbeat_at, expires_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
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
            for resource in resources:
                db.execute(
                    """
                    INSERT INTO bundle_members(resource, claim_id)
                    VALUES (?, ?)
                    """,
                    (resource, request.claim_id),
                )
                db.execute(
                    """
                    INSERT INTO resources(resource, revision) VALUES (?, ?)
                    ON CONFLICT(resource) DO UPDATE SET revision = excluded.revision
                    """,
                    (resource, revision),
                )
                db.execute(
                    """
                    INSERT INTO claims(
                        resource, claim_id, token, revision, agent_id, session_id,
                        owner_id, work_key, coordination_only, acquired_at,
                        acquire_ttl, heartbeat_at, expires_at, checkpoint
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)
                    ON CONFLICT(resource) DO UPDATE SET
                        claim_id=excluded.claim_id, token=excluded.token,
                        revision=excluded.revision, agent_id=excluded.agent_id,
                        session_id=excluded.session_id, owner_id=excluded.owner_id,
                        work_key=excluded.work_key,
                        coordination_only=excluded.coordination_only,
                        acquired_at=excluded.acquired_at,
                        acquire_ttl=excluded.acquire_ttl,
                        heartbeat_at=excluded.heartbeat_at,
                        expires_at=excluded.expires_at,
                        checkpoint=NULL
                    """,
                    (
                        resource,
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
            created = self._bundle_row(db, request.claim_id)
            if created is None:
                raise LeaseError("bundle-create-conflict", code=3)
            return {
                "ok": True,
                "operation": "acquire-bundle",
                "idempotent": False,
                "reclaimed": bool(rows),
                "resources": list(resources),
                "claim": self._bundle_claim(db, created).to_dict(),
            }

    def acquire(self, request: AcquireRequest) -> dict[str, Any]:
        """Acquire or idempotently retry one ownership epoch."""

        require_resource(request.resource)
        ttl = require_ttl(request.ttl)
        with (
            resource_lock(request.resource, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            now = self.clock()
            row = self._current(db, request.resource)
            bundle = self._bundle_for_resource(db, request.resource)
            if bundle is not None:
                raise LeaseError(
                    "bundle-operation-required",
                    resource=request.resource,
                    claim=self._bundle_claim(db, bundle).to_dict(include_token=False),
                )
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
                if float(row["acquire_ttl"]) != ttl:
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
            prior_checkpoint = (
                str(row["checkpoint"])
                if row is not None and row["checkpoint"] is not None
                else None
            )
            recovery = "expired-recovery" if row is not None else None
            if row is None:
                prior_release = db.execute(
                    """
                    SELECT checkpoint FROM releases
                    WHERE resource = ?
                    ORDER BY released_at DESC
                    LIMIT 1
                    """,
                    (request.resource,),
                ).fetchone()
                if prior_release is not None:
                    prior_checkpoint = (
                        str(prior_release["checkpoint"])
                        if prior_release["checkpoint"] is not None
                        else None
                    )
                    recovery = "clean-handoff"
            epoch = db.execute(
                """
                SELECT resource, acquired_at FROM epochs WHERE claim_id = ?
                UNION ALL
                SELECT resources AS resource, acquired_at
                FROM bundle_epochs
                WHERE claim_id = ?
                LIMIT 1
                """,
                (request.claim_id, request.claim_id),
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
                    INSERT INTO resources(resource, revision) VALUES (?, ?)
                    ON CONFLICT(resource) DO UPDATE SET revision = excluded.revision
                    """,
                (request.resource, revision),
            )
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
                    INSERT INTO claims(
                        resource, claim_id, token, revision, agent_id, session_id,
                        owner_id, work_key, coordination_only, acquired_at,
                        acquire_ttl, heartbeat_at, expires_at, checkpoint
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    ON CONFLICT(resource) DO UPDATE SET
                        claim_id=excluded.claim_id, token=excluded.token,
                        revision=excluded.revision, agent_id=excluded.agent_id,
                        session_id=excluded.session_id, owner_id=excluded.owner_id,
                        work_key=excluded.work_key,
                        coordination_only=excluded.coordination_only,
                        acquired_at=excluded.acquired_at,
                        acquire_ttl=excluded.acquire_ttl,
                        heartbeat_at=excluded.heartbeat_at,
                        expires_at=excluded.expires_at,
                        checkpoint=excluded.checkpoint
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
                    prior_checkpoint,
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
                "recovery": recovery,
                "checkpoint": deserialize_checkpoint(prior_checkpoint),
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

    def checkpoint(
        self,
        request: MutationRequest,
        value: Any,
        *,
        lock_held: bool = False,
    ) -> dict[str, Any]:
        """Persist a bounded JSON checkpoint while renewing ownership.

        ``value`` is canonicalized as JSON and must fit within the
        ``MAX_CHECKPOINT_BYTES`` UTF-8 limit. The write, revision increment,
        and lease renewal commit atomically. The latest value remains on the
        active claim and is copied into release history for clean handoff or
        expiry recovery; it is coordination metadata, not provider progress.

        """
        serialized = serialize_checkpoint(value)
        operation_request = self._receipt_request(
            request, checkpoint=json.loads(serialized)
        )
        lock = (
            nullcontext() if lock_held else resource_lock(request.resource, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            row = self._require_owner(db, request)
            cached = self._cached_operation(
                db, request, "checkpoint", operation_request
            )
            if cached is not None:
                return cached
            self._require_current(db, request)
            receipt: dict[str, Any] = {
                "ok": True,
                "operation": "checkpoint",
                "operationId": request.operation_id,
                "idempotent": False,
                "checkpoint": json.loads(serialized),
                "checkpointBytes": len(serialized.encode("utf-8")),
            }
            return self._advance_claim(
                db,
                row,
                request,
                "checkpoint",
                operation_request,
                receipt,
                checkpoint=serialized,
            )

    def transfer(self, request: TransferRequest) -> dict[str, Any]:
        """Atomically replace one active owner with a successor epoch."""

        ttl = require_ttl(request.ttl)
        operation_request = request.request_dict(
            tokenSha256=hashlib.sha256(request.token.encode("utf-8")).hexdigest()
        )
        with (
            resource_lock(request.resource, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            if self._bundle_for_resource(db, request.resource) is not None:
                raise LeaseError("bundle-operation-required", resource=request.resource)
            prior = db.execute(
                """
                SELECT * FROM operations
                WHERE resource = ? AND claim_id = ? AND operation_id = ?
                """,
                (request.resource, request.claim_id, request.operation_id),
            ).fetchone()
            if prior is not None and str(prior["kind"]) != "transfer":
                raise LeaseError(
                    "operation-id-request-mismatch",
                    code=3,
                    operationId=request.operation_id,
                )
            if prior is not None:
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
                if int(prior["expected_revision"]) != request.revision:
                    raise LeaseError(
                        "stale-revision",
                        resource=request.resource,
                        expectedRevision=int(prior["expected_revision"]),
                        suppliedRevision=request.revision,
                    )
                receipt = json.loads(str(prior["receipt"]))
                receipt["idempotent"] = True
                return receipt
            row = self._current(db, request.resource)
            if row is None:
                raise LeaseError("claim-not-found", resource=request.resource)
            if row["claim_id"] != request.claim_id or row["token"] != request.token:
                raise LeaseError(
                    "stale-claim",
                    resource=request.resource,
                    claim=self._claim(row).to_dict(include_token=False),
                )
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
            successor_epoch = db.execute(
                """
                SELECT claim_id FROM epochs WHERE claim_id = ?
                UNION ALL
                SELECT claim_id FROM bundle_epochs WHERE claim_id = ?
                LIMIT 1
                """,
                (request.successor_claim_id, request.successor_claim_id),
            ).fetchone()
            if successor_epoch is not None:
                raise LeaseError(
                    "claim-id-reused",
                    resource=request.resource,
                    claimId=request.successor_claim_id,
                )
            prior_resource = db.execute(
                "SELECT revision FROM resources WHERE resource = ?",
                (request.resource,),
            ).fetchone()
            revision = (
                max(
                    int(row["revision"]),
                    int(prior_resource["revision"]) if prior_resource else 0,
                )
                + 1
            )
            token = self.token_factory()
            while token == request.token:
                token = self.token_factory()
            now = self.clock()
            db.execute(
                """
                INSERT INTO epochs(
                    claim_id, resource, agent_id, session_id, owner_id,
                    work_key, acquired_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    request.successor_claim_id,
                    request.resource,
                    request.successor_agent_id,
                    request.successor_session_id,
                    request.successor_owner_id,
                    request.successor_work_key,
                    now,
                ),
            )
            cursor = db.execute(
                """
                UPDATE claims
                SET claim_id = ?, token = ?, revision = ?, agent_id = ?,
                    session_id = ?, owner_id = ?, work_key = ?,
                    acquired_at = ?, acquire_ttl = ?, heartbeat_at = ?,
                    expires_at = ?
                WHERE resource = ? AND claim_id = ? AND token = ? AND revision = ?
                """,
                (
                    request.successor_claim_id,
                    token,
                    revision,
                    request.successor_agent_id,
                    request.successor_session_id,
                    request.successor_owner_id,
                    request.successor_work_key,
                    now,
                    ttl,
                    now,
                    now + ttl,
                    request.resource,
                    request.claim_id,
                    request.token,
                    request.revision,
                ),
            )
            if cursor.rowcount != 1:
                raise LeaseError("claim-update-conflict", code=3)
            db.execute(
                """
                INSERT INTO resources(resource, revision) VALUES (?, ?)
                ON CONFLICT(resource) DO UPDATE SET revision = excluded.revision
                """,
                (request.resource, revision),
            )
            successor = self._current(db, request.resource)
            if successor is None:
                raise LeaseError("claim-update-conflict", code=3)
            receipt: dict[str, Any] = {
                "ok": True,
                "operation": "transfer",
                "operationId": request.operation_id,
                "idempotent": False,
                "previousClaimId": request.claim_id,
                "previousRevision": request.revision,
                "claim": self._claim(successor).to_dict(),
            }
            db.execute(
                """
                INSERT INTO operations(
                    resource, claim_id, operation_id, kind, request,
                    expected_revision, receipt, created_at
                ) VALUES (?, ?, ?, 'transfer', ?, ?, ?, ?)
                """,
                (
                    request.resource,
                    request.claim_id,
                    request.operation_id,
                    json.dumps(
                        operation_request, sort_keys=True, separators=(",", ":")
                    ),
                    request.revision,
                    json.dumps(receipt, sort_keys=True, separators=(",", ":")),
                    now,
                ),
            )
            return receipt

    @staticmethod
    def _replay_release(
        prior: sqlite3.Row | None,
        request: MutationRequest,
        operation_request: dict[str, Any],
    ) -> dict[str, Any] | None:
        if prior is None:
            return None
        if str(prior["token"]) != request.token:
            raise LeaseError("stale-claim", resource=request.resource)
        recorded = json.loads(str(prior["request"]))
        if {key: value for key, value in recorded.items() if key != "revision"} != {
            key: value for key, value in operation_request.items() if key != "revision"
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

    def release(self, request: MutationRequest, reason: str) -> dict[str, Any]:
        """Release one active ownership epoch after a durable checkpoint."""

        require_text(reason, "release-reason")
        with (
            resource_lock(request.resource, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            bundle = self._bundle_for_resource(db, request.resource)
            if bundle is not None:
                raise LeaseError(
                    "bundle-operation-required",
                    resource=request.resource,
                    claim=self._bundle_claim(db, bundle).to_dict(include_token=False),
                )
            operation_request = self._receipt_request(
                request, operationId=request.operation_id, reason=reason
            )
            prior = db.execute(
                "SELECT * FROM releases WHERE resource = ? AND claim_id = ?",
                (request.resource, request.claim_id),
            ).fetchone()
            replayed = self._replay_release(prior, request, operation_request)
            if replayed is not None:
                return replayed
            row = self._current(db, request.resource)
            if row is None:
                raise LeaseError("claim-not-found", resource=request.resource)
            if row["claim_id"] != request.claim_id or row["token"] != request.token:
                raise LeaseError(
                    "stale-claim",
                    resource=request.resource,
                    claim=self._claim(row).to_dict(include_token=False),
                )
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
                "checkpoint": deserialize_checkpoint(
                    row["checkpoint"] if row["checkpoint"] is not None else None
                ),
            }
            db.execute(
                """
                    INSERT INTO releases(
                        resource, claim_id, token, revision, operation_id,
                        request, released_at, receipt, checkpoint
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
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
                    row["checkpoint"],
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
            bundle = self._bundle_for_resource(db, resource)
            if row is None:
                return {
                    "ok": True,
                    "operation": "status",
                    "resource": resource,
                    "state": "free",
                }
            if bundle is not None:
                claim = self._bundle_claim(db, bundle)
                claim_dict = claim.to_dict(include_token=False)
            else:
                claim = self._claim(row)
                claim_dict = claim.to_dict(include_token=False)
        return {
            "ok": True,
            "operation": "status",
            "resource": resource,
            "state": "active" if claim.active else "expired",
            "claim": claim_dict,
        }

    def status_verbose(self, resource: str) -> dict[str, Any]:
        """Read a redacted diagnostic projection without mutating state."""

        require_resource(resource)
        database = self.home / "leases.sqlite3"
        state_files = (
            database,
            database.with_name(f"{database.name}-wal"),
            database.with_name(f"{database.name}-shm"),
        )
        if any(path.is_symlink() for path in state_files):
            raise LeaseError("state-file-is-symlink", code=64)
        if not database.is_file():
            return {
                "schemaVersion": 1,
                "ok": True,
                "operation": "status-verbose",
                "resource": resource,
                "state": "free",
                "claim": None,
                "unknownOperations": [],
                "release": None,
            }
        with closing(connect_readonly(self.home)) as db:
            now = self.clock()
            tables = {
                str(table["name"])
                for table in db.execute(
                    "SELECT name FROM sqlite_master WHERE type = 'table'"
                )
            }
            row = self._current(db, resource) if "claims" in tables else None
            claim: dict[str, Any] | None = None
            if row is not None:
                row_columns = set(row.keys())
                claim = {
                    "resource": resource,
                    "claimId": str(row["claim_id"]),
                    "agentId": str(row["agent_id"]),
                    "sessionId": str(row["session_id"]),
                    "ownerId": str(row["owner_id"]),
                    "workKey": str(row["work_key"]),
                    "coordinationOnly": (
                        bool(row["coordination_only"])
                        if "coordination_only" in row_columns
                        else False
                    ),
                    "revision": int(row["revision"]),
                    "acquiredAt": self._timestamp(float(row["acquired_at"])),
                    "heartbeatAt": self._timestamp(float(row["heartbeat_at"])),
                    "expiresAt": self._timestamp(float(row["expires_at"])),
                }
                state = "active" if now < float(row["expires_at"]) else "expired"
            else:
                state = "free"

            if "operations" in tables and "reconciliations" in tables:
                operation_rows = db.execute(
                    """
                    SELECT
                        o.operation_id,
                        o.kind,
                        o.expected_revision,
                        o.created_at,
                        o.request,
                        r.request_sha256,
                        r.kind AS reconciliation_kind
                    FROM operations AS o
                    LEFT JOIN reconciliations AS r
                      ON r.resource = o.resource AND r.operation_id = o.operation_id
                    WHERE o.resource = ? AND o.state = 'started'
                    ORDER BY o.created_at, o.operation_id, o.claim_id, o.kind
                    """,
                    (resource,),
                ).fetchall()
            elif "operations" in tables:
                operation_rows = db.execute(
                    """
                    SELECT operation_id, kind, expected_revision, created_at,
                           request, NULL AS request_sha256,
                           NULL AS reconciliation_kind
                    FROM operations
                    WHERE resource = ? AND state = 'started'
                    ORDER BY created_at, operation_id, claim_id, kind
                    """,
                    (resource,),
                ).fetchall()
            else:
                operation_rows = []
            unknown_operations = []
            for operation in operation_rows:
                reconciliation_sha256 = operation["request_sha256"]
                reconciliation_kind = operation["reconciliation_kind"]
                if reconciliation_sha256 is not None and str(
                    reconciliation_kind
                ) == str(operation["kind"]):
                    operation_sha256 = hashlib.sha256(
                        str(operation["request"]).encode("utf-8")
                    ).hexdigest()
                    if operation_sha256 == str(reconciliation_sha256):
                        continue
                unknown_operations.append(
                    {
                        "operationId": str(operation["operation_id"]),
                        "kind": str(operation["kind"]),
                        "expectedRevision": int(operation["expected_revision"]),
                        "createdAt": self._timestamp(float(operation["created_at"])),
                    }
                )

            if "releases" in tables:
                release_row = db.execute(
                    """
                    SELECT claim_id, operation_id, revision, released_at
                    FROM releases
                    WHERE resource = ?
                    ORDER BY released_at DESC, operation_id DESC, claim_id DESC
                    LIMIT 1
                    """,
                    (resource,),
                ).fetchone()
            else:
                release_row = None
            release = (
                {
                    "claimId": str(release_row["claim_id"]),
                    "operationId": str(release_row["operation_id"]),
                    "revision": int(release_row["revision"]),
                    "releasedAt": self._timestamp(float(release_row["released_at"])),
                }
                if release_row is not None
                else None
            )

        projection: dict[str, Any] = {
            "schemaVersion": 1,
            "ok": True,
            "operation": "status-verbose",
            "resource": resource,
            "state": state,
            "claim": claim,
            "unknownOperations": unknown_operations,
            "release": release,
        }
        if unknown_operations:
            projection["guidance"] = (
                "Unknown outcomes are non-mutating diagnostics; do not replay "
                "without authoritative evidence."
            )
        return projection

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
            claims: list[dict[str, Any]] = []
            seen_bundles: set[str] = set()
            for row in rows:
                bundle = self._bundle_row(db, str(row["claim_id"]))
                if bundle is not None:
                    claim_id = str(bundle["claim_id"])
                    if claim_id in seen_bundles:
                        continue
                    seen_bundles.add(claim_id)
                    claims.append(
                        self._bundle_claim(db, bundle).to_dict(include_token=False)
                    )
                else:
                    claims.append(self._claim(row).to_dict(include_token=False))
        return {
            "ok": True,
            "operation": "list",
            "claims": claims,
        }

    @staticmethod
    def _timestamp(value: float) -> str:
        from .models import iso8601

        return iso8601(value)
