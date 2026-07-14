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
from worklease.models import (
    AcquireRequest,
    BundleAcquireRequest,
    LeaseError,
    MutationRequest,
)
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

    def test_state_home_and_files_are_private_with_a_permissive_umask(self) -> None:
        self.home.mkdir(parents=True, mode=0o755)
        self.home.chmod(0o755)
        previous_umask = os.umask(0o022)
        try:
            self.store.acquire(self.acquire_request("private", "claim"))
        finally:
            os.umask(previous_umask)

        self.assertEqual(0o700, self.home.stat().st_mode & 0o777)
        self.assertEqual(0o700, (self.home / "locks").stat().st_mode & 0o777)
        for state_file in (path for path in self.home.rglob("*") if path.is_file()):
            with self.subTest(path=state_file):
                self.assertEqual(0o600, state_file.stat().st_mode & 0o777)

    def test_state_database_symlink_is_rejected(self) -> None:
        self.home.mkdir(parents=True)
        sentinel = Path(self.temporary.name) / "sentinel"
        sentinel.write_text("do not follow\n")
        (self.home / "leases.sqlite3").symlink_to(sentinel)

        with self.assertRaises(OSError):
            self.store.status("resource")

        self.assertEqual("do not follow\n", sentinel.read_text())

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

    def test_checkpoint_renews_replays_and_rejects_stale_owner(self) -> None:
        acquired = self.store.acquire(self.acquire_request("resource", "claim"))
        request = self.mutation(acquired, "resource", "checkpoint-1")
        first = self.store.checkpoint(request, {"step": 1, "items": ["a"]})
        retry = self.store.checkpoint(request, {"step": 1, "items": ["a"]})
        self.assertFalse(first["idempotent"])
        self.assertTrue(retry["idempotent"])
        self.assertEqual(first["claim"]["revision"], retry["claim"]["revision"])
        self.assertEqual({"step": 1, "items": ["a"]}, first["checkpoint"])
        with self.assertRaisesRegex(LeaseError, "operation-id-request-mismatch"):
            self.store.checkpoint(request, {"step": 2})
        status = self.store.status("resource")
        self.assertEqual({"step": 1, "items": ["a"]}, status["claim"]["checkpoint"])

    def test_checkpoint_size_and_expiry_are_rejected_without_changes(self) -> None:
        acquired = self.store.acquire(self.acquire_request("resource", "claim", ttl=1))
        request = self.mutation(acquired, "resource", "checkpoint-1", ttl=1)
        with self.assertRaisesRegex(LeaseError, "checkpoint-too-large"):
            self.store.checkpoint(request, "x" * 9000)
        self.assertIsNone(self.store.status("resource")["claim"]["checkpoint"])
        self.clock.advance(1.1)
        with self.assertRaisesRegex(LeaseError, "claim-expired"):
            self.store.checkpoint(request, {"step": 1})

    def test_checkpoint_survives_clean_release_and_expired_recovery(self) -> None:
        first = self.store.acquire(self.acquire_request("resource", "first", ttl=1))
        checkpoint_request = self.mutation(first, "resource", "checkpoint-1", ttl=1)
        checkpointed = self.store.checkpoint(checkpoint_request, {"offset": 4})
        release_request = MutationRequest(
            resource="resource",
            claim_id=str(checkpointed["claim"]["claimId"]),
            token=str(first["claim"]["token"]),
            revision=int(checkpointed["claim"]["revision"]),
            operation_id="release-1",
        )
        released = self.store.release(release_request, "checkpoint complete")
        self.assertEqual({"offset": 4}, released["checkpoint"])
        handed_off = self.store.acquire(self.acquire_request("resource", "second"))
        self.assertEqual("clean-handoff", handed_off["recovery"])
        self.assertEqual({"offset": 4}, handed_off["checkpoint"])
        checkpointed_again = self.store.checkpoint(
            self.mutation(handed_off, "resource", "checkpoint-2"),
            {"offset": 8},
        )
        self.assertEqual({"offset": 8}, checkpointed_again["checkpoint"])
        self.clock.advance(901)
        recovered = self.store.acquire(self.acquire_request("resource", "third"))
        self.assertEqual("expired-recovery", recovered["recovery"])
        self.assertEqual({"offset": 8}, recovered["checkpoint"])

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

    @staticmethod
    def bundle_request(
        resources: tuple[str, ...],
        claim_id: str,
        *,
        ttl: float = 10.0,
    ) -> BundleAcquireRequest:
        return BundleAcquireRequest(
            resources=resources,
            claim_id=claim_id,
            agent_id=f"agent-{claim_id}",
            session_id=f"session-{claim_id}",
            owner_id=f"owner-{claim_id}",
            work_key="implement:item:bundle",
            ttl=ttl,
        )

    def test_single_acquire_rejects_claim_id_used_by_bundle(self) -> None:
        self.store.acquire_bundle(
            self.bundle_request(("bundle-resource",), "duplicate-id", ttl=1)
        )
        self.clock.advance(1.1)
        with self.assertRaisesRegex(LeaseError, "claim-id-reused"):
            self.store.acquire(self.acquire_request("single-resource", "duplicate-id"))

    def test_bundle_validation_rejects_invalid_shapes_and_bounds(self) -> None:
        for values, reason in (
            ((), "empty-bundle"),
            (("same", "same"), "duplicate-resource"),
            (tuple(f"resource-{index}" for index in range(33)), "bundle-too-large"),
            ("resource", "invalid-bundle"),
            (None, "invalid-bundle"),
        ):
            with (
                self.subTest(values=values),
                self.assertRaisesRegex(LeaseError, reason),
            ):
                BundleAcquireRequest(
                    resources=values,  # type: ignore[arg-type]
                    claim_id="claim",
                    agent_id="agent",
                    session_id="session",
                    owner_id="owner",
                    work_key="work",
                )

    def test_bundle_acquire_is_atomic_and_single_member_mutations_are_rejected(
        self,
    ) -> None:
        acquired = self.store.acquire_bundle(
            self.bundle_request(("resource-a", "resource-b"), "bundle")
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        self.assertEqual(["resource-a", "resource-b"], claim["resources"])
        self.assertEqual(
            claim["claimId"], self.store.status("resource-a")["claim"]["claimId"]
        )
        self.assertEqual(
            claim["revision"], self.store.status("resource-b")["claim"]["revision"]
        )
        self.assertNotIn(str(claim["token"]), json.dumps(self.store.list_claims()))
        with self.assertRaisesRegex(LeaseError, "bundle-operation-required"):
            self.store.heartbeat(
                MutationRequest(
                    resource="resource-a",
                    claim_id=str(claim["claimId"]),
                    token=str(claim["token"]),
                    revision=int(claim["revision"]),
                    operation_id="member-heartbeat",
                )
            )
        with self.assertRaisesRegex(LeaseError, "bundle-operation-required"):
            self.store.acquire(self.acquire_request("resource-a", "other"))
        self.assertEqual(
            str(claim["claimId"]),
            self.store.status("resource-b")["claim"]["claimId"],
        )

    def test_bundle_retry_and_expiry_reclaim_are_idempotent_and_versioned(self) -> None:
        request = self.bundle_request(("resource-a", "resource-b"), "first", ttl=1)
        first = self.store.acquire_bundle(request)
        retry = self.store.acquire_bundle(request)
        self.assertTrue(retry["idempotent"])
        self.assertEqual(first["claim"]["token"], retry["claim"]["token"])
        self.clock.advance(1.1)
        recovered = self.store.acquire_bundle(
            self.bundle_request(("resource-a", "resource-b"), "second")
        )
        self.assertTrue(recovered["reclaimed"])
        self.assertGreater(recovered["claim"]["revision"], first["claim"]["revision"])
        self.assertNotEqual(first["claim"]["token"], recovered["claim"]["token"])

    def test_overlapping_bundles_have_one_winner_without_partial_claims(self) -> None:
        barrier = threading.Barrier(2)

        def contender(claim_id: str) -> tuple[str, object]:
            barrier.wait()
            try:
                return (
                    "ok",
                    self.store.acquire_bundle(
                        self.bundle_request(("shared", claim_id), claim_id)
                    ),
                )
            except LeaseError as error:
                return error.reason, error

        with ThreadPoolExecutor(max_workers=2) as executor:
            results = list(executor.map(contender, ("bundle-a", "bundle-b")))
        self.assertEqual(1, sum(result[0] == "ok" for result in results))
        self.assertEqual(
            1,
            sum(
                result[0] in {"already-claimed", "resource-guarded"}
                for result in results
            ),
        )
        winner = next(result[1] for result in results if result[0] == "ok")
        assert isinstance(winner, dict)
        claim = winner["claim"]
        assert isinstance(claim, dict)
        self.assertEqual("active", self.store.status("shared")["state"])
        self.assertEqual(
            "active", self.store.status(str(claim["resources"][1]))["state"]
        )


if __name__ == "__main__":
    unittest.main()
