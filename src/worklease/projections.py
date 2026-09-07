"""Read-only lease and diagnostic projections."""

from __future__ import annotations

import hashlib
from contextlib import closing
from typing import Any

from .locking import resource_locks
from .models import LeaseError, require_bundle_resources, require_resource
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
                state = "active" if now < float(claim_row["expires_at"]) else "expired"
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

    def list_claims(self: Any, resource: str | None = None) -> dict[str, Any]:
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


__all__ = ["ProjectionMixin"]
