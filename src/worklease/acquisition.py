"""Singleton and bundle lease acquisition implementations."""

from __future__ import annotations

import json
from contextlib import closing
from typing import Any

from .models import (
    AcquireRequest,
    BundleAcquireRequest,
    LeaseError,
    deserialize_checkpoint,
    require_bundle_resources,
    require_resource,
    require_ttl,
)
from .sqlite import transaction


class AcquisitionMixin:
    """Acquire ownership epochs while preserving exact resource semantics."""

    def acquire_bundle(self: Any, request: BundleAcquireRequest) -> dict[str, Any]:
        """Acquire one shared ownership epoch for all bundle resources."""

        resources = require_bundle_resources(request.resources)
        ttl = require_ttl(request.ttl)
        with (
            self._resource_locks(resources),
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

    def acquire(self: Any, request: AcquireRequest) -> dict[str, Any]:
        """Acquire or idempotently retry one ownership epoch."""

        require_resource(request.resource)
        ttl = require_ttl(request.ttl)
        # Classify persisted bundle membership before a concurrent bundle
        # mutation can hold the resource lock. The in-lock check below
        # remains authoritative for the acquire transaction.
        with closing(self._connect()) as preflight:
            bundle = self._bundle_for_resource(preflight, request.resource)
            if bundle is not None:
                raise LeaseError(
                    "bundle-operation-required",
                    resource=request.resource,
                    claim=self._bundle_claim(preflight, bundle).to_dict(
                        include_token=False
                    ),
                )
        with (
            self._resource_lock(request.resource),
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

    def _resource_lock(self: Any, resource: str) -> Any:
        from .locking import resource_lock

        return resource_lock(resource, self.home)

    def _resource_locks(self: Any, resources: tuple[str, ...]) -> Any:
        from .locking import resource_locks

        return resource_locks(resources, self.home)


__all__ = ["AcquisitionMixin"]
