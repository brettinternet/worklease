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
    BundleMutationRequest,
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

    def test_inspect_operation_redacts_unknown_and_completed_receipts(self) -> None:
        acquired = self.store.acquire(self.acquire_request("resource", "claim"))
        unknown_request = self.mutation(acquired, "resource", "unknown-1")
        operation_request = {
            "revision": unknown_request.revision,
            "ttl": float(unknown_request.ttl),
            "command": ["printf", "sentinel"],
        }
        self.assertIsNone(
            self.store.begin_operation(unknown_request, "exec", operation_request)
        )

        unknown = self.store.inspect_operation("resource", "unknown-1")
        self.assertEqual("unknown-outcome", unknown["state"])
        self.assertEqual("exec", unknown["kind"])
        self.assertNotIn("token", unknown)
        self.assertNotIn("request", unknown)
        self.assertNotIn("receipt", unknown)
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            raw_request = db.execute(
                """
                SELECT request FROM operations
                WHERE resource = ? AND operation_id = ?
                """,
                ("resource", "unknown-1"),
            ).fetchone()
        assert raw_request is not None
        self.assertEqual(
            hashlib.sha256(str(raw_request[0]).encode("utf-8")).hexdigest(),
            unknown["requestSha256"],
        )

        heartbeat = self.store.heartbeat(
            self.mutation(acquired, "resource", "completed-1")
        )
        completed = self.store.inspect_operation("resource", "completed-1")
        self.assertEqual("completed", completed["state"])
        self.assertEqual(
            heartbeat["claim"]["revision"] - 1, completed["expectedRevision"]
        )
        self.assertNotIn("token", completed)
        self.assertNotIn("receipt", completed)

    def test_inspect_operation_reads_reconciliation_projection_and_migrates_table(
        self,
    ) -> None:
        acquired = self.store.acquire(self.acquire_request("resource", "claim"))
        request = self.mutation(acquired, "resource", "unknown-1")
        self.store.begin_operation(
            request,
            "exec",
            {"revision": request.revision, "ttl": float(request.ttl)},
        )
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            table = db.execute(
                """
                SELECT name FROM sqlite_master
                WHERE type = 'table' AND name = 'reconciliations'
                """
            ).fetchone()
            self.assertEqual(("reconciliations",), table)
            db.execute("ALTER TABLE reconciliations RENAME TO reconciliations_legacy")
            db.execute(
                """
                CREATE TABLE reconciliations(
                    resource TEXT NOT NULL,
                    operation_id TEXT NOT NULL,
                    kind TEXT NOT NULL,
                    claim_id TEXT NOT NULL,
                    outcome TEXT NOT NULL,
                    evidence TEXT NOT NULL,
                    resolver_agent_id TEXT NOT NULL,
                    resolver_session_id TEXT NOT NULL,
                    resolver_owner_id TEXT NOT NULL,
                    resolver_work_key TEXT NOT NULL,
                    request_sha256 TEXT NOT NULL,
                    reconciliation_operation_id TEXT NOT NULL,
                    reconciled_at REAL NOT NULL,
                    PRIMARY KEY(resource, operation_id)
                )
                """
            )
            db.execute(
                """
                INSERT INTO reconciliations(
                    resource, operation_id, kind, claim_id, outcome, evidence,
                    resolver_agent_id, resolver_session_id, resolver_owner_id,
                    resolver_work_key, request_sha256,
                    reconciliation_operation_id, reconciled_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    "resource",
                    "unknown-1",
                    "exec",
                    str(request.claim_id),
                    "observed-success",
                    '{"token":"sentinel"}',
                    "agent",
                    "session",
                    "owner",
                    "work",
                    "a" * 64,
                    "reconcile-1",
                    self.clock(),
                ),
            )
            db.commit()

        projection = self.store.inspect_operation("resource", "unknown-1")
        self.assertEqual("reconciled", projection["state"])
        self.assertEqual("observed-success", projection["outcome"])
        self.assertEqual("reconcile-1", projection["reconciliationOperationId"])
        self.assertNotIn("evidence", projection)
        self.assertNotIn("sentinel", json.dumps(projection))
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            columns = {
                str(row[1]) for row in db.execute("PRAGMA table_info(reconciliations)")
            }
        self.assertIn("receipt", columns)

    def test_reconcile_operation_is_authorized_idempotent_and_append_only(self) -> None:
        acquired = self.store.acquire(self.acquire_request("resource", "claim"))
        target = self.mutation(acquired, "resource", "unknown-1")
        operation_request = {
            "revision": target.revision,
            "ttl": float(target.ttl),
            "command": ["publish", "artifact"],
        }
        self.assertIsNone(self.store.begin_operation(target, "exec", operation_request))
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            raw_request = db.execute(
                """
                SELECT request FROM operations
                WHERE resource = ? AND operation_id = ?
                """,
                ("resource", "unknown-1"),
            ).fetchone()
        assert raw_request is not None
        request_sha256 = hashlib.sha256(str(raw_request[0]).encode("utf-8")).hexdigest()
        reconcile_request = MutationRequest(
            resource=target.resource,
            claim_id=target.claim_id,
            token=target.token,
            revision=target.revision,
            operation_id="reconcile-1",
            ttl=target.ttl,
        )
        with self.assertRaisesRegex(LeaseError, "invalid-reconciliation-outcome"):
            self.store.reconcile_operation(
                reconcile_request,
                "unknown-1",
                request_sha256,
                "bogus",
                {"providerReceipt": "receipt-1"},
            )
        reconcile = self.store.reconcile_operation(
            reconcile_request,
            "unknown-1",
            request_sha256,
            "observed-success",
            {"providerReceipt": "receipt-1"},
        )
        self.assertFalse(reconcile["idempotent"])
        self.assertEqual(2, reconcile["claim"]["revision"])
        self.assertNotIn("token", reconcile["claim"])
        self.assertEqual(
            "reconciled", self.store.inspect_operation("resource", "unknown-1")["state"]
        )

        replay = self.store.reconcile_operation(
            reconcile_request,
            "unknown-1",
            request_sha256,
            "observed-success",
            {"providerReceipt": "receipt-1"},
        )
        self.assertTrue(replay["idempotent"])
        self.assertEqual(reconcile["claim"], replay["claim"])
        with self.assertRaisesRegex(LeaseError, "operation-id-request-mismatch"):
            self.store.reconcile_operation(
                reconcile_request,
                "unknown-1",
                request_sha256,
                "observed-success",
                {"providerReceipt": "changed"},
            )
        advanced = self.store.heartbeat(
            MutationRequest(
                resource=target.resource,
                claim_id=target.claim_id,
                token=target.token,
                revision=reconcile["claim"]["revision"],
                operation_id="heartbeat-after-reconcile",
            )
        )
        self.assertGreater(
            advanced["claim"]["revision"], reconcile["claim"]["revision"]
        )
        with self.assertRaisesRegex(LeaseError, "stale-revision"):
            self.store.reconcile_operation(
                reconcile_request,
                "unknown-1",
                request_sha256,
                "observed-success",
                {"providerReceipt": "receipt-1"},
            )
        self.clock.advance(901)
        with self.assertRaisesRegex(LeaseError, "claim-expired"):
            self.store.reconcile_operation(
                reconcile_request,
                "unknown-1",
                request_sha256,
                "observed-success",
                {"providerReceipt": "receipt-1"},
            )
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            original = db.execute(
                """
                SELECT state, request FROM operations
                WHERE resource = ? AND operation_id = ?
                """,
                ("resource", "unknown-1"),
            ).fetchone()
        self.assertEqual(("started", str(raw_request[0])), original)

    def test_reconcile_rejects_fingerprint_and_malformed_evidence(self) -> None:
        acquired = self.store.acquire(self.acquire_request("resource", "claim"))
        target = self.mutation(acquired, "resource", "unknown-invalid")
        operation_request = {
            "revision": target.revision,
            "argv": ["publish", "artifact"],
        }
        self.assertIsNone(self.store.begin_operation(target, "exec", operation_request))
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            raw_request = db.execute(
                """
                SELECT request FROM operations
                WHERE resource = ? AND operation_id = ?
                """,
                ("resource", "unknown-invalid"),
            ).fetchone()
        assert raw_request is not None
        request_sha256 = hashlib.sha256(str(raw_request[0]).encode("utf-8")).hexdigest()
        reconcile_request = self.mutation(acquired, "resource", "reconcile-invalid")
        with self.assertRaisesRegex(LeaseError, "request-fingerprint-mismatch"):
            self.store.reconcile_operation(
                reconcile_request,
                "unknown-invalid",
                "0" * 64,
                "observed-failure",
                {"provider": "did-not-run"},
            )
        with self.assertRaisesRegex(LeaseError, "invalid-evidence"):
            self.store.reconcile_operation(
                reconcile_request,
                "unknown-invalid",
                request_sha256,
                "observed-failure",
                {"not-json": float("nan")},
            )
        with self.assertRaisesRegex(LeaseError, "invalid-evidence"):
            self.store.reconcile_operation(
                reconcile_request,
                "unknown-invalid",
                request_sha256,
                "observed-failure",
                {"evidence": "x" * 8193},
            )
        self.assertEqual(
            "unknown-outcome",
            self.store.inspect_operation("resource", "unknown-invalid")["state"],
        )

    def test_reconcile_storage_failure_rolls_back_claim_and_audit_record(self) -> None:
        acquired = self.store.acquire(self.acquire_request("resource", "claim"))
        target = self.mutation(acquired, "resource", "unknown-storage")
        self.assertIsNone(
            self.store.begin_operation(
                target,
                "exec",
                {"revision": target.revision, "argv": ["publish", "artifact"]},
            )
        )
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            raw_request = db.execute(
                """
                SELECT request FROM operations
                WHERE resource = ? AND operation_id = ?
                """,
                ("resource", "unknown-storage"),
            ).fetchone()
            assert raw_request is not None
            request_sha256 = hashlib.sha256(
                str(raw_request[0]).encode("utf-8")
            ).hexdigest()
            db.execute(
                """
                CREATE TRIGGER fail_reconciliation_insert
                BEFORE INSERT ON reconciliations
                BEGIN
                    SELECT RAISE(ABORT, 'injected reconciliation failure');
                END
                """
            )
            db.commit()
        reconcile_request = self.mutation(acquired, "resource", "reconcile-storage")
        with self.assertRaisesRegex(
            sqlite3.IntegrityError, "injected reconciliation failure"
        ):
            self.store.reconcile_operation(
                reconcile_request,
                "unknown-storage",
                request_sha256,
                "observed-success",
                {"provider": "observed"},
            )
        status = self.store.status("resource")
        assert isinstance(status["claim"], dict)
        self.assertEqual(int(target.revision), int(status["claim"]["revision"]))
        self.assertEqual(
            "unknown-outcome",
            self.store.inspect_operation("resource", "unknown-storage")["state"],
        )

    def test_inspect_operation_rejects_reused_operation_id_as_ambiguous(self) -> None:
        first = self.store.acquire(self.acquire_request("resource", "first", ttl=1))
        first_request = self.mutation(first, "resource", "reused-1", ttl=1)
        self.store.begin_operation(
            first_request,
            "exec",
            {"revision": first_request.revision, "ttl": float(first_request.ttl)},
        )
        self.clock.advance(1.1)
        second = self.store.acquire(self.acquire_request("resource", "second"))
        second_request = self.mutation(second, "resource", "reused-1")
        self.store.begin_operation(
            second_request,
            "exec",
            {"revision": second_request.revision, "ttl": float(second_request.ttl)},
        )

        with self.assertRaisesRegex(LeaseError, "operation-id-ambiguous"):
            self.store.inspect_operation("resource", "reused-1")

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

    def test_verbose_status_fresh_home_is_read_only(self) -> None:
        fresh_home = self.home / "fresh"
        self.assertFalse(fresh_home.exists())
        diagnostic = LeaseStore(fresh_home).status_verbose("fresh-resource")
        self.assertEqual("free", diagnostic["state"])
        self.assertEqual([], diagnostic["unknownOperations"])
        self.assertFalse(fresh_home.exists())

    def test_verbose_status_is_redacted_deterministic_and_read_only(self) -> None:
        resource = "verbose-resource"
        acquired = self.store.acquire(self.acquire_request(resource, "claim", ttl=1))
        token = str(acquired["claim"]["token"])
        unknown_request = self.mutation(acquired, resource, "unknown-verbose")
        self.assertIsNone(
            self.store.begin_operation(
                unknown_request,
                "exec",
                {
                    "revision": unknown_request.revision,
                    "command": ["printf", "sentinel-request"],
                    "token": token,
                },
            )
        )

        before_active_tree = sorted(
            path.relative_to(self.home).as_posix() for path in self.home.rglob("*")
        )
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            before_active = db.execute(
                """
                SELECT
                    (SELECT COUNT(*) FROM claims),
                    (SELECT COUNT(*) FROM operations),
                    (SELECT COUNT(*) FROM releases)
                """
            ).fetchone()
        active = self.store.status_verbose(resource)
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            after_active = db.execute(
                """
                SELECT
                    (SELECT COUNT(*) FROM claims),
                    (SELECT COUNT(*) FROM operations),
                    (SELECT COUNT(*) FROM releases)
                """
            ).fetchone()
        self.assertEqual(before_active, after_active)
        after_active_tree = sorted(
            path.relative_to(self.home).as_posix() for path in self.home.rglob("*")
        )
        self.assertEqual(before_active_tree, after_active_tree)
        self.assertEqual(1, active["schemaVersion"])
        self.assertEqual("active", active["state"])
        self.assertEqual(
            {
                "resource",
                "claimId",
                "agentId",
                "sessionId",
                "ownerId",
                "workKey",
                "coordinationOnly",
                "revision",
                "acquiredAt",
                "heartbeatAt",
                "expiresAt",
            },
            set(active["claim"]),
        )
        self.assertEqual(
            {
                "operationId",
                "kind",
                "expectedRevision",
                "createdAt",
            },
            set(active["unknownOperations"][0]),
        )
        self.assertEqual(
            "unknown-verbose", active["unknownOperations"][0]["operationId"]
        )
        self.assertEqual("exec", active["unknownOperations"][0]["kind"])
        self.assertIn("authoritative evidence", active["guidance"])
        self.assertNotIn(token, json.dumps(active))
        self.assertNotIn("sentinel-request", json.dumps(active))
        self.assertNotIn("receipt", json.dumps(active))

        released = self.store.release(unknown_request, "diagnostic complete")
        self.assertEqual("diagnostic complete", released["reason"])
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            before_free = db.execute(
                """
                SELECT
                    (SELECT COUNT(*) FROM claims),
                    (SELECT COUNT(*) FROM operations),
                    (SELECT COUNT(*) FROM releases)
                """
            ).fetchone()
        free = self.store.status_verbose(resource)
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            after_free = db.execute(
                """
                SELECT
                    (SELECT COUNT(*) FROM claims),
                    (SELECT COUNT(*) FROM operations),
                    (SELECT COUNT(*) FROM releases)
                """
            ).fetchone()
        self.assertEqual(before_free, after_free)
        self.assertEqual("free", free["state"])
        self.assertIsNone(free["claim"])
        self.assertEqual(
            {
                "claimId",
                "operationId",
                "revision",
                "releasedAt",
            },
            set(free["release"]),
        )
        self.assertEqual("unknown-verbose", free["unknownOperations"][0]["operationId"])

    def test_verbose_status_reports_expired_claim_without_checkpoint_or_token(
        self,
    ) -> None:
        acquired = self.store.acquire(self.acquire_request("expired", "claim", ttl=1))
        token = str(acquired["claim"]["token"])
        self.clock.advance(1.1)
        diagnostic = self.store.status_verbose("expired")
        self.assertEqual(1, diagnostic["schemaVersion"])
        self.assertEqual("expired", diagnostic["state"])
        self.assertEqual(False, diagnostic["claim"]["coordinationOnly"])
        self.assertNotIn("checkpoint", diagnostic["claim"])
        self.assertNotIn(token, json.dumps(diagnostic))

    def test_verbose_status_keeps_reused_operation_id_unknown(self) -> None:
        first = self.store.acquire(self.acquire_request("reused", "first", ttl=1))
        first_request = self.mutation(first, "reused", "same-operation", ttl=1)
        self.assertIsNone(
            self.store.begin_operation(
                first_request,
                "exec",
                {"revision": first_request.revision, "attempt": 1},
            )
        )
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            raw_request = db.execute(
                """
                SELECT request
                FROM operations
                WHERE resource = ? AND claim_id = ? AND operation_id = ?
                """,
                ("reused", first_request.claim_id, first_request.operation_id),
            ).fetchone()
        assert raw_request is not None
        request_sha256 = hashlib.sha256(str(raw_request[0]).encode("utf-8")).hexdigest()
        reconcile_request = self.mutation(
            first,
            "reused",
            "reconcile-same-operation",
            ttl=1,
        )
        self.store.reconcile_operation(
            reconcile_request,
            "same-operation",
            request_sha256,
            "observed-success",
            {"provider": "observed"},
        )

        self.clock.advance(1.1)
        second = self.store.acquire(self.acquire_request("reused", "second"))
        second_request = self.mutation(second, "reused", "same-operation")
        self.assertIsNone(
            self.store.begin_operation(
                second_request,
                "exec",
                {"revision": second_request.revision, "attempt": 2},
            )
        )

        diagnostic = self.store.status_verbose("reused")
        unknown_operations = diagnostic["unknownOperations"]
        assert isinstance(unknown_operations, list)
        self.assertEqual(
            ["same-operation"],
            [operation["operationId"] for operation in unknown_operations],
        )

    def test_verbose_status_matches_reconciliation_by_request_fingerprint(
        self,
    ) -> None:
        first = self.store.acquire(self.acquire_request("cross-claim", "first", ttl=1))
        first_request = self.mutation(first, "cross-claim", "cross-operation", ttl=1)
        self.assertIsNone(
            self.store.begin_operation(
                first_request,
                "exec",
                {"revision": first_request.revision, "attempt": 1},
            )
        )
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            raw_request = db.execute(
                """
                SELECT request
                FROM operations
                WHERE resource = ? AND claim_id = ? AND operation_id = ?
                """,
                ("cross-claim", first_request.claim_id, first_request.operation_id),
            ).fetchone()
        assert raw_request is not None
        request_sha256 = hashlib.sha256(str(raw_request[0]).encode("utf-8")).hexdigest()

        self.clock.advance(1.1)
        second = self.store.acquire(
            self.acquire_request("cross-claim", "second", ttl=1)
        )
        resolver_request = self.mutation(
            second,
            "cross-claim",
            "reconcile-cross-operation",
            ttl=1,
        )
        self.store.reconcile_operation(
            resolver_request,
            "cross-operation",
            request_sha256,
            "observed-success",
            {"provider": "observed"},
        )

        diagnostic = self.store.status_verbose("cross-claim")
        self.assertEqual([], diagnostic["unknownOperations"])

    def test_verbose_status_matches_reconciliation_kind(self) -> None:
        acquired = self.store.acquire(
            self.acquire_request("kind-match", "claim", ttl=60)
        )
        request = self.mutation(acquired, "kind-match", "same-operation")
        operation_request = {"revision": request.revision, "attempt": 1}
        self.assertIsNone(
            self.store.begin_operation(request, "exec", operation_request)
        )
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            raw_request = db.execute(
                """
                SELECT request
                FROM operations
                WHERE resource = ? AND claim_id = ? AND operation_id = ? AND kind = ?
                """,
                ("kind-match", request.claim_id, request.operation_id, "exec"),
            ).fetchone()
            assert raw_request is not None
            request_sha256 = hashlib.sha256(
                str(raw_request[0]).encode("utf-8")
            ).hexdigest()
            db.execute(
                """
                INSERT INTO operations(
                    resource, claim_id, operation_id, kind, state, request,
                    expected_revision, receipt, created_at
                ) VALUES (?, ?, ?, ?, 'started', ?, ?, ?, ?)
                """,
                (
                    "kind-match",
                    request.claim_id,
                    request.operation_id,
                    "replace-file",
                    raw_request[0],
                    request.revision,
                    "{}",
                    self.clock(),
                ),
            )
            claim = acquired["claim"]
            assert isinstance(claim, dict)
            db.execute(
                """
                INSERT INTO reconciliations(
                    resource, operation_id, kind, claim_id, outcome, evidence,
                    resolver_agent_id, resolver_session_id, resolver_owner_id,
                    resolver_work_key, request_sha256,
                    reconciliation_operation_id, reconciled_at, receipt
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    "kind-match",
                    "same-operation",
                    "exec",
                    request.claim_id,
                    "observed-success",
                    "{}",
                    str(claim["agentId"]),
                    str(claim["sessionId"]),
                    str(claim["ownerId"]),
                    str(claim["workKey"]),
                    request_sha256,
                    "manual-reconcile",
                    self.clock(),
                    "{}",
                ),
            )
            db.commit()

        diagnostic = self.store.status_verbose("kind-match")
        unknown_operations = diagnostic["unknownOperations"]
        assert isinstance(unknown_operations, list)
        self.assertEqual(
            [{"operationId": "same-operation", "kind": "replace-file"}],
            [
                {
                    "operationId": operation["operationId"],
                    "kind": operation["kind"],
                }
                for operation in unknown_operations
            ],
        )

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

    def bundle_mutation(
        self,
        acquired: dict[str, object],
        resources: tuple[str, ...],
        operation_id: str,
        *,
        ttl: float = 10.0,
    ) -> BundleMutationRequest:
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        return BundleMutationRequest(
            resources=resources,
            claim_id=str(claim["claimId"]),
            token=str(claim["token"]),
            revision=int(claim["revision"]),
            operation_id=operation_id,
            ttl=ttl,
        )

    def test_bundle_status_heartbeat_and_release_are_atomic_and_idempotent(
        self,
    ) -> None:
        resources = ("resource-a", "resource-b")
        acquired = self.store.acquire_bundle(self.bundle_request(resources, "bundle"))
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        token = str(claim["token"])
        status = self.store.bundle_status(resources)
        self.assertEqual("active", status["state"])
        self.assertNotIn(token, json.dumps(status))

        heartbeat_request = self.bundle_mutation(
            acquired, resources, "bundle-heartbeat"
        )
        heartbeat = self.store.heartbeat_bundle(heartbeat_request)
        retry = self.store.heartbeat_bundle(heartbeat_request)
        self.assertFalse(heartbeat["idempotent"])
        self.assertTrue(retry["idempotent"])
        self.assertEqual(heartbeat["claim"]["revision"], retry["claim"]["revision"])
        self.assertEqual(
            heartbeat["claim"]["revision"],
            self.store.status("resource-b")["claim"]["revision"],
        )
        with self.assertRaisesRegex(LeaseError, "bundle-membership-mismatch"):
            self.store.bundle_status(("resource-a",))

        release_request = self.bundle_mutation(heartbeat, resources, "bundle-release")
        released = self.store.release_bundle(release_request, "bundle complete")
        replay = self.store.release_bundle(release_request, "bundle complete")
        self.assertFalse(released["idempotent"])
        self.assertTrue(replay["idempotent"])
        self.assertEqual("free", self.store.bundle_status(resources)["state"])
        self.assertNotIn(token, json.dumps(released))

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

    def test_overlapping_bundles_across_processes_have_one_winner(self) -> None:
        self.store.status("process-schema")
        script = """
import json
import os
import time
from pathlib import Path

from worklease.models import BundleAcquireRequest, LeaseError
from worklease.store import LeaseStore

claim_id = os.environ["BUNDLE_CLAIM_ID"]
marker_dir = Path(os.environ["BUNDLE_MARKER_DIR"])
marker_dir.mkdir(parents=True, exist_ok=True)
(marker_dir / f"ready-{claim_id}").touch()
held_marker = marker_dir / "held"
release_marker = marker_dir / "release"
request = BundleAcquireRequest(
    resources=("process-shared", f"process-{claim_id}"),
    claim_id=claim_id,
    agent_id=f"agent-{claim_id}",
    session_id=f"session-{claim_id}",
    owner_id=f"owner-{claim_id}",
    work_key="implement:process:bundle",
)

def held_clock() -> float:
    if not held_marker.exists():
        held_marker.touch()
        while not release_marker.exists():
            time.sleep(0.01)
    return time.time()

try:
    result = LeaseStore(clock=held_clock if claim_id == "process-a" else time.time).acquire_bundle(request)
except LeaseError as error:
    print(json.dumps({"result": error.reason}))
else:
    print(json.dumps({"result": "ok", "claim": result["claim"]}))
"""
        marker_dir = self.home / "process-markers"
        marker_dir.mkdir(parents=True)
        environment = {
            **os.environ,
            "PYTHONPATH": str(Path(__file__).parents[1] / "src"),
            "WORKLEASE_HOME": str(self.home),
            "BUNDLE_MARKER_DIR": str(marker_dir),
        }

        def run_contender(claim_id: str) -> dict[str, object]:
            completed = subprocess.run(
                [sys.executable, "-c", script],
                env={**environment, "BUNDLE_CLAIM_ID": claim_id},
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(0, completed.returncode, completed.stderr)
            decoded = json.loads(completed.stdout)
            assert isinstance(decoded, dict)
            return decoded

        with ThreadPoolExecutor(max_workers=2) as executor:
            future_a = executor.submit(run_contender, "process-a")
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline and not (marker_dir / "held").exists():
                time.sleep(0.01)
            self.assertTrue((marker_dir / "held").exists())

            future_b = executor.submit(run_contender, "process-b")
            deadline = time.monotonic() + 5
            while (
                time.monotonic() < deadline
                and not (marker_dir / "ready-process-b").exists()
            ):
                time.sleep(0.01)
            self.assertTrue((marker_dir / "ready-process-b").exists())
            (marker_dir / "release").touch()
            results = [future_a.result(), future_b.result()]

        self.assertEqual(1, sum(result["result"] == "ok" for result in results))
        self.assertEqual(
            1,
            sum(
                result["result"] in {"already-claimed", "resource-guarded"}
                for result in results
            ),
        )
        self.assertEqual("active", self.store.status("process-shared")["state"])

    def test_bundle_stale_owner_cannot_mutate_after_expiry_reclaim(self) -> None:
        resources = ("stale-a", "stale-b")
        first = self.store.acquire_bundle(
            self.bundle_request(resources, "stale-first", ttl=1)
        )
        old_request = self.bundle_mutation(first, resources, "stale-heartbeat")
        self.clock.advance(1.1)
        recovered = self.store.acquire_bundle(
            self.bundle_request(resources, "stale-second")
        )

        with self.assertRaisesRegex(LeaseError, "stale-claim"):
            self.store.heartbeat_bundle(old_request)
        recovered_claim = recovered["claim"]
        assert isinstance(recovered_claim, dict)
        for resource in resources:
            self.assertEqual(
                recovered_claim["claimId"],
                self.store.status(resource)["claim"]["claimId"],
            )

    def test_bundle_changed_operation_replay_is_rejected_without_revision_change(
        self,
    ) -> None:
        resources = ("replay-a", "replay-b")
        acquired = self.store.acquire_bundle(self.bundle_request(resources, "replay"))
        request = self.bundle_mutation(acquired, resources, "changed-replay", ttl=2)
        heartbeat = self.store.heartbeat_bundle(request)
        claim = heartbeat["claim"]
        assert isinstance(claim, dict)
        changed = BundleMutationRequest(
            resources=resources,
            claim_id=request.claim_id,
            token=request.token,
            revision=request.revision,
            operation_id=request.operation_id,
            ttl=3,
        )

        with self.assertRaisesRegex(LeaseError, "operation-id-request-mismatch"):
            self.store.heartbeat_bundle(changed)
        self.assertEqual(
            claim["revision"],
            self.store.bundle_status(resources)["claim"]["revision"],
        )

    def test_bundle_acquire_rolls_back_after_partial_member_failure(self) -> None:
        self.store.status("failure-schema")
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            db.execute(
                """
                CREATE TRIGGER fail_bundle_member
                BEFORE INSERT ON claims
                WHEN NEW.resource = 'bundle-failure-b'
                BEGIN
                    SELECT RAISE(ABORT, 'injected bundle failure');
                END
                """
            )

        resources = ("bundle-failure-a", "bundle-failure-b")
        with self.assertRaisesRegex(sqlite3.IntegrityError, "injected bundle failure"):
            self.store.acquire_bundle(self.bundle_request(resources, "partial"))

        self.assertEqual("free", self.store.bundle_status(resources)["state"])
        with sqlite3.connect(self.home / "leases.sqlite3") as db:
            for table in ("bundle_epochs", "bundles", "bundle_members", "claims"):
                self.assertEqual(
                    0, db.execute(f"SELECT COUNT(*) FROM {table}").fetchone()[0]
                )


if __name__ == "__main__":
    unittest.main()
