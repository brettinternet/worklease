"""Ownership transitions that are not part of acquisition."""

from __future__ import annotations

import hashlib
import hmac
import json
import sqlite3
from contextlib import closing
from typing import Any

from .locking import resource_lock, resource_locks
from .models import (
    BundleMutationRequest,
    LeaseError,
    MutationRequest,
    TransferRequest,
    deserialize_checkpoint,
    lease_is_active,
    require_text,
    require_ttl,
)
from .sqlite import transaction


class LifecycleMixin:
    """Implement transfer and release transitions for claim epochs."""

    def _bundle_operation_request(
        self: Any, request: BundleMutationRequest, **extra: Any
    ) -> dict[str, Any]:
        value = request.request_dict(**extra)
        value["tokenHash"] = hashlib.sha256(request.token.encode("utf-8")).hexdigest()
        return value

    def release_bundle(
        self: Any, request: BundleMutationRequest, reason: str
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
            cached = self._cached_operation(
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
                    request.resource,
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

    def transfer(self: Any, request: TransferRequest) -> dict[str, Any]:
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
                state = str(prior["state"])
                if state == "started":
                    raise LeaseError(
                        "unknown-outcome",
                        code=3,
                        operationId=request.operation_id,
                        operation="transfer",
                    )
                if state != "completed":
                    raise LeaseError(
                        "invalid-operation-state",
                        code=3,
                        operationId=request.operation_id,
                    )
                receipt = json.loads(str(prior["receipt"]))
                claim = receipt.get("claim")
                current = self._current(db, request.resource)
                if isinstance(claim, dict) and current is not None:
                    claim["token"] = str(current["token"])
                receipt["idempotent"] = True
                return receipt
            row = self._current(db, request.resource)
            if row is None:
                raise LeaseError("claim-not-found", resource=request.resource)
            if row["claim_id"] != request.claim_id or not hmac.compare_digest(
                str(row["token"]), request.token
            ):
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
            if not lease_is_active(row, self.clock()):
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
                UNION ALL
                SELECT claim_id FROM claims WHERE claim_id = ?
                UNION ALL
                SELECT claim_id FROM bundles WHERE claim_id = ?
                LIMIT 1
                """,
                (
                    request.successor_claim_id,
                    request.successor_claim_id,
                    request.successor_claim_id,
                    request.successor_claim_id,
                ),
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
            persisted_receipt = dict(receipt)
            persisted_receipt["claim"] = self._claim(successor).to_dict(
                include_token=False
            )
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
                    json.dumps(
                        persisted_receipt,
                        sort_keys=True,
                        separators=(",", ":"),
                    ),
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
        if not hmac.compare_digest(str(prior["token"]), request.token):
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

    def release(self: Any, request: MutationRequest, reason: str) -> dict[str, Any]:
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
            if row["claim_id"] != request.claim_id or not hmac.compare_digest(
                str(row["token"]), request.token
            ):
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
            if not lease_is_active(row, self.clock()):
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


__all__ = ["LifecycleMixin"]
