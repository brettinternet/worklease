"""Read-only lease and diagnostic projections."""

from __future__ import annotations

import hashlib
import json
import sqlite3
from contextlib import closing
from typing import Any

from .locking import resource_locks
from .models import (
    LeaseError,
    lease_is_active,
    require_bundle_resources,
    require_resource,
)
from .sqlite import connect_readonly, transaction


class ProjectionMixin:
    """Expose stable redacted singleton, bundle, and inventory projections."""

    def bundle_status(self: Any, resources: tuple[str, ...]) -> dict[str, Any]:
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

    def status(self: Any, resource: str) -> dict[str, Any]:
        """Read one claim without exposing its bearer token."""

        require_resource(resource)
        with closing(self._connect()) as db, transaction(db, immediate=False):
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

    def status_verbose(self: Any, resource: str) -> dict[str, Any]:
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
            bundle = (
                self._bundle_for_resource(db, resource)
                if {"bundles", "bundle_members"}.issubset(tables)
                else None
            )
            claim: dict[str, Any] | None = None
            if row is not None:
                claim_row = bundle if bundle is not None else row
                row_columns = set(claim_row.keys())
                claim = {
                    ("resources" if bundle is not None else "resource"): (
                        list(self._bundle_resources(db, str(bundle["claim_id"])))
                        if bundle is not None
                        else resource
                    ),
                    "claimId": str(claim_row["claim_id"]),
                    "agentId": str(claim_row["agent_id"]),
                    "sessionId": str(claim_row["session_id"]),
                    "ownerId": str(claim_row["owner_id"]),
                    "workKey": str(claim_row["work_key"]),
                    "coordinationOnly": (
                        bool(claim_row["coordination_only"])
                        if "coordination_only" in row_columns
                        else False
                    ),
                    "revision": int(claim_row["revision"]),
                    "acquiredAt": self._timestamp(float(claim_row["acquired_at"])),
                    "heartbeatAt": self._timestamp(float(claim_row["heartbeat_at"])),
                    "expiresAt": self._timestamp(float(claim_row["expires_at"])),
                }
                state = "active" if lease_is_active(claim_row, now) else "expired"
            else:
                state = "free"

            if "bundle_epochs" in tables:
                operation_filter = """
                    (
                        o.resource = ?
                        OR EXISTS (
                            SELECT 1
                            FROM bundle_epochs AS be
                            JOIN json_each(be.resources) AS member
                            WHERE be.claim_id = o.claim_id
                              AND be.resources = o.resource
                              AND member.value = ?
                        )
                    )
                """
                operation_parameters = (resource, resource)
            else:
                operation_filter = "o.resource = ?"
                operation_parameters = (resource,)

            if "operations" in tables and "reconciliations" in tables:
                operation_rows = db.execute(
                    f"""
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
                    WHERE o.state = 'started' AND {operation_filter}
                    ORDER BY o.created_at, o.operation_id, o.claim_id, o.kind
                    """,
                    operation_parameters,
                ).fetchall()
            elif "operations" in tables:
                operation_rows = db.execute(
                    f"""
                    SELECT o.operation_id, o.kind, o.expected_revision, o.created_at,
                           o.request, NULL AS request_sha256,
                           NULL AS reconciliation_kind
                    FROM operations AS o
                    WHERE o.state = 'started' AND {operation_filter}
                    ORDER BY o.created_at, o.operation_id, o.claim_id, o.kind
                    """,
                    operation_parameters,
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
                    if operation_sha256 == str(reconciliation_sha256).lower():
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

    def history(self: Any, resource: str) -> dict[str, Any]:
        """Read one deterministic, token-free retained resource history.

        History deliberately uses a read-only SQLite connection and never asks
        the authority clock for a value.  In particular, an expired current
        claim is still an open epoch until a later mutation records its
        termination.
        """

        require_resource(resource)
        database = self.home / "leases.sqlite3"
        state_files = (
            database,
            database.with_name(f"{database.name}-wal"),
            database.with_name(f"{database.name}-shm"),
        )
        if any(path.is_symlink() for path in state_files):
            raise LeaseError("state-file-is-symlink", code=64)

        empty: dict[str, Any] = {
            "ok": True,
            "operation": "history",
            "resource": resource,
            "coverage": {
                "earliestRetainedAcquisitionRevision": None,
                "resourceRevisionWatermark": None,
                "legacyIncompleteCount": 0,
            },
            "epochs": [],
        }
        if not database.exists():
            return empty
        if not database.is_file():
            raise OSError("state database is not a regular file")

        with (
            closing(connect_readonly(self.home)) as db,
            transaction(db, immediate=False),
        ):
            tables = {
                str(table["name"])
                for table in db.execute(
                    "SELECT name FROM sqlite_master WHERE type = 'table'"
                )
            }

            def value(row: sqlite3.Row, name: str, default: Any = None) -> Any:
                try:
                    return row[name]
                except IndexError:
                    return default

            def number(value_to_render: Any) -> int | None:
                if value_to_render is None or isinstance(value_to_render, bool):
                    return None
                try:
                    return int(value_to_render)
                except TypeError, ValueError, OverflowError:
                    return None

            def timestamp(value_to_render: Any) -> str | None:
                if value_to_render is None:
                    return None
                try:
                    return self._timestamp(float(value_to_render))
                except TypeError, ValueError, OverflowError:
                    return None

            def timestamp_number(value_to_render: Any) -> float | None:
                if value_to_render is None:
                    return None
                try:
                    return float(value_to_render)
                except TypeError, ValueError, OverflowError:
                    return None

            def bundle_key(values: tuple[str, ...]) -> str:
                return json.dumps(list(values), separators=(",", ":"))

            def decoded_bundle(value_to_decode: Any) -> tuple[str, ...] | None:
                try:
                    decoded = json.loads(str(value_to_decode))
                except TypeError, ValueError:
                    return None
                if not isinstance(decoded, list) or not decoded:
                    return None
                if not all(isinstance(member, str) for member in decoded):
                    return None
                return tuple(decoded)

            def operation_bundle_key(value_to_decode: Any) -> tuple[str, ...] | None:
                return decoded_bundle(value_to_decode)

            resource_revision_watermark: int | None = None
            if "resources" in tables:
                watermark_row = db.execute(
                    "SELECT revision FROM resources WHERE resource = ?",
                    (resource,),
                ).fetchone()
                if watermark_row is not None:
                    resource_revision_watermark = number(
                        value(watermark_row, "revision")
                    )

            epoch_rows: list[dict[str, Any]] = []
            if "epochs" in tables:
                singleton_rows = db.execute(
                    """
                    SELECT claim_id, resource, agent_id, session_id, owner_id,
                           work_key, acquired_at, acquisition_revision
                    FROM epochs
                    WHERE resource = ?
                    """,
                    (resource,),
                ).fetchall()
                for row in singleton_rows:
                    acquired_number = timestamp_number(value(row, "acquired_at"))
                    acquisition_revision = number(value(row, "acquisition_revision"))
                    epoch_rows.append(
                        {
                            "source": "epoch",
                            "resource": resource,
                            "kind": "singleton",
                            "claimId": str(value(row, "claim_id", "")),
                            "agentId": str(value(row, "agent_id", "")),
                            "sessionId": str(value(row, "session_id", "")),
                            "ownerId": str(value(row, "owner_id", "")),
                            "workKey": str(value(row, "work_key", "")),
                            "acquiredAt": timestamp(value(row, "acquired_at")),
                            "acquisitionRevision": acquisition_revision,
                            "_acquired_number": acquired_number,
                            "_resource_key": resource,
                            "_resources": None,
                            "_operations": [],
                            "_reconciliations": [],
                            "_termination": None,
                            "_current": None,
                        }
                    )

            if "bundle_epochs" in tables:
                bundle_rows = db.execute(
                    """
                    SELECT claim_id, resources, agent_id, session_id, owner_id,
                           work_key, acquired_at, acquisition_revision
                    FROM bundle_epochs AS bundle_epoch
                    WHERE EXISTS (
                        SELECT 1
                        FROM json_each(
                            CASE WHEN json_valid(bundle_epoch.resources)
                                 THEN bundle_epoch.resources ELSE '[]' END
                        ) AS member
                        WHERE member.value = ?
                    )
                    """,
                    (resource,),
                ).fetchall()
                for row in bundle_rows:
                    resources = decoded_bundle(value(row, "resources"))
                    if resources is None or resource not in resources:
                        continue
                    acquired_number = timestamp_number(value(row, "acquired_at"))
                    acquisition_revision = number(value(row, "acquisition_revision"))
                    epoch_rows.append(
                        {
                            "source": "epoch",
                            "resource": resource,
                            "resources": list(resources),
                            "kind": "bundle",
                            "claimId": str(value(row, "claim_id", "")),
                            "agentId": str(value(row, "agent_id", "")),
                            "sessionId": str(value(row, "session_id", "")),
                            "ownerId": str(value(row, "owner_id", "")),
                            "workKey": str(value(row, "work_key", "")),
                            "acquiredAt": timestamp(value(row, "acquired_at")),
                            "acquisitionRevision": acquisition_revision,
                            "_acquired_number": acquired_number,
                            "_resource_key": bundle_key(resources),
                            "_resources": resources,
                            "_operations": [],
                            "_reconciliations": [],
                            "_termination": None,
                            "_current": None,
                        }
                    )

            current_row: sqlite3.Row | None = None
            if "claims" in tables:
                current_row = db.execute(
                    """
                    SELECT claim_id, revision, agent_id, session_id, owner_id,
                           work_key, coordination_only, acquired_at, heartbeat_at,
                           expires_at
                    FROM claims
                    WHERE resource = ?
                    """,
                    (resource,),
                ).fetchone()

            current_claim_id = (
                str(value(current_row, "claim_id"))
                if current_row is not None
                and value(current_row, "claim_id") is not None
                else None
            )
            current_snapshot: dict[str, Any] | None = None
            if current_row is not None and current_claim_id is not None:
                coordination_only = bool(value(current_row, "coordination_only", 0))
                current_snapshot = {
                    "source": "current-claim",
                    "claimId": current_claim_id,
                    "revision": number(value(current_row, "revision")),
                    "agentId": str(value(current_row, "agent_id", "")),
                    "sessionId": str(value(current_row, "session_id", "")),
                    "ownerId": str(value(current_row, "owner_id", "")),
                    "workKey": str(value(current_row, "work_key", "")),
                    "coordinationOnly": coordination_only,
                    "guarantee": (
                        "local-coordination" if coordination_only else "fenced"
                    ),
                    "acquiredAt": timestamp(value(current_row, "acquired_at")),
                    "heartbeatAt": timestamp(value(current_row, "heartbeat_at")),
                    "expiresAt": timestamp(value(current_row, "expires_at")),
                }

            termination_rows: dict[str, sqlite3.Row] = {}
            if "epoch_terminations" in tables:
                for row in db.execute(
                    """
                    SELECT resource, claim_id, reason, effective_at, recorded_at,
                           final_revision, heartbeat_at, expires_at,
                           checkpoint IS NOT NULL AS checkpoint_present,
                           successor_claim_id, operation_id
                    FROM epoch_terminations
                    WHERE resource = ?
                    """,
                    (resource,),
                ).fetchall():
                    claim_id = value(row, "claim_id")
                    if claim_id is not None:
                        termination_rows[str(claim_id)] = row

            relevant_claim_ids = tuple(str(epoch["claimId"]) for epoch in epoch_rows)
            operation_rows = (
                db.execute(
                    f"""
                    SELECT resource, claim_id, operation_id, kind, state,
                           expected_revision, created_at
                    FROM operations
                    WHERE claim_id IN ({",".join("?" for _ in relevant_claim_ids)})
                    """,
                    relevant_claim_ids,
                ).fetchall()
                if "operations" in tables and relevant_claim_ids
                else []
            )
            for row in operation_rows:
                operation_resource = str(value(row, "resource", ""))
                claim_id = str(value(row, "claim_id", ""))
                operation_id = str(value(row, "operation_id", ""))
                kind = str(value(row, "kind", ""))
                state = str(value(row, "state", "completed"))
                expected_revision = number(value(row, "expected_revision"))
                created_number = timestamp_number(value(row, "created_at"))
                operation = {
                    "source": "operation",
                    "operationId": operation_id,
                    "kind": kind,
                    "state": state,
                    "expectedRevision": expected_revision,
                    "createdAt": timestamp(value(row, "created_at")),
                    "_claim_id": claim_id,
                    "_resource": operation_resource,
                    "_resource_key": operation_resource,
                    "_bundle_key": operation_bundle_key(operation_resource),
                    "_expected_number": expected_revision,
                    "_created_number": created_number,
                }
                for epoch in epoch_rows:
                    if claim_id != epoch["claimId"]:
                        continue
                    if epoch["kind"] == "singleton":
                        matches = operation_resource == resource
                    else:
                        matches = operation["_bundle_key"] == epoch["_resources"]
                    if matches:
                        epoch["_operations"].append(operation)
                        break

            reconciliation_rows = (
                db.execute(
                    f"""
                    SELECT resource, operation_id, kind, claim_id,
                           target_claim_id, outcome,
                           reconciliation_operation_id, reconciled_at
                    FROM reconciliations
                    WHERE claim_id IN ({",".join("?" for _ in relevant_claim_ids)})
                    """,
                    relevant_claim_ids,
                ).fetchall()
                if "reconciliations" in tables and relevant_claim_ids
                else []
            )
            for row in reconciliation_rows:
                reconciliation_resource = str(value(row, "resource", ""))
                reconciliation_key = operation_bundle_key(reconciliation_resource)
                resolver_claim_id = str(value(row, "claim_id", ""))
                target_claim_id_value = value(row, "target_claim_id")
                target_claim_id = (
                    str(target_claim_id_value)
                    if target_claim_id_value not in (None, "")
                    else None
                )
                target_operation_id = str(value(row, "operation_id", ""))
                reconciliation_kind = str(value(row, "kind", ""))
                reconciliation_operation_id = str(
                    value(row, "reconciliation_operation_id", "")
                )
                recorded_at = timestamp(value(row, "reconciled_at"))
                recorded_number = timestamp_number(value(row, "reconciled_at"))
                event = {
                    "source": "reconciliation",
                    "targetClaimId": target_claim_id,
                    "targetOperationId": target_operation_id,
                    "reconciliationOperationId": reconciliation_operation_id,
                    "kind": reconciliation_kind,
                    "outcome": str(value(row, "outcome", "")),
                    "reconciledAt": recorded_at,
                    "_recorded_number": recorded_number,
                    "_target_claim_id": target_claim_id,
                    "_target_operation_id": target_operation_id,
                    "_kind": reconciliation_kind,
                    "_resource": reconciliation_resource,
                    "_resource_key": reconciliation_key,
                }
                for epoch in epoch_rows:
                    if resolver_claim_id != epoch["claimId"]:
                        continue
                    if epoch["kind"] == "singleton":
                        matches = reconciliation_resource == resource
                    else:
                        matches = reconciliation_key == epoch["_resources"]
                    if matches:
                        epoch["_reconciliations"].append(event)
                        break

            for epoch in epoch_rows:
                termination_row = termination_rows.get(epoch["claimId"])
                if termination_row is not None:
                    epoch["_termination"] = {
                        "source": "termination",
                        "reason": str(value(termination_row, "reason", "")),
                        "effectiveAt": timestamp(
                            value(termination_row, "effective_at")
                        ),
                        "recordedAt": timestamp(value(termination_row, "recorded_at")),
                        "finalRevision": number(
                            value(termination_row, "final_revision")
                        ),
                        "heartbeatAt": timestamp(
                            value(termination_row, "heartbeat_at")
                        ),
                        "expiresAt": timestamp(value(termination_row, "expires_at")),
                        "checkpointPresent": bool(
                            value(termination_row, "checkpoint_present", 0)
                        ),
                        "successorClaimId": (
                            str(value(termination_row, "successor_claim_id"))
                            if value(termination_row, "successor_claim_id") is not None
                            else None
                        ),
                        "operationId": (
                            str(value(termination_row, "operation_id"))
                            if value(termination_row, "operation_id") is not None
                            else None
                        ),
                    }
                if (
                    current_snapshot is not None
                    and current_claim_id == epoch["claimId"]
                    and epoch["_termination"] is None
                ):
                    if epoch["kind"] == "bundle":
                        epoch["_current"] = {
                            **current_snapshot,
                            "resources": list(epoch["resources"]),
                        }
                    else:
                        epoch["_current"] = current_snapshot

            def epoch_sort_key(epoch: dict[str, Any]) -> tuple[Any, ...]:
                revision = epoch["acquisitionRevision"]
                acquired_number = epoch["_acquired_number"]
                acquired_sort = (
                    acquired_number if acquired_number is not None else float("inf")
                )
                stable_resource = tuple(epoch["_resources"] or (resource,))
                if revision is None:
                    return (
                        1,
                        acquired_sort,
                        str(epoch["claimId"]),
                        str(epoch["kind"]),
                        stable_resource,
                    )
                return (
                    0,
                    revision,
                    str(epoch["claimId"]),
                    str(epoch["kind"]),
                    stable_resource,
                )

            epoch_rows.sort(key=epoch_sort_key)
            projected_epochs: list[dict[str, Any]] = []
            for epoch in epoch_rows:
                operations = epoch["_operations"]
                operations.sort(
                    key=lambda operation: (
                        operation["_expected_number"] is None,
                        operation["_expected_number"]
                        if operation["_expected_number"] is not None
                        else 0,
                        operation["_created_number"]
                        if operation["_created_number"] is not None
                        else float("inf"),
                        str(operation["operationId"]),
                        str(operation["kind"]),
                        str(operation["_claim_id"]),
                        str(operation["_resource"]),
                    )
                )
                reconciliations = epoch["_reconciliations"]
                reconciliations.sort(
                    key=lambda event: (
                        event["_recorded_number"]
                        if event["_recorded_number"] is not None
                        else float("inf"),
                        str(event["targetOperationId"]),
                        str(event["targetClaimId"] or ""),
                        str(event["kind"]),
                        str(event["reconciliationOperationId"]),
                    )
                )
                current = epoch["_current"]
                termination = epoch["_termination"]
                if epoch["acquisitionRevision"] is None or (
                    termination is None and current is None
                ):
                    completeness = "legacy-incomplete"
                elif current is not None and termination is None:
                    completeness = "open"
                else:
                    completeness = "complete"
                projected: dict[str, Any] = {
                    "source": "epoch",
                    "resource": resource,
                    "kind": epoch["kind"],
                    "claimId": epoch["claimId"],
                    "agentId": epoch["agentId"],
                    "sessionId": epoch["sessionId"],
                    "ownerId": epoch["ownerId"],
                    "workKey": epoch["workKey"],
                    "acquiredAt": epoch["acquiredAt"],
                    "acquisitionRevision": epoch["acquisitionRevision"],
                    "completeness": completeness,
                    "operations": [
                        {
                            key: value_to_render
                            for key, value_to_render in operation.items()
                            if not key.startswith("_")
                        }
                        for operation in operations
                    ],
                    "reconciliations": [
                        {
                            key: value_to_render
                            for key, value_to_render in event.items()
                            if not key.startswith("_")
                        }
                        for event in reconciliations
                    ],
                    "termination": termination,
                    "currentClaim": current,
                }
                if epoch["kind"] == "bundle":
                    projected["resources"] = list(epoch["resources"])
                projected_epochs.append(projected)

            acquisition_revisions = [
                epoch["acquisitionRevision"]
                for epoch in epoch_rows
                if epoch["acquisitionRevision"] is not None
            ]
            legacy_incomplete_count = sum(
                epoch["completeness"] == "legacy-incomplete"
                for epoch in projected_epochs
            )
            coverage = {
                "earliestRetainedAcquisitionRevision": (
                    min(acquisition_revisions) if acquisition_revisions else None
                ),
                "resourceRevisionWatermark": resource_revision_watermark,
                "legacyIncompleteCount": legacy_incomplete_count,
            }

        return {
            "ok": True,
            "operation": "history",
            "resource": resource,
            "coverage": coverage,
            "epochs": projected_epochs,
        }

    def list_claims(self: Any, resource: str | None = None) -> dict[str, Any]:
        """List claims without exposing bearer tokens."""

        if resource is not None:
            require_resource(resource)

        query = """
            SELECT c.*, be.resources AS bundle_resources
            FROM claims AS c
            LEFT JOIN bundles AS b ON b.claim_id = c.claim_id
            LEFT JOIN bundle_epochs AS be ON be.claim_id = b.claim_id
        """
        parameters: tuple[str, ...] = ()
        if resource is not None:
            query += " WHERE c.resource = ?"
            parameters = (resource,)
        query += " ORDER BY c.resource"

        with closing(self._connect()) as db, transaction(db, immediate=False):
            rows = db.execute(query, parameters).fetchall()
            claims: list[dict[str, Any]] = []
            seen_bundles: set[str] = set()
            for row in rows:
                bundle_resources = row["bundle_resources"]
                if bundle_resources is not None:
                    claim_id = str(row["claim_id"])
                    if claim_id in seen_bundles:
                        continue
                    seen_bundles.add(claim_id)
                    resources = tuple(
                        str(value) for value in json.loads(str(bundle_resources))
                    )
                    claims.append(
                        self._bundle_claim(db, row, resources=resources).to_dict(
                            include_token=False
                        )
                    )
                else:
                    claims.append(self._claim(row).to_dict(include_token=False))
        return {
            "ok": True,
            "operation": "list",
            "claims": claims,
        }


__all__ = ["ProjectionMixin"]
