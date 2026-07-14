from __future__ import annotations

import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from worklease import __version__


class CliContractTests(unittest.TestCase):
    def setUp(self) -> None:
        self.home = tempfile.TemporaryDirectory()
        self.environment = os.environ.copy()
        self.environment["WORKLEASE_HOME"] = self.home.name

    def tearDown(self) -> None:
        self.home.cleanup()

    def run_cli(self, *arguments: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, "-m", "worklease.cli", *arguments],
            check=False,
            capture_output=True,
            text=True,
            env=self.environment,
        )

    def json_cli(self, *arguments: str, expected_code: int = 0) -> dict[str, object]:
        result = self.run_cli(*arguments)
        self.assertEqual(expected_code, result.returncode, result.stderr)
        self.assertEqual("", result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(1, payload["schemaVersion"])
        self.assertIn("operation", payload)
        self.assertIn("ok", payload)
        return payload

    @staticmethod
    def acquire_arguments(
        resource: str = "repo:cli",
        claim_id: str = "claim-cli",
    ) -> tuple[str, ...]:
        return (
            "acquire",
            "--resource",
            resource,
            "--claim-id",
            claim_id,
            "--agent-id",
            "agent-cli",
            "--session-id",
            "session-cli",
            "--owner-id",
            "owner-cli",
            "--work-key",
            "implement:cli:T5",
        )

    @staticmethod
    def mutation_arguments(
        operation: str,
        resource: str,
        claim: dict[str, object],
        operation_id: str,
    ) -> tuple[str, ...]:
        claim_id = str(claim["claimId"])
        token = str(claim["token"])
        revision = str(claim["revision"])
        return (
            operation,
            "--resource",
            resource,
            "--claim-id",
            claim_id,
            "--token",
            token,
            "--revision",
            revision,
            "--operation-id",
            operation_id,
        )

    def test_version_is_json_by_default_and_bare_in_text_mode(self) -> None:
        payload = self.json_cli("--version")
        self.assertEqual("version", payload["operation"])
        self.assertEqual(True, payload["ok"])
        self.assertEqual(__version__, payload["version"])

        result = self.run_cli("--format", "text", "--version")
        self.assertEqual(0, result.returncode)
        self.assertEqual(f"{__version__}\n", result.stdout)
        self.assertEqual("", result.stderr)

    def test_key_is_schema_versioned_and_uses_adapter_policy(self) -> None:
        payload = self.json_cli(
            "key",
            "--provider",
            "linear",
            "--source",
            "team",
            "--item",
            "ITEM-1",
        )
        self.assertEqual("key", payload["operation"])
        self.assertEqual(True, payload["ok"])
        self.assertEqual("local-coordination", payload["capability"])
        self.assertEqual(False, payload["fencedMutations"])
        self.assertEqual(False, payload["providerFencing"])
        self.assertEqual("local-coordination", payload["genericExecutionGuarantee"])

    def test_lifecycle_redacts_read_only_tokens_and_supports_text_list(self) -> None:
        resource = "repo:cli"
        acquired = self.json_cli(*self.acquire_arguments(resource=resource))
        claim = acquired["claim"]
        self.assertIsInstance(claim, dict)
        assert isinstance(claim, dict)
        token = str(claim["token"])

        status = self.json_cli("status", "--resource", resource)
        self.assertEqual("active", status["state"])
        self.assertNotIn(token, json.dumps(status))

        listed = self.json_cli("list", "--resource", resource)
        claims = listed["claims"]
        assert isinstance(claims, list)
        self.assertEqual(1, len(claims))
        self.assertNotIn(token, json.dumps(listed))

        text_list = self.run_cli("list", "--resource", resource, "--format", "text")
        self.assertEqual(0, text_list.returncode)
        self.assertIn(
            "STATE\tRESOURCE\tCLAIM_ID\tOWNER_ID\tEXPIRES_AT", text_list.stdout
        )
        self.assertNotIn(token, text_list.stdout)

        heartbeat_args = self.mutation_arguments(
            "heartbeat", resource, claim, "heartbeat-cli"
        )
        heartbeat = self.json_cli(*heartbeat_args)
        new_claim = heartbeat["claim"]
        self.assertIsInstance(new_claim, dict)
        assert isinstance(new_claim, dict)
        self.assertGreater(int(new_claim["revision"]), int(claim["revision"]))
        self.assertEqual(token, new_claim["token"])
        checkpoint = self.json_cli(
            *self.mutation_arguments(
                "checkpoint", resource, new_claim, "checkpoint-cli"
            ),
            "--checkpoint",
            '{"step":2,"done":false}',
        )
        checkpoint_claim = checkpoint["claim"]
        assert isinstance(checkpoint_claim, dict)
        self.assertEqual({"step": 2, "done": False}, checkpoint["checkpoint"])
        self.assertGreater(
            int(checkpoint_claim["revision"]), int(new_claim["revision"])
        )
        status_after_checkpoint = self.json_cli("status", "--resource", resource)
        status_claim = status_after_checkpoint["claim"]
        assert isinstance(status_claim, dict)
        self.assertEqual({"step": 2, "done": False}, status_claim["checkpoint"])

        release = self.json_cli(
            *self.mutation_arguments(
                "release", resource, checkpoint_claim, "release-cli"
            ),
            "--reason",
            "checkpoint complete",
        )
        self.assertEqual(True, release["ok"])
        self.assertEqual(
            "free", self.json_cli("status", "--resource", resource)["state"]
        )

    def test_bundle_cli_lifecycle_and_guarded_exec(self) -> None:
        resources = ("repo:bundle-a", "repo:bundle-b")
        acquire_args = (
            "acquire-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--claim-id",
            "bundle-cli",
            "--agent-id",
            "agent-cli",
            "--session-id",
            "session-cli",
            "--owner-id",
            "owner-cli",
            "--work-key",
            "implement:cli:bundle",
        )
        acquired = self.json_cli(*acquire_args)
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        token = str(claim["token"])
        status = self.json_cli(
            "status-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
        )
        self.assertEqual("active", status["state"])
        self.assertNotIn(token, json.dumps(status))

        mutation = (
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--claim-id",
            str(claim["claimId"]),
            "--token",
            token,
            "--revision",
            str(claim["revision"]),
        )
        heartbeat = self.json_cli(
            "heartbeat-bundle",
            *mutation,
            "--operation-id",
            "bundle-heartbeat-cli",
        )
        heartbeat_claim = heartbeat["claim"]
        assert isinstance(heartbeat_claim, dict)
        self.assertGreater(int(heartbeat_claim["revision"]), int(claim["revision"]))
        executed = self.json_cli(
            "exec-bundle",
            *(
                *mutation[:-1],
                str(heartbeat_claim["revision"]),
            ),
            "--operation-id",
            "bundle-exec-cli",
            "--",
            sys.executable,
            "-c",
            "print('bundle-output')",
        )
        command = executed["command"]
        assert isinstance(command, dict)
        self.assertEqual("bundle-output\n", command["stdout"])
        self.assertNotIn(token, json.dumps(executed))
        executed_claim = executed["claim"]
        assert isinstance(executed_claim, dict)
        released = self.json_cli(
            "release-bundle",
            *(
                *mutation[:-1],
                str(executed_claim["revision"]),
            ),
            "--operation-id",
            "bundle-release-cli",
            "--reason",
            "bundle complete",
        )
        self.assertTrue(released["ok"])
        self.assertEqual(
            "free",
            self.json_cli(
                "status-bundle",
                "--resource",
                resources[0],
                "--resource",
                resources[1],
            )["state"],
        )

    def test_malformed_input_is_json_and_does_not_echo_secret_values(self) -> None:
        result = self.run_cli(
            "status", "--resource", "repo:cli", "--token", "secret-token"
        )
        self.assertEqual(64, result.returncode)
        payload = json.loads(result.stdout)
        self.assertEqual(
            {
                "error": "invalid-arguments",
                "ok": False,
                "operation": "status",
                "schemaVersion": 1,
            },
            payload,
        )
        self.assertNotIn("secret-token", result.stdout)
        self.assertEqual("", result.stderr)

        for invalid_ttl_value in ("nan", "inf", "not-a-number"):
            invalid_result = self.run_cli(
                *self.acquire_arguments(),
                "--ttl",
                invalid_ttl_value,
            )
            self.assertEqual(64, invalid_result.returncode)
            self.assertEqual("", invalid_result.stderr)

            def reject_constant(value: str) -> None:
                raise ValueError(value)

            invalid_ttl = json.loads(
                invalid_result.stdout, parse_constant=reject_constant
            )
            expected_error = (
                "invalid-arguments"
                if invalid_ttl_value == "not-a-number"
                else "invalid-ttl"
            )
            self.assertEqual(expected_error, invalid_ttl["error"])

    def test_option_abbreviations_are_rejected(self) -> None:
        result = self.run_cli(
            "status",
            "--res",
            "repo:cli",
        )
        self.assertEqual(64, result.returncode)
        payload = json.loads(result.stdout)
        self.assertEqual("invalid-arguments", payload["error"])
        self.assertEqual("status", payload["operation"])
        self.assertEqual(1, payload["schemaVersion"])

    def test_stale_claim_errors_redact_current_token(self) -> None:
        resource = "repo:stale-error"
        acquired = self.json_cli(
            *self.acquire_arguments(resource=resource, claim_id="stale-error")
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        token = str(claim["token"])
        stale_args = self.mutation_arguments(
            "release", resource, claim, "stale-release"
        )
        contender = self.json_cli(
            *self.acquire_arguments(resource=resource, claim_id="other"),
            expected_code=2,
        )
        self.assertEqual("already-claimed", contender["error"])
        self.assertNotIn('"token"', json.dumps(contender))

        token_index = stale_args.index("--token") + 1
        stale_args = (
            *stale_args[:token_index],
            "wrong",
            *stale_args[token_index + 1 :],
        )
        stale = self.json_cli(*stale_args, "--reason", "stale", expected_code=2)
        self.assertEqual("stale-claim", stale["error"])
        self.assertNotIn('"token"', json.dumps(stale))
        self.assertNotIn(token, json.dumps(stale))

    def test_exec_returns_child_status_and_schema_envelope(self) -> None:
        resource = "repo:exec"
        acquired = self.json_cli(
            *self.acquire_arguments(resource=resource, claim_id="exec")
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        payload = self.json_cli(
            *self.mutation_arguments("exec", resource, claim, "exec-cli"),
            "--",
            sys.executable,
            "-c",
            "print('child-output')",
            "--format",
            "text",
        )
        command = payload["command"]
        assert isinstance(command, dict)
        self.assertEqual(True, payload["ok"])
        self.assertEqual("child-output\n", command["stdout"])
        self.assertNotIn(str(claim["token"]), json.dumps(payload))

        failed_claim = self.json_cli(
            *self.acquire_arguments(resource="repo:exec-fail", claim_id="exec-fail")
        )["claim"]
        assert isinstance(failed_claim, dict)
        failed = self.run_cli(
            *self.mutation_arguments(
                "exec", "repo:exec-fail", failed_claim, "exec-fail-cli"
            ),
            "--",
            sys.executable,
            "-c",
            "raise SystemExit(7)",
        )
        self.assertEqual(7, failed.returncode)
        failed_payload = json.loads(failed.stdout)
        self.assertEqual(1, failed_payload["schemaVersion"])
        self.assertEqual("child-process-failed", failed_payload["error"])
        self.assertEqual("local-coordination", failed_payload["guarantee"])
        self.assertEqual(False, failed_payload["providerFencing"])
        self.assertEqual(False, failed_payload["ok"])
        self.assertEqual(7, failed_payload["command"]["returncode"])
        self.assertNotIn(str(failed_claim["token"]), json.dumps(failed_payload))

    def test_replace_file_is_wired_through_the_cli(self) -> None:
        directory = Path(self.home.name)
        target = directory / "target.txt"
        candidate = directory / "candidate.txt"
        target.write_text("old\n")
        candidate.write_text("new\n")
        expected = hashlib.sha256(target.read_bytes()).hexdigest()
        acquired = self.json_cli(
            *self.acquire_arguments(resource="repo:replace", claim_id="replace")
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        payload = self.json_cli(
            *self.mutation_arguments(
                "replace-file", "repo:replace", claim, "replace-cli"
            ),
            "--path",
            str(target),
            "--expected-sha256",
            expected,
            "--content-file",
            str(candidate),
        )
        self.assertEqual(True, payload["ok"])
        self.assertEqual("new\n", target.read_text())
        self.assertNotIn(str(claim["token"]), json.dumps(payload))


if __name__ == "__main__":
    unittest.main()
