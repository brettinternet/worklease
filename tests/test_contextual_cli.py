from __future__ import annotations

import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from typing import Any, cast


class ContextualCliTests(unittest.TestCase):
    def setUp(self) -> None:
        self.home = tempfile.TemporaryDirectory()
        self.cwd = tempfile.TemporaryDirectory()
        self.environment = {
            **os.environ,
            "PYTHONPATH": str(Path(__file__).parents[1] / "src"),
            "WORKLEASE_HOME": self.home.name,
            "WORKLEASE_AGENT_ID": "context-agent",
        }

    def tearDown(self) -> None:
        self.cwd.cleanup()
        self.home.cleanup()

    def run_cli(self, *arguments: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, "-m", "worklease.cli", "--json", *arguments],
            cwd=self.cwd.name,
            env=self.environment,
            capture_output=True,
            text=True,
            check=False,
        )

    def json_cli(self, *arguments: str, code: int = 0) -> dict[str, object]:
        result = self.run_cli(*arguments)
        self.assertEqual(code, result.returncode, result.stderr)
        self.assertEqual("", result.stderr)
        return json.loads(result.stdout)

    def test_bare_singleton_lifecycle_uses_private_context_handle(self) -> None:
        acquired = self.json_cli("acquire", "--resource", "context:singleton")
        claim = cast(dict[str, Any], acquired["claim"])
        self.assertNotIn("token", claim)
        lease_file = Path(str(acquired["leaseFile"]))
        self.assertTrue(lease_file.is_file())
        self.assertEqual(0o600, stat.S_IMODE(lease_file.stat().st_mode))
        self.assertEqual(0o700, stat.S_IMODE(lease_file.parent.stat().st_mode))

        for arguments in (
            ("status",),
            ("heartbeat",),
            ("checkpoint", "--checkpoint", "{}"),
            ("exec", "--", sys.executable, "-c", "pass"),
        ):
            payload = self.json_cli(*arguments)
            claim = payload.get("claim")
            if isinstance(claim, dict):
                self.assertNotIn("token", claim)
        self.json_cli("release", "--reason", "done")
        self.assertFalse(lease_file.exists())

    def test_bare_bundle_lifecycle_uses_same_context_handle(self) -> None:
        acquired = self.json_cli(
            "acquire-bundle", "--resource", "context:a", "--resource", "context:b"
        )
        self.assertNotIn("token", cast(dict[str, Any], acquired["claim"]))
        lease_file = Path(str(acquired["leaseFile"]))
        status = self.json_cli("status-bundle")
        self.assertEqual(["context:a", "context:b"], status["resources"])
        self.json_cli("heartbeat-bundle")
        self.json_cli("release-bundle", "--reason", "done")
        self.assertFalse(lease_file.exists())

    def test_stateless_opt_out_and_precedence_diagnostics(self) -> None:
        stateless = self.json_cli(
            "acquire", "--resource", "context:stateless", "--no-lease-file"
        )
        self.assertIn("token", cast(dict[str, Any], stateless["claim"]))
        conflict = self.json_cli(
            "heartbeat", "--resource", "context:stateless", code=64
        )
        self.assertEqual("lease-context-conflict", conflict["error"])
        both = self.json_cli(
            "acquire",
            "--resource",
            "context:both",
            "--no-lease-file",
            "-L",
            str(Path(self.home.name) / "handle"),
            code=64,
        )
        self.assertEqual("lease-context-conflict", both["error"])

    def test_missing_context_and_wrong_kind_are_actionable(self) -> None:
        missing = self.json_cli("status", code=64)
        self.assertEqual("lease-context-missing", missing["error"])
        bundle = self.json_cli(
            "acquire-bundle", "--resource", "context:x", "--resource", "context:y"
        )
        wrong = self.json_cli("status", "-L", str(bundle["leaseFile"]), code=64)
        self.assertEqual("lease-file-kind-mismatch", wrong["error"])

    def test_malformed_context_fails_before_store_and_explicit_status_bypasses_it(
        self,
    ) -> None:
        acquired = self.json_cli("acquire", "--resource", "context:original")
        lease_file = Path(str(acquired["leaseFile"]))
        lease_file.write_text("not-json\\n", encoding="utf-8")

        failed = self.json_cli("acquire", "--resource", "context:replacement", code=64)
        self.assertEqual("lease-file-malformed", failed["error"])
        status = self.json_cli("status", "--resource", "context:original")
        self.assertEqual("active", status["state"])

        explicit = self.json_cli(
            "status", "--resource", "context:original", "-L", str(lease_file)
        )
        self.assertEqual("active", explicit["state"])

    def test_unsafe_symlink_and_oversized_contexts_fail_closed(self) -> None:
        acquired = self.json_cli("acquire", "--resource", "context:original")
        lease_file = Path(str(acquired["leaseFile"]))

        lease_file.chmod(0o644)
        unsafe = self.json_cli("acquire", "--resource", "context:unsafe", code=64)
        self.assertEqual("lease-file-unsafe", unsafe["error"])
        lease_file.chmod(0o600)

        lease_file.write_text("x" * (64 * 1024 + 1), encoding="utf-8")
        oversized = self.json_cli("acquire", "--resource", "context:oversized", code=64)
        self.assertEqual("lease-file-too-large", oversized["error"])

        target = lease_file.with_name("target.lease")
        target.write_text("not-used", encoding="utf-8")
        lease_file.unlink()
        lease_file.symlink_to(target)
        symlink = self.json_cli("acquire", "--resource", "context:symlink", code=64)
        self.assertEqual("lease-file-is-symlink", symlink["error"])

    def test_wrong_kind_active_context_is_not_overwritten(self) -> None:
        acquired = self.json_cli("acquire", "--resource", "context:singleton")
        lease_file = str(acquired["leaseFile"])
        rejected = self.json_cli(
            "acquire-bundle",
            "--resource",
            "context:one",
            "--resource",
            "context:two",
            code=64,
        )
        self.assertEqual("lease-file-in-use", rejected["error"])
        self.assertNotIn("token", json.dumps(rejected))
        self.assertTrue(Path(lease_file).is_file())

    def test_contextual_acquire_is_serialized_across_processes(self) -> None:
        command = ("--json", "acquire", "--resource")

        def acquire(resource: str) -> subprocess.CompletedProcess[str]:
            return self.run_cli(*command, resource)

        with ThreadPoolExecutor(max_workers=2) as workers:
            results = list(workers.map(acquire, ("context:race-a", "context:race-b")))

        self.assertCountEqual([0, 64], [result.returncode for result in results])
        payloads = [json.loads(result.stdout) for result in results]
        failure = next(payload for payload in payloads if not payload["ok"])
        self.assertEqual("lease-file-in-use", failure["error"])
        self.assertNotIn("token", failure["error"])
        success = next(payload for payload in payloads if payload["ok"])
        lease_file = Path(str(success["leaseFile"]))
        self.assertTrue(lease_file.is_file())
        self.assertNotIn("token", cast(dict[str, Any], success["claim"]))


if __name__ == "__main__":
    unittest.main()
