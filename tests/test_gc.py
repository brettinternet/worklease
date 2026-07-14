from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from worklease.models import AcquireRequest, LeaseError, MutationRequest
from worklease.store import LeaseStore


class GarbageCollectionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.home = tempfile.TemporaryDirectory()
        self.now = 0.0
        self.store = LeaseStore(self.home.name, clock=lambda: self.now)

    def tearDown(self) -> None:
        self.home.cleanup()

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
