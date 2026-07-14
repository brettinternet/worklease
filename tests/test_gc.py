from __future__ import annotations

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

    def test_cutoff_is_strict_and_excludes_boundary_records(self) -> None:
        self._seed_released("repo:gc-before-cutoff")
        self.now = 100.0
        self._seed_released("repo:gc-at-cutoff")

        result = self.store.garbage_collect(
            cutoff="1970-01-01T00:01:40Z",
        )
        eligible = result["eligible"]
        self.assertEqual(1, eligible["epochs"]["count"])
        self.assertEqual(1, eligible["releases"]["count"])
        self.assertEqual(1, eligible["resources"]["count"])
        self.assertEqual(
            "1970-01-01T00:00:00Z",
            eligible["epochs"]["oldest"],
        )
        self.assertEqual(
            "1970-01-01T00:00:00Z",
            eligible["epochs"]["newest"],
        )

    def test_active_and_expired_claims_protect_old_records(self) -> None:
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
        self.now = 10.0
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
        self.assertTrue(expired["ok"])
        self.assertTrue(active["ok"])
        result = self.store.garbage_collect(
            cutoff="1970-01-01T00:00:10Z",
        )
        for record_class in ("epochs", "resources"):
            self.assertEqual(0, result["eligible"][record_class]["count"])

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
