from __future__ import annotations

import hashlib
import json
import os
import sqlite3
import subprocess
import sys
import tempfile
import threading
import unittest
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from worklease.models import (
    AcquireRequest,
    BundleAcquireRequest,
    BundleMutationRequest,
    LeaseError,
    MutationRequest,
)
from worklease.sqlite import connect
from worklease.store import LeaseStore


class GarbageCollectionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.home = tempfile.TemporaryDirectory()
        self.now = 0.0
        self.store = LeaseStore(self.home.name, clock=lambda: self.now)

    def tearDown(self) -> None:
        self.home.cleanup()

    def _seed_released(self, resource: str) -> None:
        acquired = self.store.acquire(
            AcquireRequest(
                resource=resource,
                claim_id=f"{resource}-claim",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
            )
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        self.store.release(
            MutationRequest(
                resource=resource,
                claim_id=str(claim["claimId"]),
                token=str(claim["token"]),
                revision=int(claim["revision"]),
                operation_id=f"{resource}-release",
            ),
            "done",
        )

    def test_dry_run_reports_old_records_and_protects_current_claim(self) -> None:
        resource = "repo:gc-old"
        acquired = self.store.acquire(
            AcquireRequest(
                resource=resource,
                claim_id="claim-gc-old",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
            )
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        released = self.store.release(
            MutationRequest(
                resource=resource,
                claim_id=str(claim["claimId"]),
                token=str(claim["token"]),
                revision=int(claim["revision"]),
                operation_id="release-gc-old",
            ),
            "done",
        )
        self.assertTrue(released["ok"])
        self.now = 31 * 86400

        result = self.store.garbage_collect()
        self.assertTrue(result["dryRun"])
        eligible = result["eligible"]
        self.assertEqual(1, eligible["epochs"]["count"])
        self.assertEqual(1, eligible["releases"]["count"])
        self.assertEqual(1, eligible["resources"]["count"])

        current = self.store.acquire(
            AcquireRequest(
                resource=resource,
                claim_id="claim-gc-current",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc-current",
            )
        )
        self.assertEqual(
            1,
            self.store.garbage_collect()["eligible"]["epochs"]["count"],
        )
        self.assertEqual(
            0,
            self.store.garbage_collect()["eligible"]["resources"]["count"],
        )
        self.assertTrue(current["ok"])

    def test_dry_run_protects_unresolved_operations_from_resource_inventory(
        self,
    ) -> None:
        resource = "repo:gc-unknown"
        acquired = self.store.acquire(
            AcquireRequest(
                resource=resource,
                claim_id="claim-gc-unknown",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc-unknown",
            )
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        request = MutationRequest(
            resource=resource,
            claim_id=str(claim["claimId"]),
            token=str(claim["token"]),
            revision=int(claim["revision"]),
            operation_id="exec-gc-unknown",
        )
        self.assertIsNone(
            self.store.begin_operation(
                request,
                "exec",
                {"command": ["sentinel"]},
            )
        )
        self.store.release(
            MutationRequest(
                resource=resource,
                claim_id=request.claim_id,
                token=request.token,
                revision=request.revision,
                operation_id="release-gc-unknown",
            ),
            "done",
        )
        self.now = 31 * 86400
        result = self.store.garbage_collect()
        self.assertEqual(0, result["eligible"]["operations"]["count"])
        self.assertEqual(0, result["eligible"]["epochs"]["count"])
        self.assertEqual(0, result["eligible"]["resources"]["count"])

    def test_apply_is_atomic_and_preserves_resource_revision(self) -> None:
        resource = "repo:gc-apply"
        self._seed_released(resource)
        self.now = 31 * 86400

        preview = self.store.garbage_collect()
        applied = self.store.garbage_collect(apply=True)
        self.assertFalse(applied["dryRun"])
        self.assertEqual(preview["eligible"], applied["collected"])
        with connect(self.home.name) as db:
            self.assertEqual(
                0,
                db.execute(
                    "SELECT COUNT(*) FROM epochs WHERE resource = ?",
                    (resource,),
                ).fetchone()[0],
            )
            self.assertEqual(
                0,
                db.execute(
                    "SELECT COUNT(*) FROM releases WHERE resource = ?",
                    (resource,),
                ).fetchone()[0],
            )
            self.assertEqual(
                1,
                db.execute(
                    "SELECT revision FROM resources WHERE resource = ?",
                    (resource,),
                ).fetchone()[0],
            )
            for table in (
                "epochs",
                "bundle_epochs",
                "operations",
                "releases",
                "reconciliations",
            ):
                if table == "bundle_epochs":
                    query = "SELECT COUNT(*) FROM bundle_epochs"
                    parameters = ()
                else:
                    query = f"SELECT COUNT(*) FROM {table} WHERE resource = ?"
                    parameters = (resource,)
                self.assertEqual(
                    0,
                    db.execute(query, parameters).fetchone()[0],
                )

        second = self.store.garbage_collect(apply=True)
        self.assertTrue(
            all(details["count"] == 0 for details in second["collected"].values())
        )

        reacquired = self.store.acquire(
            AcquireRequest(
                resource=resource,
                claim_id="claim-gc-apply-new",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc-new",
            )
        )
        claim = reacquired["claim"]
        assert isinstance(claim, dict)
        self.assertEqual(2, claim["revision"])

    def test_bundle_apply_preserves_resource_revisions(self) -> None:
        resources = ("repo:gc-bundle-revision-a", "repo:gc-bundle-revision-b")
        acquired = self.store.acquire_bundle(
            BundleAcquireRequest(
                resources=resources,
                claim_id="bundle-gc-revision",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
            )
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        self.store.release_bundle(
            BundleMutationRequest(
                resources=resources,
                claim_id=str(claim["claimId"]),
                token=str(claim["token"]),
                revision=int(claim["revision"]),
                operation_id="bundle-release-gc-revision",
            ),
            "done",
        )
        self.now = 31 * 86400
        applied = self.store.garbage_collect(apply=True)
        self.assertEqual(2, applied["collected"]["resources"]["count"])
        reacquired = self.store.acquire_bundle(
            BundleAcquireRequest(
                resources=resources,
                claim_id="bundle-gc-revision-new",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
            )
        )
        new_claim = reacquired["claim"]
        assert isinstance(new_claim, dict)
        self.assertEqual(2, new_claim["revision"])

    def test_apply_rolls_back_on_protected_record_conflict(self) -> None:
        resource = "repo:gc-rollback"
        self._seed_released(resource)
        self.now = 31 * 86400

        class FailingStore(LeaseStore):
            @staticmethod
            def _gc_candidates(db, cutoff_value):
                candidates = LeaseStore._gc_candidates(db, cutoff_value)
                candidates["epochs"].append(candidates["epochs"][0])
                return candidates

        failing = FailingStore(self.home.name, clock=lambda: self.now)
        with self.assertRaisesRegex(LeaseError, "gc-protected-record"):
            failing.garbage_collect(apply=True)
        with connect(self.home.name) as db:
            self.assertEqual(
                1,
                db.execute(
                    "SELECT COUNT(*) FROM epochs WHERE resource = ?",
                    (resource,),
                ).fetchone()[0],
            )
            self.assertEqual(
                1,
                db.execute(
                    "SELECT COUNT(*) FROM releases WHERE resource = ?",
                    (resource,),
                ).fetchone()[0],
            )

    def test_apply_rolls_back_on_injected_sqlite_interruption(self) -> None:
        resource = "repo:gc-interrupted"
        self._seed_released(resource)
        self.now = 31 * 86400
        with connect(self.home.name) as db:
            db.execute(
                """
                CREATE TRIGGER fail_gc_epoch
                AFTER DELETE ON epochs
                BEGIN
                    SELECT RAISE(ABORT, 'injected-interruption');
                END
                """
            )

        with self.assertRaisesRegex(LeaseError, "gc-storage-conflict"):
            self.store.garbage_collect(apply=True)
        with connect(self.home.name) as db:
            self.assertEqual(
                1,
                db.execute(
                    "SELECT COUNT(*) FROM epochs WHERE resource = ?",
                    (resource,),
                ).fetchone()[0],
            )
            self.assertEqual(
                1,
                db.execute(
                    "SELECT COUNT(*) FROM releases WHERE resource = ?",
                    (resource,),
                ).fetchone()[0],
            )

    def test_cutoff_is_strict_and_excludes_boundary_records(self) -> None:
        self._seed_released("repo:gc-before-cutoff")
        self.now = 100.0
        self._seed_released("repo:gc-at-cutoff")
        with connect(self.home.name) as db:
            for label, recorded_at in (("old", 0.0), ("boundary", 100.0)):
                resource = f"repo:gc-operation-{label}"
                operation_id = f"operation-gc-{label}"
                claim_id = f"claim-gc-{label}"
                db.execute(
                    """
                    INSERT INTO operations(
                        resource, claim_id, operation_id, kind, state,
                        request, expected_revision, receipt, created_at
                    ) VALUES (?, ?, ?, 'exec', 'completed', '{}', 1, '{}', ?)
                    """,
                    (resource, claim_id, operation_id, recorded_at),
                )
                db.execute(
                    """
                    INSERT INTO reconciliations(
                        resource, operation_id, kind, claim_id, outcome,
                        evidence, resolver_agent_id, resolver_session_id,
                        resolver_owner_id, resolver_work_key, request_sha256,
                        reconciliation_operation_id, reconciled_at, receipt
                    ) VALUES (
                        ?, ?, 'exec', ?, 'observed-success', '{}',
                        'agent', 'session', 'owner', 'work',
                        ?, ?, ?, '{}'
                    )
                    """,
                    (
                        resource,
                        operation_id,
                        claim_id,
                        "0" * 64,
                        f"reconcile-{label}",
                        recorded_at,
                    ),
                )
            for label, recorded_at in (("old", 0.0), ("boundary", 100.0)):
                db.execute(
                    """
                    INSERT INTO bundle_epochs(
                        claim_id, resources, agent_id, session_id,
                        owner_id, work_key, acquired_at
                    ) VALUES (?, ?, 'agent', 'session', 'owner', 'work', ?)
                    """,
                    (
                        f"bundle-gc-{label}",
                        json.dumps([f"repo:gc-bundle-{label}"]),
                        recorded_at,
                    ),
                )

        result = self.store.garbage_collect(
            cutoff="1970-01-01T00:01:40Z",
        )
        eligible = result["eligible"]
        for details in eligible.values():
            self.assertEqual(
                {"count", "oldest", "newest"},
                set(details),
            )
        self.assertEqual(1, eligible["epochs"]["count"])
        self.assertEqual(1, eligible["releases"]["count"])
        self.assertEqual(1, eligible["resources"]["count"])
        self.assertEqual(1, eligible["bundleEpochs"]["count"])
        self.assertEqual(1, eligible["operations"]["count"])
        self.assertEqual(1, eligible["reconciliations"]["count"])
        self.assertEqual(
            "1970-01-01T00:00:00Z",
            eligible["epochs"]["oldest"],
        )
        self.assertEqual(
            "1970-01-01T00:00:00Z",
            eligible["epochs"]["newest"],
        )

    def test_expired_claim_protects_old_records(self) -> None:
        expired = self.store.acquire(
            AcquireRequest(
                resource="repo:gc-expired",
                claim_id="claim-gc-expired",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
                ttl=1.0,
            )
        )
        self.assertTrue(expired["ok"])
        self.now = 10.0
        result = self.store.garbage_collect(cutoff="1970-01-01T00:00:10Z")
        self.assertEqual(0, result["eligible"]["epochs"]["count"])
        self.assertEqual(0, result["eligible"]["resources"]["count"])

    def test_active_claim_protects_old_records(self) -> None:
        active = self.store.acquire(
            AcquireRequest(
                resource="repo:gc-active",
                claim_id="claim-gc-active",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
                ttl=100.0,
            )
        )
        self.assertTrue(active["ok"])
        self.now = 10.0
        result = self.store.garbage_collect(cutoff="1970-01-01T00:00:10Z")
        self.assertEqual(0, result["eligible"]["epochs"]["count"])
        self.assertEqual(0, result["eligible"]["resources"]["count"])

    def test_unresolved_bundle_operation_protects_epoch_and_resources(self) -> None:
        resources = ("repo:gc-bundle-a", "repo:gc-bundle-b")
        acquired = self.store.acquire_bundle(
            BundleAcquireRequest(
                resources=resources,
                claim_id="bundle-gc-unknown",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
            )
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        request = BundleMutationRequest(
            resources=resources,
            claim_id=str(claim["claimId"]),
            token=str(claim["token"]),
            revision=int(claim["revision"]),
            operation_id="bundle-exec-gc-unknown",
        )
        self.assertIsNone(
            self.store.begin_bundle_operation(
                request,
                "exec",
                {"command": ["sentinel"]},
            )
        )
        release_request = BundleMutationRequest(
            resources=resources,
            claim_id=request.claim_id,
            token=request.token,
            revision=request.revision,
            operation_id="bundle-release-gc-unknown",
        )
        self.store.release_bundle(release_request, "done")
        self.now = 31 * 86400
        eligible = self.store.garbage_collect()["eligible"]
        self.assertEqual(0, eligible["bundleEpochs"]["count"])
        with connect(self.home.name) as db:
            self.assertEqual(
                "started",
                db.execute(
                    "SELECT state FROM operations WHERE operation_id = ?",
                    ("bundle-exec-gc-unknown",),
                ).fetchone()[0],
            )
        self.assertEqual(0, eligible["resources"]["count"])

    def test_gc_serializes_concurrent_acquire_and_heartbeat(self) -> None:
        resource = "repo:gc-concurrent"
        self._seed_released(resource)
        active = self.store.acquire(
            AcquireRequest(
                resource="repo:gc-concurrent-active",
                claim_id="claim-gc-concurrent-active",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
                ttl=3600.0,
            )
        )
        active_claim = active["claim"]
        assert isinstance(active_claim, dict)
        heartbeat_request = MutationRequest(
            resource="repo:gc-concurrent-active",
            claim_id=str(active_claim["claimId"]),
            token=str(active_claim["token"]),
            revision=int(active_claim["revision"]),
            operation_id="heartbeat-gc-concurrent",
        )
        self.now = 100.0
        entered = threading.Event()
        continue_collection = threading.Event()

        class PausingStore(LeaseStore):
            @staticmethod
            def _gc_candidates(
                db: sqlite3.Connection, cutoff_value: float
            ) -> dict[str, list[sqlite3.Row]]:
                candidates = LeaseStore._gc_candidates(db, cutoff_value)
                entered.set()
                if not continue_collection.wait(5):
                    raise AssertionError("timed out waiting to continue collection")
                return candidates

        pausing = PausingStore(self.home.name, clock=lambda: self.now)
        with ThreadPoolExecutor(max_workers=3) as executor:
            collection = executor.submit(
                pausing.garbage_collect,
                cutoff="1970-01-01T00:01:40Z",
                apply=True,
            )
            self.assertTrue(entered.wait(5))
            reacquire = executor.submit(
                self.store.acquire,
                AcquireRequest(
                    resource=resource,
                    claim_id="claim-gc-concurrent-reacquire",
                    agent_id="agent",
                    session_id="session",
                    owner_id="owner",
                    work_key="implement:gc",
                ),
            )
            heartbeat = executor.submit(
                self.store.heartbeat,
                heartbeat_request,
            )
            continue_collection.set()
            applied = collection.result(timeout=5)
            reacquired = reacquire.result(timeout=5)
            renewed = heartbeat.result(timeout=5)

        self.assertFalse(applied["dryRun"])
        self.assertEqual(2, reacquired["claim"]["revision"])
        self.assertEqual(2, renewed["claim"]["revision"])

    def test_gc_runs_after_legacy_claim_schema_migration(self) -> None:
        database = Path(self.home.name) / "leases.sqlite3"
        with sqlite3.connect(database) as db:
            db.executescript(
                """
                CREATE TABLE schema_meta(version INTEGER PRIMARY KEY);
                INSERT INTO schema_meta(version) VALUES (1);
                CREATE TABLE claims(
                    resource TEXT PRIMARY KEY,
                    claim_id TEXT NOT NULL,
                    token TEXT NOT NULL,
                    revision INTEGER NOT NULL,
                    agent_id TEXT NOT NULL,
                    session_id TEXT NOT NULL,
                    owner_id TEXT NOT NULL,
                    work_key TEXT NOT NULL,
                    acquired_at REAL NOT NULL,
                    heartbeat_at REAL NOT NULL,
                    expires_at REAL NOT NULL
                );
                INSERT INTO claims VALUES(
                    'legacy-gc', 'legacy-claim', 'secret', 1,
                    'agent', 'session', 'owner', 'work', 0.0, 0.0, 1.0
                );
                """
            )

        migrated = LeaseStore(self.home.name, clock=lambda: 31 * 86400)
        result = migrated.garbage_collect()
        self.assertEqual(0, result["eligible"]["epochs"]["count"])
        self.assertEqual("expired", migrated.status("legacy-gc")["state"])

    def test_gc_serializes_operation_reconciliation_and_release(self) -> None:
        operation_resource = "repo:gc-operation"
        operation_claim = self.store.acquire(
            AcquireRequest(
                resource=operation_resource,
                claim_id="claim-gc-operation",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
            )
        )["claim"]
        assert isinstance(operation_claim, dict)
        operation_request = MutationRequest(
            resource=operation_resource,
            claim_id=str(operation_claim["claimId"]),
            token=str(operation_claim["token"]),
            revision=int(operation_claim["revision"]),
            operation_id="exec-gc-operation",
        )
        operation_payload = {"command": ["sentinel"]}
        self.assertIsNone(
            self.store.begin_operation(operation_request, "exec", operation_payload)
        )

        reconciliation_resource = "repo:gc-reconciliation"
        reconciliation_claim = self.store.acquire(
            AcquireRequest(
                resource=reconciliation_resource,
                claim_id="claim-gc-reconciliation",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
            )
        )["claim"]
        assert isinstance(reconciliation_claim, dict)
        target_operation = "target-gc-reconciliation"
        target_payload = {"command": ["target"]}
        target_request = MutationRequest(
            resource=reconciliation_resource,
            claim_id=str(reconciliation_claim["claimId"]),
            token=str(reconciliation_claim["token"]),
            revision=int(reconciliation_claim["revision"]),
            operation_id=target_operation,
        )
        self.assertIsNone(
            self.store.begin_operation(target_request, "exec", target_payload)
        )
        reconciliation_request = MutationRequest(
            resource=reconciliation_resource,
            claim_id=target_request.claim_id,
            token=target_request.token,
            revision=target_request.revision,
            operation_id="reconcile-gc-operation",
        )
        request_sha256 = hashlib.sha256(
            json.dumps(target_payload, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()

        release_resource = "repo:gc-release"
        release_claim = self.store.acquire(
            AcquireRequest(
                resource=release_resource,
                claim_id="claim-gc-release",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
            )
        )["claim"]
        assert isinstance(release_claim, dict)
        release_request = MutationRequest(
            resource=release_resource,
            claim_id=str(release_claim["claimId"]),
            token=str(release_claim["token"]),
            revision=int(release_claim["revision"]),
            operation_id="release-gc-release",
        )

        self._seed_released("repo:gc-lifecycle-old")
        self.now = 100.0
        entered = threading.Event()
        continue_collection = threading.Event()
        base_candidates = LeaseStore._gc_candidates

        class PausingStore(LeaseStore):
            @staticmethod
            def _gc_candidates(db, cutoff_value):
                candidates = base_candidates(db, cutoff_value)
                entered.set()
                if not continue_collection.wait(5):
                    raise AssertionError("timed out waiting to continue collection")
                return candidates

        pausing = PausingStore(self.home.name, clock=lambda: self.now)
        with ThreadPoolExecutor(max_workers=4) as executor:
            collection = executor.submit(
                pausing.garbage_collect, cutoff="1970-01-01T00:01:40Z", apply=True
            )
            self.assertTrue(entered.wait(5))
            complete = executor.submit(
                self.store.complete_operation,
                operation_request,
                "exec",
                operation_payload,
                {
                    "ok": True,
                    "operation": "exec",
                    "operationId": operation_request.operation_id,
                },
            )
            reconcile = executor.submit(
                self.store.reconcile_operation,
                reconciliation_request,
                target_operation,
                request_sha256,
                "observed-success",
                {"status": "ok"},
            )
            release = executor.submit(self.store.release, release_request, "done")
            continue_collection.set()
            applied = collection.result(timeout=5)
            completed = complete.result(timeout=5)
            reconciled = reconcile.result(timeout=5)
            released = release.result(timeout=5)

        self.assertFalse(applied["dryRun"])
        self.assertEqual(1, applied["collected"]["epochs"]["count"])
        self.assertEqual(1, applied["collected"]["releases"]["count"])
        self.assertTrue(completed["ok"])
        self.assertTrue(reconciled["ok"])
        self.assertTrue(released["ok"])

    def test_dry_run_does_not_mutate_records_or_revisions(self) -> None:
        resource = "repo:gc-dry-run-snapshot"
        self._seed_released(resource)
        self.now = 31 * 86400
        with connect(self.home.name) as db:
            before = {
                table: db.execute(f"SELECT * FROM {table} ORDER BY rowid").fetchall()
                for table in (
                    "epochs",
                    "bundle_epochs",
                    "operations",
                    "releases",
                    "reconciliations",
                    "resources",
                )
            }
        self.store.garbage_collect()
        with connect(self.home.name) as db:
            after = {
                table: db.execute(f"SELECT * FROM {table} ORDER BY rowid").fetchall()
                for table in before
            }
        self.assertEqual(before, after)

    def test_reused_operation_id_does_not_hide_unresolved_operation(self) -> None:
        resource = "repo:gc-reused-operation"
        first = self.store.acquire(
            AcquireRequest(
                resource=resource,
                claim_id="claim-gc-reused-first",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
            )
        )["claim"]
        assert isinstance(first, dict)
        first_request = MutationRequest(
            resource=resource,
            claim_id=str(first["claimId"]),
            token=str(first["token"]),
            revision=int(first["revision"]),
            operation_id="reused-operation",
        )
        first_payload = {"command": ["old"]}
        self.assertIsNone(
            self.store.begin_operation(first_request, "exec", first_payload)
        )
        first_sha = hashlib.sha256(
            json.dumps(first_payload, sort_keys=True, separators=(",", ":")).encode()
        ).hexdigest()
        self.store.reconcile_operation(
            MutationRequest(
                resource=resource,
                claim_id=first_request.claim_id,
                token=first_request.token,
                revision=first_request.revision,
                operation_id="reconcile-reused-old",
            ),
            first_request.operation_id,
            first_sha,
            "observed-success",
            {"status": "ok"},
        )
        self.store.release(
            MutationRequest(
                resource=resource,
                claim_id=first_request.claim_id,
                token=first_request.token,
                revision=2,
                operation_id="release-reused-old",
            ),
            "done",
        )

        self.now = 5.0
        second = self.store.acquire(
            AcquireRequest(
                resource=resource,
                claim_id="claim-gc-reused-second",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:gc",
            )
        )["claim"]
        assert isinstance(second, dict)
        second_request = MutationRequest(
            resource=resource,
            claim_id=str(second["claimId"]),
            token=str(second["token"]),
            revision=int(second["revision"]),
            operation_id="reused-operation",
        )
        self.assertIsNone(
            self.store.begin_operation(second_request, "exec", {"command": ["new"]})
        )
        self.store.release(
            MutationRequest(
                resource=resource,
                claim_id=second_request.claim_id,
                token=second_request.token,
                revision=second_request.revision,
                operation_id="release-reused-new",
            ),
            "done",
        )
        self.now = 10.0
        applied = self.store.garbage_collect(cutoff="1970-01-01T00:00:10Z", apply=True)
        self.assertEqual(1, applied["collected"]["operations"]["count"])
        with connect(self.home.name) as db:
            remaining = db.execute(
                "SELECT claim_id, state FROM operations WHERE resource = ?",
                (resource,),
            ).fetchall()
            resource_rows = db.execute(
                "SELECT resource, revision FROM resources WHERE resource = ?",
                (resource,),
            ).fetchall()
        self.assertEqual(
            [("claim-gc-reused-second", "started")],
            [(str(row[0]), str(row[1])) for row in remaining],
        )
        self.assertEqual(
            [(resource, 3)],
            [(str(row[0]), int(row[1])) for row in resource_rows],
        )

    def test_invalid_cutoff_is_stable_and_non_mutating(self) -> None:
        with self.assertRaisesRegex(LeaseError, "invalid-gc-cutoff"):
            self.store.garbage_collect(cutoff="not-a-timestamp")

    def test_cli_returns_schema_versioned_dry_run(self) -> None:
        environment = os.environ.copy()
        environment["WORKLEASE_HOME"] = self.home.name
        result = subprocess.run(
            [sys.executable, "-m", "worklease.cli", "gc"],
            check=False,
            capture_output=True,
            text=True,
            env=environment,
            cwd=Path(__file__).parents[1],
        )
        self.assertEqual(0, result.returncode, result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(1, payload["schemaVersion"])
        self.assertEqual("gc", payload["operation"])
        self.assertTrue(payload["dryRun"])


if __name__ == "__main__":
    unittest.main()
