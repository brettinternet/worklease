"""Retention inventory and atomic garbage collection for lease history."""

from __future__ import annotations

import datetime as dt
import math
import sqlite3
from contextlib import closing
from typing import Any

from .models import LeaseError, iso8601
from .sqlite import transaction

DEFAULT_GC_RETENTION_DAYS = 30.0


class GarbageCollectionMixin:
    """Capture and optionally delete only records safe for collection."""

    @staticmethod
    def _gc_summary(timestamps: list[float]) -> dict[str, Any]:
        if not timestamps:
            return {"count": 0, "oldest": None, "newest": None}
        return {
            "count": len(timestamps),
            "oldest": iso8601(min(timestamps)),
            "newest": iso8601(max(timestamps)),
        }

    @staticmethod
    def _gc_cutoff(
        now: float, retention_days: float | None, cutoff: str | None
    ) -> tuple[float, float | None]:
        if retention_days is not None and cutoff is not None:
            raise LeaseError("conflicting-gc-options", code=64)
        if cutoff is not None:
            if not isinstance(cutoff, str) or not cutoff.strip():
                raise LeaseError("invalid-gc-cutoff", code=64)
            try:
                value = dt.datetime.fromisoformat(cutoff.strip().replace("Z", "+00:00"))
                if value.tzinfo is None:
                    raise ValueError
                cutoff_value = value.timestamp()
            except (TypeError, ValueError, OverflowError) as error:
                raise LeaseError("invalid-gc-cutoff", code=64) from error
            if not math.isfinite(cutoff_value) or cutoff_value > now:
                raise LeaseError("invalid-gc-cutoff", code=64)
            return cutoff_value, None
        days = DEFAULT_GC_RETENTION_DAYS if retention_days is None else retention_days
        try:
            days = float(days)
        except (TypeError, ValueError) as error:
            raise LeaseError("invalid-gc-retention", code=64) from error
        if not math.isfinite(days) or days <= 0 or days > 36500:
            raise LeaseError(
                "invalid-gc-retention",
                code=64,
                minimumExclusive=0,
                maximumInclusive=36500,
            )
        return now - days * 86400, days

    @staticmethod
    def _expired_claim_inventory(
        db: sqlite3.Connection, cutoff_value: float
    ) -> tuple[dict[str, list[sqlite3.Row]], dict[str, list[sqlite3.Row]]]:
        """Separate old expired claims from claims protected by unknown work."""

        unresolved = """
            EXISTS (
                SELECT 1 FROM operations AS o
                WHERE o.claim_id = {claim_id}
                  AND o.state = 'started'
                  AND NOT EXISTS (
                      SELECT 1 FROM reconciliations AS rec
                      WHERE rec.resource = o.resource
                        AND rec.operation_id = o.operation_id
                        AND rec.target_claim_id = o.claim_id
                        AND rec.kind = o.kind
                  )
            )
        """
        singleton_base = """
            SELECT c.*, c.expires_at AS recorded_at
            FROM claims AS c
            WHERE c.expires_at < ?
              AND NOT EXISTS (
                  SELECT 1 FROM bundle_members AS member
                  WHERE member.claim_id = c.claim_id
              )
              AND NOT EXISTS (
                  SELECT 1 FROM bundles AS bundle
                  WHERE bundle.claim_id = c.claim_id
              )
              AND {protection}
            ORDER BY c.resource, c.claim_id
        """
        bundle_base = """
            SELECT b.*, b.expires_at AS recorded_at
            FROM bundles AS b
            WHERE b.expires_at < ?
              AND {protection}
            ORDER BY b.claim_id
        """

        def rows(
            statement: str, claim_id: str, *, protected: bool
        ) -> list[sqlite3.Row]:
            predicate = unresolved.format(claim_id=claim_id)
            protection = predicate if protected else f"NOT ({predicate})"
            return db.execute(
                statement.format(protection=protection), (cutoff_value,)
            ).fetchall()

        eligible = {
            "expiredClaims": rows(singleton_base, "c.claim_id", protected=False),
            "expiredBundleClaims": rows(bundle_base, "b.claim_id", protected=False),
        }
        protected = {
            "expiredClaims": rows(singleton_base, "c.claim_id", protected=True),
            "expiredBundleClaims": rows(bundle_base, "b.claim_id", protected=True),
        }
        return eligible, protected

    @staticmethod
    def _gc_candidates(
        db: sqlite3.Connection, cutoff_value: float
    ) -> dict[str, list[sqlite3.Row]]:
        """Capture one atomic set of historical records eligible for collection."""

        epoch_rows = db.execute(
            """
            SELECT claim_id,
                   COALESCE(
                       (SELECT MAX(t.recorded_at)
                        FROM epoch_terminations AS t
                        WHERE t.claim_id = e.claim_id),
                       acquired_at
                   ) AS recorded_at
            FROM epochs AS e
            WHERE COALESCE(
                      (SELECT MAX(t.recorded_at)
                       FROM epoch_terminations AS t
                       WHERE t.claim_id = e.claim_id),
                      acquired_at
                  ) < ?
              AND NOT EXISTS (
                  SELECT 1 FROM claims AS c
                  WHERE c.claim_id = e.claim_id
              )
              AND NOT EXISTS (
                  SELECT 1 FROM operations AS o
                  WHERE o.claim_id = e.claim_id
                    AND o.created_at >= ?
              )
              AND NOT EXISTS (
                  SELECT 1 FROM operations AS unresolved
                  WHERE unresolved.claim_id = e.claim_id
                    AND unresolved.state = 'started'
                    AND NOT EXISTS (
                        SELECT 1
                        FROM reconciliations AS rec
                        WHERE rec.resource = unresolved.resource
                          AND rec.operation_id = unresolved.operation_id
                          AND rec.target_claim_id = unresolved.claim_id
                          AND rec.kind = unresolved.kind
                    )
              )
              AND NOT EXISTS (
                  SELECT 1 FROM releases AS r
                  WHERE r.claim_id = e.claim_id
                    AND r.released_at >= ?
              )
              AND NOT EXISTS (
                  SELECT 1 FROM reconciliations AS rec
                  WHERE rec.claim_id = e.claim_id
                    AND rec.reconciled_at >= ?
              )
            """,
            (cutoff_value, cutoff_value, cutoff_value, cutoff_value),
        ).fetchall()
        bundle_epoch_rows = db.execute(
            """
            SELECT claim_id,
                   COALESCE(
                       (SELECT MAX(t.recorded_at)
                        FROM epoch_terminations AS t
                        WHERE t.claim_id = e.claim_id),
                       acquired_at
                   ) AS recorded_at
            FROM bundle_epochs AS e
            WHERE COALESCE(
                      (SELECT MAX(t.recorded_at)
                       FROM epoch_terminations AS t
                       WHERE t.claim_id = e.claim_id),
                      acquired_at
                  ) < ?
              AND NOT EXISTS (
                  SELECT 1 FROM bundles AS b
                  WHERE b.claim_id = e.claim_id
              )
              AND NOT EXISTS (
                  SELECT 1
                  FROM claims AS c, json_each(e.resources) AS member
                  WHERE c.resource = member.value
                    AND c.claim_id = e.claim_id
              )
              AND NOT EXISTS (
                  SELECT 1 FROM operations AS o
                  WHERE o.claim_id = e.claim_id
                    AND o.created_at >= ?
              )
              AND NOT EXISTS (
                  SELECT 1 FROM operations AS unresolved
                  WHERE unresolved.claim_id = e.claim_id
                    AND unresolved.state = 'started'
                    AND NOT EXISTS (
                        SELECT 1
                        FROM reconciliations AS rec
                        WHERE rec.resource = unresolved.resource
                          AND rec.operation_id = unresolved.operation_id
                          AND rec.target_claim_id = unresolved.claim_id
                          AND rec.kind = unresolved.kind
                    )
              )
              AND NOT EXISTS (
                  SELECT 1 FROM reconciliations AS rec
                  WHERE rec.claim_id = e.claim_id
                    AND rec.reconciled_at >= ?
              )
            """,
            (cutoff_value, cutoff_value, cutoff_value),
        ).fetchall()
        operation_rows = db.execute(
            """
            SELECT resource, claim_id, operation_id, kind,
                   created_at AS recorded_at
            FROM operations AS o
            WHERE created_at < ?
              AND (
                  o.state = 'completed'
                  OR EXISTS (
                      SELECT 1 FROM reconciliations AS r
                      WHERE r.resource = o.resource
                        AND r.operation_id = o.operation_id
                        AND r.target_claim_id = o.claim_id
                        AND r.kind = o.kind
                  )
              )
              AND NOT EXISTS (
                  SELECT 1 FROM claims AS c
                  WHERE c.claim_id = o.claim_id
              )
              AND NOT EXISTS (
                  SELECT 1 FROM bundles AS b
                  WHERE b.claim_id = o.claim_id
              )
              AND NOT EXISTS (
                  SELECT 1 FROM epoch_terminations AS t
                  WHERE t.claim_id = o.claim_id
                    AND t.recorded_at >= ?
              )
              AND NOT EXISTS (
                  SELECT 1
                  FROM reconciliations AS r
                  WHERE r.resource = o.resource
                    AND r.operation_id = o.operation_id
                    AND r.target_claim_id = o.claim_id
                    AND r.kind = o.kind
                    AND r.reconciled_at >= ?
              )
            """,
            (cutoff_value, cutoff_value, cutoff_value),
        ).fetchall()
        release_rows = db.execute(
            """
            SELECT resource, claim_id, released_at AS recorded_at
            FROM releases AS r
            WHERE released_at < ?
              AND NOT EXISTS (
                  SELECT 1 FROM epoch_terminations AS t
                  WHERE t.claim_id = r.claim_id
                    AND t.recorded_at >= ?
              )
            """,
            (cutoff_value, cutoff_value),
        ).fetchall()
        reconciliation_rows = db.execute(
            """
            SELECT resource, operation_id, reconciled_at AS recorded_at
            FROM reconciliations AS r
            WHERE reconciled_at < ?
              AND (
                  r.target_claim_id <> ''
                  OR (
                      SELECT COUNT(*)
                      FROM operations AS target
                      WHERE target.resource = r.resource
                        AND target.operation_id = r.operation_id
                        AND target.kind = r.kind
                  ) = 1
              )
              AND NOT EXISTS (
                  SELECT 1 FROM claims AS c
                  WHERE c.claim_id = r.claim_id
              )
              AND NOT EXISTS (
                  SELECT 1 FROM epoch_terminations AS t
                  WHERE t.recorded_at >= ?
                    AND t.claim_id IN (r.claim_id, r.target_claim_id)
              )
              AND NOT EXISTS (
                  SELECT 1
                  FROM operations AS o
                  WHERE o.resource = r.resource
                    AND o.operation_id = r.operation_id
                    AND o.kind = r.kind
                    AND o.created_at >= ?
                    AND (
                        o.claim_id = r.target_claim_id
                        OR (
                            r.target_claim_id = ''
                            AND NOT EXISTS (
                                SELECT 1
                                FROM operations AS duplicate
                                WHERE duplicate.resource = o.resource
                                  AND duplicate.operation_id = o.operation_id
                                  AND duplicate.kind = o.kind
                                  AND duplicate.claim_id != o.claim_id
                            )
                        )
                    )
              )
            """,
            (cutoff_value, cutoff_value, cutoff_value),
        ).fetchall()
        resource_rows = db.execute(
            """
            SELECT history.resource, latest_recorded_at AS recorded_at,
                   r.revision
            FROM (
                SELECT resource, MAX(recorded_at) AS latest_recorded_at
                FROM (
                    SELECT resource, acquired_at AS recorded_at
                    FROM epochs
                    UNION ALL
                    SELECT json_each.value AS resource,
                           acquired_at AS recorded_at
                    FROM bundle_epochs, json_each(bundle_epochs.resources)
                    UNION ALL
                    SELECT operations.resource, created_at AS recorded_at
                    FROM operations
                    WHERE NOT EXISTS (
                        SELECT 1
                        FROM bundle_epochs AS bundle
                        WHERE bundle.claim_id = operations.claim_id
                    )
                    UNION ALL
                    SELECT resource, released_at AS recorded_at
                    FROM releases
                    UNION ALL
                    SELECT resource, reconciled_at AS recorded_at
                    FROM reconciliations
                    UNION ALL
                    SELECT resource, recorded_at
                    FROM epoch_terminations
                )
                GROUP BY resource
            ) AS history
            JOIN resources AS r ON r.resource = history.resource
            WHERE latest_recorded_at < ?
              AND NOT EXISTS (
                  SELECT 1 FROM claims AS c
                  WHERE c.resource = r.resource
              )
              AND NOT EXISTS (
                  SELECT 1
                  FROM bundle_members AS m
                  JOIN bundles AS b ON b.claim_id = m.claim_id
                  WHERE m.resource = r.resource
              )
              AND NOT EXISTS (
                  SELECT 1
                  FROM operations AS o
                  WHERE o.resource = r.resource
                    AND o.state = 'started'
                    AND NOT EXISTS (
                        SELECT 1
                        FROM reconciliations AS rec
                        WHERE rec.resource = o.resource
                          AND rec.operation_id = o.operation_id
                          AND rec.target_claim_id = o.claim_id
                          AND rec.kind = o.kind
                    )
              )
              AND NOT EXISTS (
                  SELECT 1
                  FROM operations AS o
                  JOIN bundle_epochs AS be ON be.claim_id = o.claim_id
                  JOIN json_each(be.resources) AS member
                  WHERE member.value = r.resource
                    AND o.state = 'started'
                    AND NOT EXISTS (
                        SELECT 1
                        FROM reconciliations AS rec
                        WHERE rec.resource = o.resource
                          AND rec.operation_id = o.operation_id
                          AND rec.target_claim_id = o.claim_id
                          AND rec.kind = o.kind
                    )
              )
            """,
            (cutoff_value,),
        ).fetchall()
        return {
            "epochs": epoch_rows,
            "bundleEpochs": bundle_epoch_rows,
            "operations": operation_rows,
            "releases": release_rows,
            "reconciliations": reconciliation_rows,
            "resources": resource_rows,
        }

    def _delete_gc_candidates(
        self: Any,
        db: sqlite3.Connection,
        candidates: dict[str, list[sqlite3.Row]],
        *,
        recorded_at: float,
    ) -> None:
        for claim in candidates["expiredClaims"]:
            db.execute(
                """
                INSERT INTO resources(resource, revision) VALUES (?, ?)
                ON CONFLICT(resource) DO UPDATE SET
                    revision = MAX(resources.revision, excluded.revision)
                """,
                (str(claim["resource"]), int(claim["revision"])),
            )
            if (
                db.execute(
                    "SELECT 1 FROM epochs WHERE claim_id = ?",
                    (str(claim["claim_id"]),),
                ).fetchone()
                is not None
            ):
                self._record_epoch_termination(
                    db,
                    claim,
                    reason="expired",
                    effective_at=float(claim["expires_at"]),
                    recorded_at=recorded_at,
                )
            deleted = db.execute(
                "DELETE FROM claims WHERE resource = ? AND claim_id = ?",
                (str(claim["resource"]), str(claim["claim_id"])),
            ).rowcount
            if deleted != 1:
                raise LeaseError(
                    "gc-protected-record", code=3, recordClass="expiredClaims"
                )

        for bundle in candidates["expiredBundleClaims"]:
            claim_id = str(bundle["claim_id"])
            members = db.execute(
                "SELECT resource FROM bundle_members WHERE claim_id = ? "
                "ORDER BY resource",
                (claim_id,),
            ).fetchall()
            claims = db.execute(
                "SELECT * FROM claims WHERE claim_id = ? ORDER BY resource",
                (claim_id,),
            ).fetchall()
            if (
                not members
                or [row["resource"] for row in members]
                != [row["resource"] for row in claims]
                or any(
                    int(claim["revision"]) != int(bundle["revision"])
                    or float(claim["expires_at"]) != float(bundle["expires_at"])
                    for claim in claims
                )
            ):
                raise LeaseError(
                    "gc-protected-record",
                    code=3,
                    recordClass="expiredBundleClaims",
                )
            has_epoch = (
                db.execute(
                    "SELECT 1 FROM bundle_epochs WHERE claim_id = ?", (claim_id,)
                ).fetchone()
                is not None
            )
            for claim in claims:
                db.execute(
                    """
                    INSERT INTO resources(resource, revision) VALUES (?, ?)
                    ON CONFLICT(resource) DO UPDATE SET
                        revision = MAX(resources.revision, excluded.revision)
                    """,
                    (str(claim["resource"]), int(claim["revision"])),
                )
                if has_epoch:
                    self._record_epoch_termination(
                        db,
                        claim,
                        reason="expired",
                        effective_at=float(claim["expires_at"]),
                        recorded_at=recorded_at,
                    )
            if db.execute(
                "DELETE FROM claims WHERE claim_id = ?", (claim_id,)
            ).rowcount != len(claims):
                raise LeaseError(
                    "gc-protected-record",
                    code=3,
                    recordClass="expiredBundleClaims",
                )
            if db.execute(
                "DELETE FROM bundle_members WHERE claim_id = ?", (claim_id,)
            ).rowcount != len(members):
                raise LeaseError(
                    "gc-protected-record",
                    code=3,
                    recordClass="expiredBundleClaims",
                )
            if (
                db.execute(
                    "DELETE FROM bundles WHERE claim_id = ?", (claim_id,)
                ).rowcount
                != 1
            ):
                raise LeaseError(
                    "gc-protected-record",
                    code=3,
                    recordClass="expiredBundleClaims",
                )

        statements = (
            (
                "reconciliations",
                "DELETE FROM reconciliations WHERE resource = ? AND operation_id = ?",
                lambda row: (str(row["resource"]), str(row["operation_id"])),
            ),
            (
                "operations",
                "DELETE FROM operations "
                "WHERE resource = ? AND claim_id = "
                "? AND operation_id = ? AND kind = ?",
                lambda row: (
                    str(row["resource"]),
                    str(row["claim_id"]),
                    str(row["operation_id"]),
                    str(row["kind"]),
                ),
            ),
            (
                "releases",
                "DELETE FROM releases WHERE resource = ? AND claim_id = ?",
                lambda row: (str(row["resource"]), str(row["claim_id"])),
            ),
            (
                "bundleEpochs",
                "DELETE FROM bundle_epochs WHERE claim_id = ?",
                lambda row: (str(row["claim_id"]),),
            ),
            (
                "epochs",
                "DELETE FROM epochs WHERE claim_id = ?",
                lambda row: (str(row["claim_id"]),),
            ),
            (
                "resources",
                "DELETE FROM resources WHERE resource = ?",
                lambda row: (str(row["resource"]),),
            ),
        )
        for name, statement, parameters in statements:
            for row in candidates[name]:
                if name in {"bundleEpochs", "epochs"}:
                    db.execute(
                        "DELETE FROM epoch_terminations WHERE claim_id = ?",
                        (str(row["claim_id"]),),
                    )
                deleted = db.execute(statement, parameters(row)).rowcount
                if deleted != 1:
                    raise LeaseError(
                        "gc-protected-record",
                        code=3,
                        recordClass=name,
                    )
                if name == "resources":
                    db.execute(
                        "INSERT INTO resources(resource, revision) VALUES (?, ?)",
                        (str(row["resource"]), int(row["revision"])),
                    )

    def garbage_collect(
        self: Any,
        *,
        retention_days: float | None = None,
        cutoff: str | None = None,
        apply: bool = False,
    ) -> dict[str, Any]:
        """Inspect or atomically collect records older than a cutoff."""

        if not isinstance(apply, bool):
            raise LeaseError("invalid-gc-apply", code=64)
        now = float(self.clock())
        cutoff_value, resolved_days = self._gc_cutoff(now, retention_days, cutoff)

        def summary(candidates: dict[str, list[sqlite3.Row]]) -> dict[str, Any]:
            return {
                name: self._gc_summary([float(row["recorded_at"]) for row in rows])
                for name, rows in candidates.items()
            }

        candidates: dict[str, list[sqlite3.Row]]
        try:
            with closing(self._connect()) as db:
                if apply:
                    with transaction(db):
                        expired, protected = self._expired_claim_inventory(
                            db, cutoff_value
                        )
                        candidates = {
                            **self._gc_candidates(db, cutoff_value),
                            **expired,
                        }
                        retirement_recorded_at = max(now, float(self.clock()))
                        self._delete_gc_candidates(
                            db, candidates, recorded_at=retirement_recorded_at
                        )
                else:
                    db.execute("BEGIN")
                    try:
                        expired, protected = self._expired_claim_inventory(
                            db, cutoff_value
                        )
                        candidates = {
                            **self._gc_candidates(db, cutoff_value),
                            **expired,
                        }
                        db.commit()
                    except BaseException:
                        db.rollback()
                        raise
        except sqlite3.Error as error:
            raise LeaseError("gc-storage-conflict", code=3) from error

        eligible = summary(candidates)
        result: dict[str, Any] = {
            "ok": True,
            "operation": "gc",
            "dryRun": not apply,
            "capturedAt": iso8601(now),
            "cutoff": iso8601(cutoff_value),
            "retentionDays": resolved_days,
            "eligible": eligible,
            "protected": summary(protected),
        }
        if apply:
            result["collected"] = eligible
        return result


__all__ = ["DEFAULT_GC_RETENTION_DAYS", "GarbageCollectionMixin"]
