"""Shared claim and bundle ownership primitives for :mod:`worklease.store`."""

from __future__ import annotations

import json
import sqlite3
from contextlib import closing, nullcontext
from typing import Any

from .credentials import credentials_match
from .locking import resource_lock
from .models import (
    BundleClaim,
    BundleMutationRequest,
    LeaseError,
    MutationRequest,
    bundle_claim_from_row,
)
from .sqlite import transaction


class ClaimStoreMixin:
    """Claim lookup, ownership guards, and bundle projections used by the store."""

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
        self: Any,
        connection: sqlite3.Connection,
        row: sqlite3.Row,
        resources: tuple[str, ...] | None = None,
    ) -> BundleClaim:
        return bundle_claim_from_row(
            row,
            (
                self._bundle_resources(connection, str(row["claim_id"]))
                if resources is None
                else resources
            ),
            self.clock(),
        )

    @staticmethod
    def _bundle_operation_resource(resources: tuple[str, ...]) -> str:
        return json.dumps(list(resources), separators=(",", ":"))

    def _bundle_for_resource(
        self: Any, connection: sqlite3.Connection, resource: str
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

    def _bundle_current(
        self: Any, connection: sqlite3.Connection, resources: tuple[str, ...]
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

    def _require_claim_owner(
        self: Any,
        connection: sqlite3.Connection,
        request: MutationRequest | BundleMutationRequest,
    ) -> sqlite3.Row:
        if isinstance(request, BundleMutationRequest):
            resource = ",".join(request.resources)
            row = self._bundle_current(connection, request.resources)
            if row is None:
                raise LeaseError("claim-not-found", resource=resource)
            if row["claim_id"] != request.claim_id:
                raise LeaseError(
                    "stale-claim",
                    resource=resource,
                    claim=self._bundle_claim(connection, row).to_dict(
                        include_token=False
                    ),
                )
            if not credentials_match(str(row["token"]), request.token):
                raise LeaseError(
                    "invalid-token",
                    resource=resource,
                    claim=self._bundle_claim(connection, row).to_dict(
                        include_token=False
                    ),
                )
            return row

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
        if row["claim_id"] != request.claim_id:
            raise LeaseError(
                "stale-claim",
                resource=request.resource,
                claim=self._claim(row).to_dict(include_token=False),
            )
        if not credentials_match(str(row["token"]), request.token):
            raise LeaseError(
                "invalid-token",
                resource=request.resource,
                claim=self._claim(row).to_dict(include_token=False),
            )
        return row

    def _require_claim_current(
        self: Any,
        connection: sqlite3.Connection,
        request: MutationRequest | BundleMutationRequest,
    ) -> sqlite3.Row:
        row = self._require_claim_owner(connection, request)
        resource = (
            ",".join(request.resources)
            if isinstance(request, BundleMutationRequest)
            else request.resource
        )
        if int(row["revision"]) != request.revision:
            raise LeaseError(
                "stale-revision",
                resource=resource,
                expectedRevision=int(row["revision"]),
                suppliedRevision=request.revision,
            )
        if self.clock() >= float(row["expires_at"]):
            claim = (
                self._bundle_claim(connection, row).to_dict(include_token=False)
                if isinstance(request, BundleMutationRequest)
                else self._claim(row).to_dict(include_token=False)
            )
            raise LeaseError("claim-expired", resource=resource, claim=claim)
        return row

    # Keep the old private names as narrow compatibility seams for existing
    # store call sites while sharing the ownership implementation above.
    def _require_owner(
        self: Any, connection: sqlite3.Connection, request: MutationRequest
    ) -> sqlite3.Row:
        return self._require_claim_owner(connection, request)

    def _require_current(
        self: Any, connection: sqlite3.Connection, request: MutationRequest
    ) -> sqlite3.Row:
        return self._require_claim_current(connection, request)

    def _require_bundle_owner(
        self: Any, connection: sqlite3.Connection, request: BundleMutationRequest
    ) -> sqlite3.Row:
        return self._require_claim_owner(connection, request)

    def _require_bundle_current(
        self: Any, connection: sqlite3.Connection, request: BundleMutationRequest
    ) -> sqlite3.Row:
        return self._require_claim_current(connection, request)

    def owner_claim(
        self: Any, request: MutationRequest, *, lock_held: bool = False
    ) -> dict[str, Any]:
        """Read the matching ownership epoch without requiring its revision."""

        lock = (
            nullcontext() if lock_held else resource_lock(request.resource, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            row = self._require_owner(db, request)
            return self._claim(row).to_dict(include_token=False)

    def validate_current(
        self: Any, request: MutationRequest, *, lock_held: bool = False
    ) -> dict[str, Any]:
        """Validate ownership immediately before a guarded side effect."""

        lock = (
            nullcontext() if lock_held else resource_lock(request.resource, self.home)
        )
        with lock, closing(self._connect()) as db, transaction(db):
            row = self._require_current(db, request)
            return self._claim(row).to_dict()


__all__ = ["ClaimStoreMixin"]
