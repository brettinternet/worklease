"""Authorized reconciliation of durable unknown-outcome operations."""

from __future__ import annotations

import hashlib
import json
import sqlite3
from contextlib import closing
from typing import Any

from .locking import resource_locks
from .models import (
    BundleMutationRequest,
    LeaseError,
    MutationRequest,
    lease_is_active,
    require_text,
    require_ttl,
    serialize_checkpoint,
)
from .sqlite import transaction

_ReconciliationRequest = MutationRequest | BundleMutationRequest


class ReconciliationMixin:
    """Record observed outcomes with one implementation for both claim shapes."""

    @staticmethod
    def _reconciliation_evidence(
        request_operation_id: str,
        target_operation_id: str,
        expected_request_sha256: str,
        outcome: str,
        evidence: Any,
    ) -> str:
        require_text(target_operation_id, "target-operation-id")
        require_text(expected_request_sha256, "expected-request-sha256")
        if request_operation_id == target_operation_id:
            raise LeaseError(
                "operation-id-conflict", code=64, operationId=request_operation_id
            )
        if len(expected_request_sha256) != 64 or any(
            value not in "0123456789abcdefABCDEF" for value in expected_request_sha256
        ):
            raise LeaseError("invalid-request-sha256", code=64)
        if outcome not in {"observed-success", "observed-failure"}:
            raise LeaseError("invalid-reconciliation-outcome", code=64, outcome=outcome)
        try:
            return serialize_checkpoint(evidence)
        except LeaseError as error:
            raise LeaseError(
                "invalid-evidence", code=error.code, **error.details
            ) from error

    @staticmethod
    def _validate_reconciliation_replay(
        receipt: dict[str, Any], request: _ReconciliationRequest
    ) -> None:
        resolver_revision = receipt.get("resolverRevision")
        if resolver_revision is None:
            claim = receipt.get("claim")
            claim_revision = claim.get("revision") if isinstance(claim, dict) else None
            if isinstance(claim_revision, int) and not isinstance(claim_revision, bool):
                resolver_revision = claim_revision - 1

        ttl = require_ttl(request.ttl)
        ttl_mismatch = "ttl" in receipt and receipt["ttl"] != ttl
        if resolver_revision != request.revision or ttl_mismatch:
            raise LeaseError(
                "operation-id-request-mismatch",
                code=3,
                operationId=request.operation_id,
            )

    @staticmethod
    def _reconciliation_lock(request: _ReconciliationRequest, home: Any) -> Any:
        if isinstance(request, BundleMutationRequest):
            return resource_locks(request.resources, home)
        from .locking import resource_lock

        return resource_lock(request.resource, home)

    def _reconcile_operation(
        self: Any,
        request: _ReconciliationRequest,
        target_operation_id: str,
        expected_request_sha256: str,
        outcome: str,
        evidence: Any,
    ) -> dict[str, Any]:
        evidence_json = self._reconciliation_evidence(
            request.operation_id,
            target_operation_id,
            expected_request_sha256,
            outcome,
            evidence,
        )
        operation_resource = request.resource
        resource_label = (
            ",".join(request.resources)
            if isinstance(request, BundleMutationRequest)
            else request.resource
        )
        kind = (
            "reconcile-operation-bundle"
            if isinstance(request, BundleMutationRequest)
            else "reconcile-operation"
        )
        with (
            self._reconciliation_lock(request, self.home),
            closing(self._connect()) as db,
            transaction(db),
        ):
            owner = self._require_claim_owner(db, request)
            if not lease_is_active(owner, self.clock()):
                raise LeaseError(
                    "claim-expired",
                    resource=resource_label,
                    claim=self._operation_claim_dict(db, request, owner),
                )
            replay = db.execute(
                """
                SELECT * FROM reconciliations
                WHERE resource = ? AND reconciliation_operation_id = ?
                """,
                (operation_resource, request.operation_id),
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
                self._validate_reconciliation_replay(receipt, request)
                claim = receipt.get("claim")
                if isinstance(claim, dict) and int(owner["revision"]) > int(
                    claim.get("revision", owner["revision"])
                ):
                    raise LeaseError(
                        "stale-revision",
                        resource=resource_label,
                        expectedRevision=int(owner["revision"]),
                        suppliedRevision=request.revision,
                    )
                receipt["idempotent"] = True
                return receipt
            self._require_claim_current(db, request)
            target_rows = db.execute(
                """
                SELECT * FROM operations
                WHERE resource = ? AND operation_id = ?
                ORDER BY claim_id, kind
                """,
                (operation_resource, target_operation_id),
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
                    **(
                        {"resources": list(request.resources)}
                        if isinstance(request, BundleMutationRequest)
                        else {"resource": request.resource}
                    ),
                    operationId=target_operation_id,
                )
            target = target_rows[0]
            if str(target["state"]) != "started":
                raise LeaseError(
                    "operation-not-unknown",
                    code=3,
                    operationId=target_operation_id,
                    state=str(target["state"]),
                )
            actual_sha256 = hashlib.sha256(
                str(target["request"]).encode("utf-8")
            ).hexdigest()
            if actual_sha256 != expected_request_sha256.lower():
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
                (operation_resource, target_operation_id),
            ).fetchone()
            if existing_target is not None:
                raise LeaseError(
                    "operation-already-reconciled",
                    code=3,
                    operationId=target_operation_id,
                )

            now = self.clock()
            receipt: dict[str, Any] = {
                "ok": True,
                "operation": kind,
                "operationId": request.operation_id,
                "targetOperationId": target_operation_id,
                "state": "reconciled",
                "outcome": outcome,
                "requestSha256": expected_request_sha256,
                "reconciledAt": self._timestamp(now),
                "resolverRevision": request.revision,
                "ttl": require_ttl(request.ttl),
                "idempotent": False,
            }
            if isinstance(request, BundleMutationRequest):
                receipt["resources"] = list(request.resources)
            else:
                receipt["resource"] = request.resource
            self._advance_operation(
                db,
                owner,
                request,
                kind,
                {},
                receipt,
                record_operation=False,
                include_token=False,
            )
            current = self._operation_current_row(db, request)
            if current is None:
                raise LeaseError(
                    "claim-update-conflict", code=3, resource=resource_label
                )
            if isinstance(request, BundleMutationRequest):
                current_claim = self._bundle_claim(db, current)
            else:
                current_claim = self._claim(current)
            receipt["claim"] = current_claim.to_dict(include_token=False)
            db.execute(
                """
                INSERT INTO reconciliations(
                    resource, operation_id, kind, claim_id, target_claim_id,
                    outcome, evidence, resolver_agent_id, resolver_session_id,
                    resolver_owner_id, resolver_work_key, request_sha256,
                    reconciliation_operation_id, reconciled_at, receipt
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    operation_resource,
                    target_operation_id,
                    str(target["kind"]),
                    str(request.claim_id),
                    str(target["claim_id"]),
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

    def _operation_claim_dict(
        self: Any,
        connection: sqlite3.Connection,
        request: _ReconciliationRequest,
        row: sqlite3.Row,
    ) -> dict[str, Any]:
        if isinstance(request, BundleMutationRequest):
            return self._bundle_claim(connection, row).to_dict(include_token=False)
        return self._claim(row).to_dict(include_token=False)

    def _operation_current_row(
        self: Any,
        connection: sqlite3.Connection,
        request: _ReconciliationRequest,
    ) -> sqlite3.Row | None:
        if isinstance(request, BundleMutationRequest):
            return self._bundle_row(connection, request.claim_id)
        return self._current(connection, request.resource)

    def reconcile_operation(
        self: Any,
        request: MutationRequest,
        target_operation_id: str,
        expected_request_sha256: str,
        outcome: str,
        evidence: Any,
    ) -> dict[str, Any]:
        """Record an observed result for one started singleton operation."""

        return self._reconcile_operation(
            request,
            target_operation_id,
            expected_request_sha256,
            outcome,
            evidence,
        )

    def reconcile_bundle_operation(
        self: Any,
        request: BundleMutationRequest,
        target_operation_id: str,
        expected_request_sha256: str,
        outcome: str,
        evidence: Any,
    ) -> dict[str, Any]:
        """Record an observed result for one started bundle operation."""

        return self._reconcile_operation(
            request,
            target_operation_id,
            expected_request_sha256,
            outcome,
            evidence,
        )


__all__ = ["ReconciliationMixin"]
