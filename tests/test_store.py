from __future__ import annotations

import hashlib
import json
import os
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from worklease.locking import resource_lock_path
from worklease.models import AcquireRequest, LeaseError, MutationRequest
from worklease.store import LeaseStore


class MutableClock:
    def __init__(self, value: float = 1_000.0) -> None:
        self.value = value

    def __call__(self) -> float:
        return self.value

    def advance(self, seconds: float) -> None:
        self.value += seconds


class StoreTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.home = Path(self.temporary.name) / "state"
        self.clock = MutableClock()
        self.store = LeaseStore(self.home, clock=self.clock)

    @staticmethod
    def acquire_request(
        resource: str, claim_id: str, *, ttl: float = 10.0
    ) -> AcquireRequest:
        return AcquireRequest(
            resource=resource,
            claim_id=claim_id,
            agent_id=f"agent-{claim_id}",
            session_id=f"session-{claim_id}",
            owner_id=f"owner-{claim_id}",
            work_key="implement:item:next",
            ttl=ttl,
        )

    @staticmethod
    def mutation(
        acquired: dict[str, object],
        resource: str,
        operation_id: str,
        *,
        ttl: float = 10.0,
    ) -> MutationRequest:
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        return MutationRequest(
            resource=resource,
            claim_id=str(claim["claimId"]),
            token=str(claim["token"]),
            revision=int(claim["revision"]),
            operation_id=operation_id,
            ttl=ttl,
        )

    def test_empty_xdg_state_home_uses_default(self) -> None:
        previous_work = os.environ.pop("WORKLEASE_HOME", None)
        previous_xdg = os.environ.get("XDG_STATE_HOME")
        try:
            os.environ["XDG_STATE_HOME"] = ""
            self.assertEqual(
                Path.home() / ".local" / "state" / "worklease",
                LeaseStore().home,
            )
        finally:
            if previous_work is not None:
                os.environ["WORKLEASE_HOME"] = previous_work
            if previous_xdg is None:
                os.environ.pop("XDG_STATE_HOME", None)
            else:
                os.environ["XDG_STATE_HOME"] = previous_xdg

    def test_opaque_resource_is_preserved_and_lock_hash_is_internal(self) -> None:
        resource = "  opaque provider/value::?  "
        acquired = self.store.acquire(self.acquire_request(resource, "claim"))
        self.assertEqual(resource, acquired["claim"]["resource"])
        expected = hashlib.sha256(resource.encode("utf-8")).hexdigest()
        self.assertEqual(expected, resource_lock_path(resource, self.home).stem)

    def test_concurrent_acquire_has_one_winner_and_independent_resources_proceed(
        self,
    ) -> None:
        barrier = threading.Barrier(2)

        def contender(claim_id: str) -> tuple[str, object]:
            barrier.wait()
            try:
                return "ok", self.store.acquire(
                    self.acquire_request("shared", claim_id)
                )
            except LeaseError as error:
                return error.reason, error

        with ThreadPoolExecutor(max_workers=2) as executor:
            results = list(executor.map(contender, ("first", "second")))
        self.assertEqual(1, sum(result[0] == "ok" for result in results))
        self.assertEqual(
            1,
            sum(
                result[0] in {"already-claimed", "resource-guarded"}
                for result in results
            ),
        )

        with ThreadPoolExecutor(max_workers=2) as executor:
            independent = list(
                executor.map(
                    lambda item: self.store.acquire(self.acquire_request(item, item)),
                    ("resource-a", "resource-b"),
                )
            )
        self.assertEqual(
            {"resource-a", "resource-b"},
            {entry["claim"]["resource"] for entry in independent},
        )

    def test_expiry_reclaim_replaces_token_and_increases_revision(self) -> None:
        first = self.store.acquire(self.acquire_request("resource", "first", ttl=1))
        self.clock.advance(1.1)
        second = self.store.acquire(self.acquire_request("resource", "second"))
        self.assertTrue(second["reclaimed"])
        self.assertNotEqual(first["claim"]["token"], second["claim"]["token"])
        self.assertGreater(second["claim"]["revision"], first["claim"]["revision"])

    def test_acquire_retry_rejects_any_ttl_change(self) -> None:
        self.store.acquire(self.acquire_request("resource", "claim", ttl=1))
        request = self.acquire_request("resource", "claim", ttl=1.0000001)
        with self.assertRaisesRegex(LeaseError, "claim-id-request-mismatch"):
            self.store.acquire(request)

    def test_stale_owner_cannot_heartbeat_after_reclaim(self) -> None:
        first = self.store.acquire(self.acquire_request("resource", "first", ttl=1))
        old = self.mutation(first, "resource", "old-heartbeat")
        self.clock.advance(1.1)
        self.store.acquire(self.acquire_request("resource", "second"))
        with self.assertRaisesRegex(LeaseError, "stale-claim") as raised:
            self.store.heartbeat(old)
        self.assertEqual("stale-claim", raised.exception.reason)

    def test_heartbeat_retry_is_idempotent_and_rejects_request_mismatch(self) -> None:
        acquired = self.store.acquire(self.acquire_request("resource", "claim"))
        request = self.mutation(acquired, "resource", "heartbeat-1")
        first = self.store.heartbeat(request)
        retry = self.store.heartbeat(request)
        self.assertFalse(first["idempotent"])
        self.assertTrue(retry["idempotent"])
        self.assertEqual(first["claim"]["revision"], retry["claim"]["revision"])
        with self.assertRaisesRegex(LeaseError, "operation-id-request-mismatch"):
            self.store.heartbeat(
                MutationRequest(
                    resource="resource",
                    claim_id=request.claim_id,
                    token=request.token,
                    revision=request.revision,
                    operation_id=request.operation_id,
                    ttl=11,
                )
            )
        with self.assertRaisesRegex(LeaseError, "stale-revision"):
            self.store.heartbeat(
                MutationRequest(
                    resource="resource",
                    claim_id=request.claim_id,
                    token=request.token,
                    revision=int(first["claim"]["revision"]),
                    operation_id=request.operation_id,
                    ttl=request.ttl,
                )
            )

    def test_release_retry_is_idempotent_and_claim_id_cannot_be_reused(self) -> None:
        acquired = self.store.acquire(self.acquire_request("resource", "claim"))
        request = self.mutation(acquired, "resource", "release-1")
        first = self.store.release(request, "checkpoint complete")
        retry = self.store.release(request, "checkpoint complete")
        self.assertFalse(first["idempotent"])
        self.assertTrue(retry["idempotent"])
        with self.assertRaisesRegex(LeaseError, "claim-id-reused"):
            self.store.acquire(self.acquire_request("resource", "claim"))
        next_claim = self.store.acquire(self.acquire_request("resource", "next"))
        self.assertGreater(next_claim["claim"]["revision"], request.revision)
        replay_after_reclaim = self.store.release(request, "checkpoint complete")
        self.assertTrue(replay_after_reclaim["idempotent"])

    def test_list_and_status_redact_tokens(self) -> None:
        acquired = self.store.acquire(self.acquire_request("resource", "claim", ttl=1))
        token = str(acquired["claim"]["token"])
        listed = self.store.list_claims()
        self.assertNotIn("token", listed["claims"][0])
        self.assertNotIn(token, json.dumps(listed))
        status = self.store.status("resource")
        self.assertNotIn("token", status["claim"])
        self.assertNotIn(token, json.dumps(status))
        self.clock.advance(1.1)
        expired = self.store.status("resource")
        self.assertEqual("expired", expired["state"])
        self.assertNotIn("token", expired["claim"])

    def test_legacy_claim_schema_is_migrated_before_read(self) -> None:
        self.home.mkdir(parents=True)
        connection = sqlite3.connect(self.home / "leases.sqlite3")
        connection.executescript(
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
                'legacy', 'claim', 'secret', 1, 'agent', 'session',
                'owner', 'work', 1.0, 1.0, 2_000.0
            );
            """
        )
        connection.close()
        status = LeaseStore(self.home, clock=self.clock).status("legacy")
        self.assertEqual("active", status["state"])
        self.assertNotIn("token", status["claim"])

    def test_invalid_ttl_and_blank_release_reason_do_not_change_state(self) -> None:
        with self.assertRaisesRegex(LeaseError, "invalid-ttl"):
            self.store.acquire(self.acquire_request("resource", "bad", ttl=3601))
        self.assertEqual("free", self.store.status("resource")["state"])
        acquired = self.store.acquire(self.acquire_request("resource", "claim"))
        request = self.mutation(acquired, "resource", "release-invalid")
        with self.assertRaisesRegex(LeaseError, "invalid-release-reason"):
            self.store.release(request, " ")
        self.assertEqual("active", self.store.status("resource")["state"])

    def test_claim_survives_process_exit_and_can_be_reclaimed(self) -> None:
        script = """
from worklease.models import AcquireRequest
from worklease.store import LeaseStore
LeaseStore().acquire(AcquireRequest('crash-resource', 'child', 'agent', 'session', 'owner', 'work', ttl=0.1))
"""
        environment = {
            **os.environ,
            "PYTHONPATH": str(Path(__file__).parents[1] / "src"),
            "WORKLEASE_HOME": str(self.home),
        }
        completed = subprocess.run(
            [sys.executable, "-c", script],
            env=environment,
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(0, completed.returncode, completed.stderr)
        time.sleep(0.2)
        reclaimed = LeaseStore(self.home).acquire(
            self.acquire_request("crash-resource", "parent")
        )
        self.assertTrue(reclaimed["reclaimed"])


if __name__ == "__main__":
    unittest.main()
