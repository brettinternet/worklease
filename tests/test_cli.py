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
from worklease.models import MutationRequest
from worklease.store import LeaseStore


class CliContractTests(unittest.TestCase):
    def setUp(self) -> None:
        self.home = tempfile.TemporaryDirectory()
        self.environment = os.environ.copy()
        self.environment["WORKLEASE_HOME"] = self.home.name

    def tearDown(self) -> None:
        self.home.cleanup()

    def run_cli(
        self, *arguments: str, pass_fds: tuple[int, ...] = ()
    ) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, "-m", "worklease.cli", *arguments],
            check=False,
            capture_output=True,
            text=True,
            env=self.environment,
            pass_fds=pass_fds,
        )

    def json_cli(
        self,
        *arguments: str,
        expected_code: int = 0,
        pass_fds: tuple[int, ...] = (),
    ) -> dict[str, object]:
        result = self.run_cli(*arguments, pass_fds=pass_fds)
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

    def test_policy_list_and_describe_commands_are_versioned(self) -> None:
        listed = self.json_cli("policy", "list")
        policies = listed["policies"]
        self.assertIsInstance(policies, list)
        assert isinstance(policies, list)
        names = {policy["name"] for policy in policies if isinstance(policy, dict)}
        self.assertIn("generic", names)
        generic = self.json_cli("policy", "describe", "--name", "generic")
        self.assertEqual("policy-describe", generic["operation"])
        self.assertEqual("generic", generic["name"])
        self.assertEqual(1, generic["contractVersion"])
        self.assertEqual("local-coordination", generic["capability"])
        self.assertFalse(generic["providerFencingSupported"])

        text = self.run_cli(
            "policy", "describe", "--name", "generic", "--format", "text"
        )
        self.assertEqual(0, text.returncode)
        self.assertIn("name: generic\n", text.stdout)
        self.assertIn("capability: local-coordination\n", text.stdout)

    def test_policy_unknown_name_has_stable_error(self) -> None:
        payload = self.json_cli(
            "policy", "describe", "--name", "typo-provider", expected_code=2
        )
        self.assertEqual("resource-policy-not-found", payload["error"])
        self.assertEqual(1, payload["schemaVersion"])

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

    def test_transfer_cli_returns_successor_claim_and_preserves_checkpoint(
        self,
    ) -> None:
        resource = "repo:transfer-cli"
        acquired = self.json_cli(
            *self.acquire_arguments(resource=resource, claim_id="transfer-first")
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        checkpoint = self.json_cli(
            *self.mutation_arguments(
                "checkpoint", resource, claim, "transfer-checkpoint"
            ),
            "--checkpoint",
            '{"step":1}',
        )["claim"]
        assert isinstance(checkpoint, dict)
        arguments = (
            "transfer",
            "--resource",
            resource,
            "--claim-id",
            str(checkpoint["claimId"]),
            "--token",
            str(claim["token"]),
            "--revision",
            str(checkpoint["revision"]),
            "--operation-id",
            "transfer-cli",
            "--successor-claim-id",
            "transfer-second",
            "--successor-agent-id",
            "agent-second",
            "--successor-session-id",
            "session-second",
            "--successor-owner-id",
            "owner-second",
            "--successor-work-key",
            "implement:cli:transfer",
        )
        transferred = self.json_cli(*arguments)
        successor = transferred["claim"]
        assert isinstance(successor, dict)
        self.assertEqual("transfer-second", successor["claimId"])
        self.assertEqual({"step": 1}, successor["checkpoint"])
        self.assertNotEqual(str(claim["token"]), str(successor["token"]))
        replay = self.json_cli(*arguments)
        self.assertTrue(replay["idempotent"])
        replay_claim = replay["claim"]
        assert isinstance(replay_claim, dict)
        self.assertEqual(successor["token"], replay_claim["token"])
        status = self.json_cli("status", "--resource", resource)
        self.assertNotIn(str(successor["token"]), json.dumps(status))

    def test_verbose_status_cli_is_redacted_and_has_stable_text(self) -> None:
        resource = "repo:verbose-cli"
        acquired = self.json_cli(
            *self.acquire_arguments(resource=resource, claim_id="verbose-cli")
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        token = str(claim["token"])
        request = MutationRequest(
            resource=resource,
            claim_id=str(claim["claimId"]),
            token=token,
            revision=int(claim["revision"]),
            operation_id="verbose-cli-unknown",
        )
        store = LeaseStore(self.home.name)
        self.assertIsNone(
            store.begin_operation(
                request,
                "exec",
                {
                    "revision": request.revision,
                    "command": ["printf", "verbose-sentinel"],
                    "token": token,
                },
            )
        )

        verbose = self.json_cli(
            "status",
            "--resource",
            resource,
            "--verbose",
        )
        self.assertEqual("status-verbose", verbose["operation"])
        self.assertEqual("active", verbose["state"])
        self.assertNotIn(token, json.dumps(verbose))
        self.assertNotIn("verbose-sentinel", json.dumps(verbose))
        unknown_operations = verbose["unknownOperations"]
        assert isinstance(unknown_operations, list)
        self.assertEqual("verbose-cli-unknown", unknown_operations[0]["operationId"])

        text = self.run_cli(
            "status",
            "--resource",
            resource,
            "--verbose",
            "--format",
            "text",
        )
        self.assertEqual(0, text.returncode)
        self.assertEqual("", text.stderr)
        self.assertIn(f'RESOURCE\t"{resource}"', text.stdout)
        self.assertIn("STATE\tactive", text.stdout)
        self.assertIn('UNKNOWN\t"verbose-cli-unknown"\t"exec"', text.stdout)
        self.assertIn("UNKNOWN_OPERATIONS\t1", text.stdout)
        self.assertNotIn(token, text.stdout)
        self.assertNotIn("verbose-sentinel", text.stdout)
        repeated_text = self.run_cli(
            "status",
            "--resource",
            resource,
            "--verbose",
            "--format",
            "text",
        )
        self.assertEqual(text.stdout, repeated_text.stdout)

    def test_operation_inspection_and_reconciliation_cli(self) -> None:
        resource = "repo:reconcile"
        acquired = self.json_cli(*self.acquire_arguments(resource=resource))
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        request = MutationRequest(
            resource=resource,
            claim_id=str(claim["claimId"]),
            token=str(claim["token"]),
            revision=int(claim["revision"]),
            operation_id="unknown-cli",
        )
        store = LeaseStore(self.home.name)
        self.assertIsNone(
            store.begin_operation(
                request,
                "exec",
                {"revision": request.revision, "argv": ["deploy", "artifact"]},
            )
        )
        request_sha256 = hashlib.sha256(
            json.dumps(
                {"revision": request.revision, "argv": ["deploy", "artifact"]},
                sort_keys=True,
                separators=(",", ":"),
            ).encode("utf-8")
        ).hexdigest()
        inspected = self.json_cli(
            "inspect-operation",
            "--resource",
            resource,
            "--operation-id",
            "unknown-cli",
        )
        self.assertEqual("unknown-outcome", inspected["state"])
        self.assertNotIn(str(claim["token"]), json.dumps(inspected))
        reconciled = self.json_cli(
            *self.mutation_arguments(
                "reconcile-operation", resource, claim, "reconcile-cli"
            ),
            "--target-operation-id",
            "unknown-cli",
            "--expected-request-sha256",
            request_sha256,
            "--outcome",
            "observed-failure",
            "--evidence",
            '{"provider":"did-not-run"}',
        )
        self.assertEqual("reconciled", reconciled["state"])
        self.assertEqual("observed-failure", reconciled["outcome"])
        self.assertNotIn(str(claim["token"]), json.dumps(reconciled))
        replay = self.json_cli(
            *self.mutation_arguments(
                "reconcile-operation", resource, claim, "reconcile-cli"
            ),
            "--target-operation-id",
            "unknown-cli",
            "--expected-request-sha256",
            request_sha256,
            "--outcome",
            "observed-failure",
            "--evidence",
            '{"provider":"did-not-run"}',
        )
        self.assertTrue(replay["idempotent"])

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
            "--provider-directory",
            self.home.name,
            "--",
            sys.executable,
            "-c",
            "from pathlib import Path; print(Path.cwd())",
        )
        command = executed["command"]
        assert isinstance(command, dict)
        self.assertEqual(
            {"mode": "provider-directory", "path": str(Path(self.home.name).resolve())},
            command["executionDirectory"],
        )
        self.assertEqual(f"{Path(self.home.name).resolve()}\n", command["stdout"])
        self.assertNotIn(token, json.dumps(executed))
        replayed = self.json_cli(
            "exec-bundle",
            *(
                *mutation[:-1],
                str(heartbeat_claim["revision"]),
            ),
            "--operation-id",
            "bundle-exec-cli",
            "--provider-directory",
            self.home.name,
            "--",
            sys.executable,
            "-c",
            "from pathlib import Path; print(Path.cwd())",
        )
        replayed_command = replayed["command"]
        assert isinstance(replayed_command, dict)
        self.assertTrue(replayed["idempotent"])
        self.assertEqual(
            f"{Path(self.home.name).resolve()}\n", replayed_command["stdout"]
        )
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

    def test_non_argv_token_sources_cover_lifecycle_and_prevent_state_changes(
        self,
    ) -> None:
        resource = "repo:credential-cli"
        acquired = self.json_cli(
            *self.acquire_arguments(resource=resource, claim_id="credential-cli")
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        token = str(claim["token"])

        with tempfile.TemporaryDirectory() as directory:
            token_file = Path(directory) / "token"
            token_file.write_text(f"{token}\n", encoding="utf-8")
            token_file.chmod(0o600)
            heartbeat_args = list(
                self.mutation_arguments(
                    "heartbeat", resource, claim, "credential-heartbeat"
                )
            )
            resource_index = heartbeat_args.index("--resource") + 1
            heartbeat_args[resource_index] = resource
            token_index = heartbeat_args.index("--token")
            del heartbeat_args[token_index : token_index + 2]
            heartbeat_args.extend(("--token-file", str(token_file)))
            heartbeat = self.json_cli(*heartbeat_args)
            renewed = heartbeat["claim"]
            assert isinstance(renewed, dict)
            self.assertGreater(int(renewed["revision"]), int(claim["revision"]))

            missing_args = list(
                self.mutation_arguments(
                    "release", resource, renewed, "credential-missing"
                )
            )
            token_index = missing_args.index("--token")
            del missing_args[token_index : token_index + 2]
            missing = self.json_cli(
                *missing_args,
                "--reason",
                "missing",
                expected_code=64,
            )
            self.assertEqual("credential-missing", missing["error"])
            self.assertEqual(
                "active", self.json_cli("status", "--resource", resource)["state"]
            )

            read_fd, write_fd = os.pipe()

            exec_args = list(
                self.mutation_arguments("exec", resource, renewed, "credential-exec")
            )
            token_index = exec_args.index("--token")
            del exec_args[token_index : token_index + 2]
            exec_args.extend(
                (
                    "--token-file",
                    str(token_file),
                    "--",
                    sys.executable,
                    "-c",
                    "import os; print(os.environ.get('WORKLEASE_TOKEN', ''))",
                )
            )
            executed = self.json_cli(*exec_args)
            self.assertNotIn(token, json.dumps(executed))
            executed_claim = executed["claim"]
            assert isinstance(executed_claim, dict)

            try:
                os.write(write_fd, token.encode("utf-8"))
            finally:
                os.close(write_fd)
            try:
                release_claim = {**executed_claim, "token": token}
                release_args = list(
                    self.mutation_arguments(
                        "release", resource, release_claim, "credential-fd"
                    )
                )
                token_index = release_args.index("--token")
                del release_args[token_index : token_index + 2]
                release_args.extend(("--token-fd", str(read_fd), "--reason", "done"))
                released = self.json_cli(*release_args, pass_fds=(read_fd,))
            finally:
                os.close(read_fd)
            self.assertTrue(released["ok"])

        conflict = self.json_cli(
            "release",
            "--resource",
            "repo:conflict",
            "--claim-id",
            "missing",
            "--token",
            "secret",
            "--token-file",
            "missing",
            "--revision",
            "1",
            "--operation-id",
            "conflict",
            "--reason",
            "conflict",
            expected_code=64,
        )
        self.assertEqual("credential-source-conflict", conflict["error"])

    def test_exec_returns_child_status_and_schema_envelope(self) -> None:
        resource = "repo:exec"
        acquired = self.json_cli(
            *self.acquire_arguments(resource=resource, claim_id="exec")
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        payload = self.json_cli(
            *self.mutation_arguments("exec", resource, claim, "exec-cli"),
            "--provider-directory",
            self.home.name,
            "--",
            sys.executable,
            "-c",
            "print('child-output')",
            "--format",
            "text",
        )
        command = payload["command"]
        assert isinstance(command, dict)
        self.assertEqual(
            {"mode": "provider-directory", "path": str(Path(self.home.name).resolve())},
            command["executionDirectory"],
        )
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
