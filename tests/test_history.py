from __future__ import annotations

import hashlib
import json
import os
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from contextlib import closing
from pathlib import Path
from typing import Any
from unittest.mock import patch

import worklease.projections
from worklease.models import (
    AcquireRequest,
    BundleAcquireRequest,
    MutationRequest,
    TransferRequest,
)
from worklease.store import LeaseStore


class HistoryProjectionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.home = Path(self.temporary.name) / "state"
        self.now = 1_000.0
        self.store = LeaseStore(self.home, clock=lambda: self.now)

    def _acquire(
        self, resource: str, claim_id: str, *, ttl: float = 900.0
    ) -> dict[str, Any]:
        result = self.store.acquire(
            AcquireRequest(
                resource=resource,
                claim_id=claim_id,
                agent_id=f"agent-{claim_id}",
                session_id=f"session-{claim_id}",
                owner_id=f"owner-{claim_id}",
                work_key=f"work-{claim_id}",
                ttl=ttl,
            )
        )
        claim = result["claim"]
        assert isinstance(claim, dict)
        return claim

    @staticmethod
    def _mutation(
        resource: str, claim: dict[str, Any], operation_id: str
    ) -> MutationRequest:
        return MutationRequest(
            resource=resource,
            claim_id=str(claim["claimId"]),
            token=str(claim["token"]),
            revision=int(claim["revision"]),
            operation_id=operation_id,
        )

    def _ensure_database(self) -> None:
        self.store.status("history-schema-bootstrap")

    def _insert_epoch(
        self,
        claim_id: str,
        resource: str,
        acquired_at: float,
        acquisition_revision: int | None,
    ) -> None:
        self._ensure_database()
        with closing(sqlite3.connect(self.home / "leases.sqlite3")) as db, db:
            db.execute(
                """
                INSERT INTO epochs(
                    claim_id, resource, agent_id, session_id, owner_id,
                    work_key, acquired_at, acquisition_revision
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    claim_id,
                    resource,
                    f"agent-{claim_id}",
                    f"session-{claim_id}",
                    f"owner-{claim_id}",
                    f"work-{claim_id}",
                    acquired_at,
                    acquisition_revision,
                ),
            )

    def _insert_termination(
        self,
        resource: str,
        claim_id: str,
        *,
        recorded_at: float,
        reason: str = "released",
    ) -> None:
        self._ensure_database()
        with closing(sqlite3.connect(self.home / "leases.sqlite3")) as db, db:
            db.execute(
                """
                INSERT INTO epoch_terminations(
                    resource, claim_id, reason, effective_at, recorded_at,
                    final_revision, heartbeat_at, expires_at, checkpoint,
                    successor_claim_id, operation_id
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    resource,
                    claim_id,
                    reason,
                    recorded_at,
                    recorded_at,
                    1,
                    recorded_at,
                    recorded_at + 900,
                    None,
                    None,
                    None,
                ),
            )

    def _insert_operation(
        self,
        resource: str,
        claim_id: str,
        operation_id: str,
        kind: str,
        state: str,
        expected_revision: int,
        created_at: float,
        request: dict[str, Any] | None = None,
        receipt: dict[str, Any] | None = None,
    ) -> None:
        self._ensure_database()
        with closing(sqlite3.connect(self.home / "leases.sqlite3")) as db, db:
            db.execute(
                """
                INSERT INTO operations(
                    resource, claim_id, operation_id, kind, state, request,
                    expected_revision, receipt, created_at
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    resource,
                    claim_id,
                    operation_id,
                    kind,
                    state,
                    json.dumps(request or {}, sort_keys=True, separators=(",", ":")),
                    expected_revision,
                    json.dumps(receipt or {}, sort_keys=True, separators=(",", ":")),
                    created_at,
                ),
            )

    def test_exact_singleton_and_bundle_member_histories(self) -> None:
        first = self._acquire("exact", "singleton-first", ttl=1)
        self.now += 2

        # Reading an expired row does not reclaim it or synthesize a termination.
        expired = self.store.history("exact")
        expired_epoch = expired["epochs"][0]
        self.assertEqual("open", expired_epoch["completeness"])
        self.assertIsNone(expired_epoch["termination"])
        self.assertEqual("singleton-first", expired_epoch["currentClaim"]["claimId"])
        with closing(sqlite3.connect(self.home / "leases.sqlite3")) as db:
            self.assertEqual(
                0,
                db.execute(
                    "SELECT COUNT(*) FROM epoch_terminations "
                    "WHERE resource = ? AND claim_id = ?",
                    ("exact", first["claimId"]),
                ).fetchone()[0],
            )

        second = self._acquire("exact", "singleton-second")
        self.store.release(self._mutation("exact", second, "release-second"), "done")
        bundle = self.store.acquire_bundle(
            BundleAcquireRequest(
                resources=("exact", "other"),
                claim_id="bundle-claim",
                agent_id="bundle-agent",
                session_id="bundle-session",
                owner_id="bundle-owner",
                work_key="bundle-work",
            )
        )
        bundle_claim = bundle["claim"]
        assert isinstance(bundle_claim, dict)

        exact = self.store.history("exact")
        other = self.store.history("other")
        self.assertEqual("exact", exact["resource"])
        self.assertEqual(
            ["singleton-first", "singleton-second", "bundle-claim"],
            [epoch["claimId"] for epoch in exact["epochs"]],
        )
        self.assertEqual(
            ["singleton", "singleton", "bundle"],
            [epoch["kind"] for epoch in exact["epochs"]],
        )
        self.assertEqual(
            ["bundle-claim"], [epoch["claimId"] for epoch in other["epochs"]]
        )
        bundle_epoch = exact["epochs"][-1]
        self.assertEqual(["exact", "other"], bundle_epoch["resources"])
        self.assertEqual([], bundle_epoch["operations"])
        self.assertNotIn("acquire", [op["kind"] for op in bundle_epoch["operations"]])
        self.assertEqual("open", bundle_epoch["completeness"])
        self.assertEqual(
            bundle_claim["claimId"], bundle_epoch["currentClaim"]["claimId"]
        )
        self.assertEqual("current-claim", bundle_epoch["currentClaim"]["source"])
        self.assertEqual(["exact", "other"], bundle_epoch["currentClaim"]["resources"])

    def test_termination_reasons_and_stored_snapshots(self) -> None:
        first = self._acquire("reasons", "first")
        transferred = self.store.transfer(
            TransferRequest(
                resource="reasons",
                claim_id=str(first["claimId"]),
                token=str(first["token"]),
                revision=int(first["revision"]),
                operation_id="transfer-first",
                successor_claim_id="second",
                successor_agent_id="agent-second",
                successor_session_id="session-second",
                successor_owner_id="owner-second",
                successor_work_key="work-second",
            )
        )
        second = transferred["claim"]
        assert isinstance(second, dict)
        self.store.release(self._mutation("reasons", second, "release-second"), "done")
        self._acquire("reasons", "third", ttl=1)
        self.now += 2
        fourth = self._acquire("reasons", "fourth")

        history = self.store.history("reasons")
        epochs = history["epochs"]
        self.assertEqual(
            ["transferred", "released", "expired"],
            [epoch["termination"]["reason"] for epoch in epochs[:3]],
        )
        self.assertTrue(
            all(epoch["termination"]["source"] == "termination" for epoch in epochs[:3])
        )
        self.assertEqual(
            ["second", None, "fourth"],
            [epoch["termination"]["successorClaimId"] for epoch in epochs[:3]],
        )
        self.assertEqual("transfer-first", epochs[0]["termination"]["operationId"])
        self.assertEqual("release-second", epochs[1]["termination"]["operationId"])
        self.assertEqual("open", epochs[3]["completeness"])
        self.assertIsNone(epochs[3]["termination"])
        self.assertEqual(fourth["claimId"], epochs[3]["currentClaim"]["claimId"])
        self.assertEqual("1970-01-01T00:16:41Z", epochs[2]["termination"]["expiresAt"])

    def test_all_explicit_operation_kinds_and_states_are_projected_for_bundle_members(
        self,
    ) -> None:
        self._ensure_database()
        with closing(sqlite3.connect(self.home / "leases.sqlite3")) as db, db:
            db.execute(
                """
                INSERT INTO bundle_epochs(
                    claim_id, resources, agent_id, session_id, owner_id,
                    work_key, acquired_at, acquisition_revision
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    "bundle-history",
                    '["member-a","member-b"]',
                    "agent",
                    "session",
                    "owner",
                    "work",
                    100,
                    4,
                ),
            )
        self._insert_epoch("singleton-history", "single-member", 90, 3)
        singleton_operation_rows = (
            ("heartbeat", "completed"),
            ("checkpoint", "started"),
            ("exec", "failed"),
            ("replace-file", "completed"),
            ("transfer", "started"),
        )
        for index, (kind, state) in enumerate(singleton_operation_rows):
            self._insert_operation(
                "single-member",
                "singleton-history",
                f"singleton-op-{index}",
                kind,
                state,
                index + 1,
                150 + index,
            )
        operation_rows = (
            ("heartbeat-bundle", "completed"),
            ("exec-bundle", "started"),
            ("release-bundle", "failed"),
            ("provider-specific-kind", "completed"),
        )
        for index, (kind, state) in enumerate(operation_rows):
            self._insert_operation(
                '["member-a","member-b"]',
                "bundle-history",
                f"bundle-op-{index}",
                kind,
                state,
                index + 1,
                200 + index,
                request={"argv": ["argv-secret"], "providerPayload": "provider-secret"},
                receipt={"stdout": "stdout-secret", "stderr": "stderr-secret"},
            )
        self._insert_operation(
            "unrelated",
            "unrelated-claim",
            "unrelated-op",
            "exec",
            "completed",
            1,
            1,
        )

        singleton = self.store.history("single-member")
        self.assertEqual(
            [(kind, state) for kind, state in singleton_operation_rows],
            [(op["kind"], op["state"]) for op in singleton["epochs"][0]["operations"]],
        )
        self.assertTrue(
            all(
                operation["source"] == "operation"
                for operation in singleton["epochs"][0]["operations"]
            )
        )
        first = self.store.history("member-a")
        second = self.store.history("member-b")
        for history in (first, second):
            epoch = history["epochs"][0]
            self.assertEqual("bundle", epoch["kind"])
            self.assertEqual(["member-a", "member-b"], epoch["resources"])
            self.assertEqual(
                [(kind, state) for kind, state in operation_rows],
                [(op["kind"], op["state"]) for op in epoch["operations"]],
            )
            self.assertNotIn(
                "exec-heartbeat", [op["kind"] for op in epoch["operations"]]
            )
        first_epoch = {
            key: value for key, value in first["epochs"][0].items() if key != "resource"
        }
        second_epoch = {
            key: value
            for key, value in second["epochs"][0].items()
            if key != "resource"
        }
        self.assertEqual(first_epoch, second_epoch)
        self.assertEqual("member-a", first["resource"])
        self.assertEqual("member-b", second["resource"])

    def test_reconciliation_is_attributed_to_resolver_and_never_exports_evidence(
        self,
    ) -> None:
        old = self._acquire("reconcile", "old")
        checkpoint = self.store.checkpoint(
            self._mutation("reconcile", old, "checkpoint-old"),
            {"checkpoint-secret": "checkpoint-body-secret"},
        )
        old_request = self._mutation("reconcile", checkpoint["claim"], "target-op")
        operation_request = old_request.request_dict(
            argv=["argv-secret"],
            stdout="stdout-secret",
            stderr="stderr-secret",
            fileContents="file-content-secret",
            providerPayload="provider-payload-secret",
            tokenHash="token-hash-secret",
            requestSecret="request-body-secret",
        )
        self.assertIsNone(
            self.store.begin_operation(old_request, "exec", operation_request)
        )
        request_hash = hashlib.sha256(
            json.dumps(operation_request, sort_keys=True, separators=(",", ":")).encode(
                "utf-8"
            )
        ).hexdigest()
        with closing(sqlite3.connect(self.home / "leases.sqlite3")) as db, db:
            db.execute(
                "UPDATE operations SET receipt = ? WHERE resource = ? AND operation_id = ?",
                (
                    json.dumps({"receiptSecret": "receipt-body-secret"}),
                    "reconcile",
                    "target-op",
                ),
            )
        self.store.release(
            self._mutation("reconcile", checkpoint["claim"], "release-old"), "done"
        )
        resolver = self._acquire("reconcile", "resolver")
        reconciliation = self.store.reconcile_operation(
            self._mutation("reconcile", resolver, "resolve-op"),
            "target-op",
            request_hash,
            "observed-success",
            {
                "evidence": "reconciliation-evidence-secret",
                "providerReceipt": "provider-receipt-secret",
            },
        )
        self.assertEqual("observed-success", reconciliation["outcome"])

        history = self.store.history("reconcile")
        old_epoch, resolver_epoch = history["epochs"]
        self.assertEqual([], old_epoch["reconciliations"])
        self.assertEqual(
            "reconciliation", resolver_epoch["reconciliations"][0]["source"]
        )
        self.assertEqual("old", resolver_epoch["reconciliations"][0]["targetClaimId"])
        self.assertEqual(
            "target-op", resolver_epoch["reconciliations"][0]["targetOperationId"]
        )
        self.assertEqual(
            "resolve-op",
            resolver_epoch["reconciliations"][0]["reconciliationOperationId"],
        )
        self.assertEqual(
            "observed-success", resolver_epoch["reconciliations"][0]["outcome"]
        )
        self.assertNotIn("outcome", old_epoch["operations"][1])
        self.assertEqual("complete", old_epoch["completeness"])
        self.assertTrue(old_epoch["termination"]["checkpointPresent"])

        rendered = json.dumps(history, sort_keys=True)
        for secret in (
            str(old["token"]),
            "token-hash-secret",
            "checkpoint-body-secret",
            "argv-secret",
            "stdout-secret",
            "stderr-secret",
            "file-content-secret",
            "provider-payload-secret",
            "request-body-secret",
            "receipt-body-secret",
            "reconciliation-evidence-secret",
            "provider-receipt-secret",
        ):
            self.assertNotIn(secret, rendered)
        forbidden_keys = {
            "token",
            "tokenHash",
            "checkpoint",
            "request",
            "receipt",
            "evidence",
            "argv",
            "stdout",
            "stderr",
            "fileContents",
            "providerPayload",
        }

        def keys(value: object) -> list[str]:
            if isinstance(value, dict):
                return [str(key) for key in value] + [
                    item for item in value.values() for item in keys(item)
                ]
            if isinstance(value, list):
                return [item for value_item in value for item in keys(value_item)]
            return []

        self.assertTrue(forbidden_keys.isdisjoint(keys(history)))

    def test_revision_first_ordering_stable_ties_and_legacy_gaps(self) -> None:
        self._insert_epoch("revision-two", "ordered", 10, 2)
        self._insert_epoch("revision-one", "ordered", 20, 1)
        self._insert_epoch("legacy-b", "ordered", 30, None)
        self._insert_epoch("legacy-a", "ordered", 20, None)
        self._insert_termination("ordered", "revision-two", recorded_at=40)
        self._insert_termination("ordered", "revision-one", recorded_at=40)
        self._insert_operation(
            "ordered",
            "revision-one",
            "operation-z",
            "custom-z",
            "completed",
            3,
            50,
        )
        self._insert_operation(
            "ordered",
            "revision-one",
            "operation-a",
            "custom-a",
            "started",
            3,
            50,
        )
        self._insert_operation(
            "ordered",
            "revision-one",
            "operation-revision-four",
            "custom-four",
            "completed",
            4,
            1,
        )

        # A history read is independent of the authority clock and does not
        # migrate or write the database.
        clockless = LeaseStore(
            self.home,
            clock=lambda: (_ for _ in ()).throw(AssertionError("clock read")),
        )
        history = clockless.history("ordered")
        self.assertEqual(
            ["revision-one", "revision-two", "legacy-a", "legacy-b"],
            [epoch["claimId"] for epoch in history["epochs"]],
        )
        self.assertEqual(
            ["operation-a", "operation-z", "operation-revision-four"],
            [op["operationId"] for op in history["epochs"][0]["operations"]],
        )
        self.assertEqual(
            ["complete", "complete", "legacy-incomplete", "legacy-incomplete"],
            [epoch["completeness"] for epoch in history["epochs"]],
        )
        self.assertEqual(
            ["epoch"] * 4,
            [epoch["source"] for epoch in history["epochs"]],
        )
        self.assertEqual(2, history["coverage"]["legacyIncompleteCount"])
        self.assertEqual(1, history["coverage"]["earliestRetainedAcquisitionRevision"])
        self.assertIsNone(history["coverage"]["resourceRevisionWatermark"])

    def test_projection_never_reads_secret_blob_columns_or_unrelated_rows(self) -> None:
        claim = self._acquire("guarded", "guarded-claim")
        self._insert_operation(
            "guarded",
            str(claim["claimId"]),
            "guarded-op",
            "exec",
            "completed",
            1,
            1,
            request={"secret": "request-secret"},
            receipt={"secret": "receipt-secret"},
        )
        self._insert_operation(
            "unrelated",
            "unrelated-claim",
            "large-unrelated-op",
            "exec",
            "completed",
            1,
            1,
            request={"secret": "x" * 1_000_000},
            receipt={"secret": "y" * 1_000_000},
        )

        original_connect = worklease.projections.connect_readonly
        forbidden_columns = {
            "token",
            "request",
            "receipt",
            "evidence",
            "request_sha256",
        }

        def guarded_connect(home: Path) -> sqlite3.Connection:
            connection = original_connect(home)

            def authorize(
                action: int,
                _table: str | None,
                column: str | None,
                _database: str | None,
                _trigger: str | None,
            ) -> int:
                if action == sqlite3.SQLITE_READ and column in forbidden_columns:
                    return sqlite3.SQLITE_DENY
                return sqlite3.SQLITE_OK

            connection.set_authorizer(authorize)
            return connection

        with patch("worklease.projections.connect_readonly", guarded_connect):
            history = self.store.history("guarded")
        self.assertEqual("guarded-claim", history["epochs"][0]["claimId"])
        self.assertEqual(
            "guarded-op", history["epochs"][0]["operations"][0]["operationId"]
        )

    def test_history_provenance_completeness_and_coverage_metadata(self) -> None:
        self._insert_epoch("legacy", "coverage", 10, None)
        self._insert_epoch("known", "coverage", 20, 5)
        self._insert_termination("coverage", "known", recorded_at=30)
        self._ensure_database()
        with closing(sqlite3.connect(self.home / "leases.sqlite3")) as db, db:
            db.execute(
                "INSERT INTO resources(resource, revision) VALUES (?, ?)",
                ("coverage", 9),
            )

        history = self.store.history("coverage")
        self.assertEqual(5, history["coverage"]["earliestRetainedAcquisitionRevision"])
        self.assertEqual(9, history["coverage"]["resourceRevisionWatermark"])
        self.assertEqual(1, history["coverage"]["legacyIncompleteCount"])
        epochs_by_claim = {epoch["claimId"]: epoch for epoch in history["epochs"]}
        legacy = epochs_by_claim["legacy"]
        complete = epochs_by_claim["known"]
        self.assertEqual("epoch", legacy["source"])
        self.assertEqual("legacy-incomplete", legacy["completeness"])
        self.assertIsNone(legacy["termination"])
        self.assertIsNone(legacy["currentClaim"])
        self.assertEqual("complete", complete["completeness"])
        self.assertEqual("termination", complete["termination"]["source"])

        current = self._acquire("current-source", "current-claim")
        current_history = self.store.history("current-source")
        current_epoch = current_history["epochs"][0]
        self.assertEqual("open", current_epoch["completeness"])
        self.assertEqual("current-claim", current_epoch["currentClaim"]["source"])
        self.assertEqual(current["claimId"], current_epoch["currentClaim"]["claimId"])

    def test_migrated_current_and_legacy_without_end_have_unknown_upper_bound(
        self,
    ) -> None:
        current = self._acquire("migrated-current", "migrated-claim")
        self._ensure_database()
        with closing(sqlite3.connect(self.home / "leases.sqlite3")) as db, db:
            db.execute(
                "UPDATE epochs SET acquisition_revision = NULL WHERE claim_id = ?",
                (current["claimId"],),
            )
        migrated = self.store.history("migrated-current")
        migrated_epoch = migrated["epochs"][0]
        self.assertEqual("legacy-incomplete", migrated_epoch["completeness"])
        self.assertIsNone(migrated_epoch["acquisitionRevision"])
        self.assertEqual("current-claim", migrated_epoch["currentClaim"]["source"])
        self.assertIsNone(migrated["coverage"]["earliestRetainedAcquisitionRevision"])
        self.assertEqual(1, migrated["coverage"]["resourceRevisionWatermark"])

        self._insert_epoch("legacy-no-end", "legacy-no-end", 10, None)
        legacy = self.store.history("legacy-no-end")["epochs"][0]
        self.assertEqual("legacy-incomplete", legacy["completeness"])
        self.assertIsNone(legacy["termination"])
        self.assertIsNone(legacy["currentClaim"])

    def test_singleton_history_lookup_uses_required_revision_index(self) -> None:
        self._ensure_database()
        with closing(sqlite3.connect(self.home / "leases.sqlite3")) as db:
            indexes = {
                str(row[0])
                for row in db.execute(
                    "SELECT name FROM sqlite_master WHERE type = 'index'"
                )
            }
            self.assertIn("epochs_by_resource_revision", indexes)
            plan = db.execute(
                "EXPLAIN QUERY PLAN "
                "SELECT claim_id, resource, agent_id, session_id, owner_id, "
                "work_key, acquired_at, acquisition_revision "
                "FROM epochs WHERE resource = ?",
                ("indexed",),
            ).fetchall()
        self.assertTrue(
            any("epochs_by_resource_revision" in str(row[-1]) for row in plan),
            plan,
        )

    def test_json_is_byte_identical_and_text_and_errors_follow_cli_conventions(
        self,
    ) -> None:
        resource = "cli-history"
        self._acquire(resource, "cli-claim")
        environment = os.environ.copy()
        environment["WORKLEASE_HOME"] = str(self.home)

        def run(*arguments: str) -> subprocess.CompletedProcess[str]:
            return subprocess.run(
                [sys.executable, "-m", "worklease.cli", *arguments],
                check=False,
                capture_output=True,
                text=True,
                env=environment,
            )

        first = run("--json", "history", "--resource", resource)
        second = run("--json", "history", "--resource", resource)
        self.assertEqual(0, first.returncode, first.stderr)
        self.assertEqual(first.stdout, second.stdout)
        self.assertNotIn("\\\\t", first.stdout)

        text = run("history", "--resource", resource)
        self.assertEqual(0, text.returncode, text.stderr)
        self.assertIn('OK history\nRESOURCE\t"cli-history"', text.stdout)
        self.assertIn("COVERAGE\n", text.stdout)
        self.assertIn('SOURCE\t"epoch"', text.stdout)
        self.assertIn('COMPLETENESS\t"open"', text.stdout)
        self.assertIn("CURRENT_CLAIM\n", text.stdout)
        self.assertNotIn("\\\\t", text.stdout)

        missing = run("history")
        self.assertEqual(64, missing.returncode)
        self.assertTrue(missing.stdout.startswith("ERROR history: invalid-arguments\n"))
        self.assertIn(
            "HINT\tExample: worklease history --resource local:formatter",
            missing.stdout,
        )

        invalid = run("history", "--resource", " ")
        self.assertEqual(64, invalid.returncode)
        self.assertEqual(
            "ERROR history: invalid-resource\n"
            "HINT\tExample: worklease history --resource local:formatter\n",
            invalid.stdout,
        )

        invalid_home = Path(self.temporary.name) / "invalid-state"
        invalid_home.mkdir()
        invalid_home.joinpath("leases.sqlite3").mkdir()
        invalid_environment = {**environment, "WORKLEASE_HOME": str(invalid_home)}
        invalid_database = subprocess.run(
            [
                sys.executable,
                "-m",
                "worklease.cli",
                "history",
                "--resource",
                resource,
            ],
            check=False,
            capture_output=True,
            text=True,
            env=invalid_environment,
        )
        self.assertEqual(75, invalid_database.returncode)
        self.assertTrue(
            invalid_database.stdout.startswith("ERROR history: storage-failure\n")
        )
        self.assertIn("check --home, WORKLEASE_HOME", invalid_database.stdout)


if __name__ == "__main__":
    unittest.main()
