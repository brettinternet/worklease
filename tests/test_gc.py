from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from worklease.models import AcquireRequest, LeaseError, MutationRequest
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
