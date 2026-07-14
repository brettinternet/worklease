from __future__ import annotations

import hashlib
import io
import os
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from typing import cast
from unittest.mock import patch

from worklease import replacement as replacement_module
from worklease.cli import main as cli_main
from worklease.execution import execute
from worklease.models import AcquireRequest, LeaseError, MutationRequest
from worklease.replacement import replace_file
from worklease.store import LeaseStore


class MutableClock:
    def __init__(self, value: float = 1_000.0) -> None:
        self.value = value

    def __call__(self) -> float:
        return self.value

    def advance(self, seconds: float) -> None:
        self.value += seconds


class ExecutionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.home = Path(self.temporary.name) / "state"
        self.store = LeaseStore(self.home)

    def acquire(
        self,
        resource: str = "opaque-resource",
        *,
        claim_id: str = "claim-1",
        operation_id: str = "operation-1",
    ) -> MutationRequest:
        acquired = self.store.acquire(
            AcquireRequest(
                resource=resource,
                claim_id=claim_id,
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:item:T3",
                ttl=5,
            )
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        return MutationRequest(
            resource=resource,
            claim_id=str(claim["claimId"]),
            token=str(claim["token"]),
            revision=int(claim["revision"]),
            operation_id=operation_id,
            ttl=5,
        )

    def test_exec_uses_argv_captures_output_and_replays_without_side_effect(
        self,
    ) -> None:
        request = self.acquire()
        output_file = self.home / "runs.txt"
        script = (
            "from pathlib import Path; "
            f"Path({str(output_file)!r}).open('a').write('once\\n'); "
            "print('stdout'); print('stderr', file=__import__('sys').stderr)"
        )
        first, first_code = execute(self.store, request, [sys.executable, "-c", script])
        retry, retry_code = execute(self.store, request, [sys.executable, "-c", script])
        self.assertEqual(0, first_code)
        self.assertEqual(0, retry_code)
        self.assertFalse(first["idempotent"])
        self.assertTrue(retry["idempotent"])
        first_command = cast(dict[str, object], first["command"])
        self.assertEqual("stdout\n", first_command["stdout"])
        self.assertEqual("stderr\n", first_command["stderr"])
        self.assertEqual("once\n", output_file.read_text())

        with self.assertRaisesRegex(LeaseError, "operation-id-request-mismatch"):
            execute(self.store, request, [sys.executable, "-c", "print('changed')"])

    def test_exec_heartbeats_during_long_child_and_returns_failure_status(self) -> None:
        request = self.acquire()
        request = MutationRequest(
            resource=request.resource,
            claim_id=request.claim_id,
            token=request.token,
            revision=request.revision,
            operation_id=request.operation_id,
            ttl=1.0,
        )
        receipt, code = execute(
            self.store,
            request,
            [
                sys.executable,
                "-c",
                "import time; time.sleep(1.2); print('done'); raise SystemExit(7)",
            ],
        )
        self.assertEqual(7, code)
        self.assertFalse(receipt["ok"])
        command = cast(dict[str, object], receipt["command"])
        self.assertEqual("done\n", command["stdout"])
        claim = cast(dict[str, object], receipt["claim"])
        revision = claim["revision"]
        assert isinstance(revision, int)
        self.assertGreater(revision, request.revision)

    def test_exec_storage_failure_terminates_child_as_unknown_outcome(self) -> None:
        request = self.acquire("heartbeat-storage-failure")
        request = MutationRequest(
            resource=request.resource,
            claim_id=request.claim_id,
            token=request.token,
            revision=request.revision,
            operation_id=request.operation_id,
            ttl=0.2,
        )
        started = self.home / "started"
        finished = self.home / "finished"
        script = (
            "from pathlib import Path; import time; "
            f"Path({str(started)!r}).write_text('started'); "
            "time.sleep(1); "
            f"Path({str(finished)!r}).write_text('finished')"
        )
        original_heartbeat = self.store.heartbeat
        calls = 0

        def heartbeat_then_fail(
            heartbeat_request: MutationRequest, *, lock_held: bool = False
        ) -> dict[str, object]:
            nonlocal calls
            calls += 1
            if not started.exists():
                return original_heartbeat(heartbeat_request, lock_held=lock_held)
            raise sqlite3.OperationalError("database is unavailable")

        with (
            patch.object(self.store, "heartbeat", side_effect=heartbeat_then_fail),
            self.assertRaisesRegex(LeaseError, "unknown-outcome"),
        ):
            execute(
                self.store,
                request,
                [sys.executable, "-c", script],
            )

        self.assertTrue(started.exists())
        self.assertFalse(finished.exists())
        self.assertGreaterEqual(calls, 2)

    def test_exec_decodes_invalid_output_and_normalizes_signal_status(self) -> None:
        decode_request = self.acquire("invalid-output")
        decoded, decoded_code = execute(
            self.store,
            decode_request,
            [
                sys.executable,
                "-c",
                "import sys; sys.stdout.buffer.write(b'\\xff')",
            ],
        )
        self.assertEqual(0, decoded_code)
        decoded_command = cast(dict[str, object], decoded["command"])
        self.assertEqual("\ufffd", decoded_command["stdout"])

        signal_request = self.acquire("signal-output", claim_id="signal-claim")
        signaled, signaled_code = execute(
            self.store,
            signal_request,
            [
                sys.executable,
                "-c",
                "import os, signal; os.kill(os.getpid(), signal.SIGTERM)",
            ],
        )
        self.assertEqual(143, signaled_code)
        signaled_command = cast(dict[str, object], signaled["command"])
        self.assertEqual(143, signaled_command["returncode"])

    def test_ownership_loss_terminates_running_process_group(self) -> None:
        request = self.acquire()
        request = MutationRequest(
            resource=request.resource,
            claim_id=request.claim_id,
            token=request.token,
            revision=request.revision,
            operation_id=request.operation_id,
            ttl=1.0,
        )
        started = self.home / "started"
        marker = self.home / "must-not-finish"
        command = [
            sys.executable,
            "-c",
            (
                "import time; from pathlib import Path; "
                f"Path({str(started)!r}).write_text('started'); "
                "time.sleep(1); "
                f"Path({str(marker)!r}).write_text('finished')"
            ),
        ]
        original_heartbeat = self.store.heartbeat
        calls = 0

        def heartbeat_then_fail(
            heartbeat_request: MutationRequest, *, lock_held: bool = False
        ) -> dict[str, object]:
            nonlocal calls
            calls += 1
            if not started.exists():
                return original_heartbeat(heartbeat_request, lock_held=lock_held)
            raise LeaseError("claim-changed-during-guard", code=3)

        with (
            patch.object(
                self.store,
                "heartbeat",
                side_effect=heartbeat_then_fail,
            ),
            self.assertRaisesRegex(LeaseError, "claim-changed-during-guard"),
        ):
            execute(self.store, request, command)
        self.assertTrue(started.exists())

        self.assertGreaterEqual(calls, 2)
        self.assertFalse(marker.exists())

    def test_ownership_loss_kills_descendant_after_leader_exit(self) -> None:
        request = self.acquire("descendant-resource")
        request = MutationRequest(
            resource=request.resource,
            claim_id=request.claim_id,
            token=request.token,
            revision=request.revision,
            operation_id=request.operation_id,
            ttl=0.2,
        )
        started = self.home / "descendant-started"
        marker = self.home / "descendant-must-not-finish"
        child_code = (
            "import signal,time; from pathlib import Path; "
            "signal.signal(signal.SIGTERM, signal.SIG_IGN); "
            f"Path({str(started)!r}).write_text('started'); "
            "time.sleep(1); "
            f"Path({str(marker)!r}).write_text('finished')"
        )
        parent_code = (
            "import subprocess,sys; "
            f"subprocess.Popen([sys.executable, '-c', {child_code!r}])"
        )
        original_heartbeat = self.store.heartbeat
        calls = 0

        def heartbeat_then_fail(
            heartbeat_request: MutationRequest, *, lock_held: bool = False
        ) -> dict[str, object]:
            nonlocal calls
            calls += 1
            if not started.exists():
                return original_heartbeat(heartbeat_request, lock_held=lock_held)
            raise LeaseError("claim-changed-during-guard", code=3)

        with (
            patch.object(
                self.store,
                "heartbeat",
                side_effect=heartbeat_then_fail,
            ),
            self.assertRaisesRegex(LeaseError, "claim-changed-during-guard"),
        ):
            execute(self.store, request, [sys.executable, "-c", parent_code])
        self.assertTrue(started.exists())
        self.assertGreaterEqual(calls, 2)
        time.sleep(1.2)
        self.assertFalse(marker.exists())

    def test_started_intent_is_unknown_outcome_and_never_reruns(self) -> None:
        request = self.acquire()
        operation_request = request.request_dict(
            argv=[sys.executable, "-c", "print('must not run')"],
            executionDirectory={"mode": "caller"},
        )
        self.assertIsNone(
            self.store.begin_operation(request, "exec", operation_request)
        )
        with self.assertRaisesRegex(LeaseError, "unknown-outcome"):
            execute(
                self.store,
                request,
                [sys.executable, "-c", "print('must not run')"],
            )

    def test_replacement_is_atomic_preserves_mode_and_replays(self) -> None:
        request = self.acquire("markdown-source")
        target = self.home / "target.md"
        candidate = self.home / "candidate.md"
        target.write_text("old\n")
        candidate.write_text("new\n")
        target.chmod(0o640)
        expected = hashlib.sha256(target.read_bytes()).hexdigest()
        first = replace_file(self.store, request, target, expected, candidate)
        retry = replace_file(self.store, request, target, expected, candidate)
        self.assertTrue(first["ok"])
        self.assertFalse(first["idempotent"])
        self.assertTrue(retry["idempotent"])
        self.assertEqual("new\n", target.read_text())
        self.assertEqual(0o640, target.stat().st_mode & 0o777)
        candidate.write_text("changed\n")
        with self.assertRaisesRegex(LeaseError, "operation-id-request-mismatch"):
            replace_file(self.store, request, target, expected, candidate)
        candidate.unlink()
        replay_without_inputs = replace_file(
            self.store, request, target, expected, candidate
        )
        self.assertTrue(replay_without_inputs["idempotent"])

    def test_completed_replacement_replays_after_claim_expiry(self) -> None:
        clock = MutableClock()
        store = LeaseStore(self.home / "replay-expiry", clock=clock)
        acquired = store.acquire(
            AcquireRequest(
                resource="replay-expiry-resource",
                claim_id="replay-expiry-claim",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:item:T3",
                ttl=1,
            )
        )
        claim = cast(dict[str, object], acquired["claim"])
        revision = claim["revision"]
        assert isinstance(revision, int)
        request = MutationRequest(
            resource="replay-expiry-resource",
            claim_id=str(claim["claimId"]),
            token=str(claim["token"]),
            revision=revision,
            operation_id="replay-expiry-operation",
            ttl=1,
        )
        target = self.home / "replay-expiry-target.md"
        candidate = self.home / "replay-expiry-candidate.md"
        target.write_text("old\n")
        candidate.write_text("new\n")
        expected = hashlib.sha256(target.read_bytes()).hexdigest()

        first = replace_file(store, request, target, expected, candidate)
        clock.advance(2)
        retry = replace_file(store, request, target, expected, candidate)

        self.assertFalse(first["idempotent"])
        self.assertTrue(retry["idempotent"])

    def test_replacement_rejects_wrong_hash_and_symlink(self) -> None:
        request = self.acquire("markdown-source")
        target = self.home / "target.md"
        candidate = self.home / "candidate.md"
        target.write_text("old\n")
        candidate.write_text("new\n")
        with self.assertRaisesRegex(LeaseError, "file-version-conflict"):
            replace_file(self.store, request, target, "0" * 64, candidate)
        link = self.home / "link.md"
        symlink_request = MutationRequest(
            resource=request.resource,
            claim_id=request.claim_id,
            token=request.token,
            revision=request.revision,
            operation_id="symlink-operation",
            ttl=request.ttl,
        )
        link.symlink_to(target)
        with self.assertRaisesRegex(LeaseError, "target-is-symlink"):
            replace_file(
                self.store,
                symlink_request,
                link,
                hashlib.sha256(target.read_bytes()).hexdigest(),
                candidate,
            )
        self.assertEqual("old\n", target.read_text())

    def test_replacement_keeps_ownership_during_atomic_write(self) -> None:
        clock = MutableClock()
        store = LeaseStore(self.home / "expiry", clock=clock)
        acquired = store.acquire(
            AcquireRequest(
                resource="expiring-resource",
                claim_id="expiring-claim",
                agent_id="agent",
                session_id="session",
                owner_id="owner",
                work_key="implement:item:T3",
                ttl=1,
            )
        )
        claim = cast(dict[str, object], acquired["claim"])
        revision = claim["revision"]
        assert isinstance(revision, int)
        request = MutationRequest(
            resource="expiring-resource",
            claim_id=str(claim["claimId"]),
            token=str(claim["token"]),
            revision=revision,
            operation_id="expiry-replace",
            ttl=1,
        )
        target = self.home / "expiry-target.md"
        candidate = self.home / "expiry-candidate.md"
        target.write_text("old\n")
        candidate.write_text("new\n")
        expected = hashlib.sha256(target.read_bytes()).hexdigest()
        original_atomic_replace = replacement_module.atomic_replace

        def delayed_replace(path: Path, content: bytes, mode: int) -> None:
            clock.advance(2)
            original_atomic_replace(path, content, mode)

        with (
            patch.object(
                replacement_module, "atomic_replace", side_effect=delayed_replace
            ),
            self.assertRaisesRegex(LeaseError, "claim-expired"),
        ):
            replace_file(store, request, target, expected, candidate)
        self.assertEqual("new\n", target.read_text())
        with self.assertRaisesRegex(LeaseError, "unknown-outcome"):
            replace_file(store, request, target, expected, candidate)

    def test_cli_wires_exec_and_returns_child_status(self) -> None:
        request = self.acquire()
        output = io.StringIO()
        with redirect_stdout(output):
            code = cli_main(
                [
                    "--home",
                    str(self.home),
                    "exec",
                    "--resource",
                    request.resource,
                    "--claim-id",
                    request.claim_id,
                    "--token",
                    request.token,
                    "--revision",
                    str(request.revision),
                    "--operation-id",
                    request.operation_id,
                    "--",
                    sys.executable,
                    "-c",
                    "raise SystemExit(4)",
                ]
            )
        self.assertEqual(4, code)
        self.assertIn('"operation":"exec"', output.getvalue())
        self.assertIn('"returncode":4', output.getvalue())

    def test_crash_before_parent_completion_is_recoverable_as_unknown(self) -> None:
        request = self.acquire("crash-resource")
        script = f"""
from worklease.models import MutationRequest
from worklease.store import LeaseStore
store = LeaseStore({str(self.home)!r})
request = MutationRequest({request.resource!r}, {request.claim_id!r}, {request.token!r}, {request.revision}, {request.operation_id!r}, ttl=5)
store.begin_operation(request, 'exec', request.request_dict(
    argv=['echo', 'crash'], executionDirectory={{'mode': 'caller'}}
))
"""
        environment = {
            **os.environ,
            "PYTHONPATH": str(Path(__file__).parents[1] / "src"),
        }
        completed = subprocess.run(
            [sys.executable, "-c", script],
            env=environment,
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(0, completed.returncode, completed.stderr)
        with self.assertRaisesRegex(LeaseError, "unknown-outcome"):
            execute(self.store, request, ["echo", "crash"])


if __name__ == "__main__":
    unittest.main()
