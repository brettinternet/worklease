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
from worklease.models import BundleMutationRequest, MutationRequest
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
        visible_arguments = (
            arguments[: arguments.index("--")] if "--" in arguments else arguments
        )
        output_arguments = (
            arguments
            if any(
                value == "--json"
                or value == "--format"
                or value.startswith("--format=")
                for value in visible_arguments
            )
            else ("--json", *arguments)
        )
        result = self.run_cli(*output_arguments, pass_fds=pass_fds)
        self.assertEqual(expected_code, result.returncode, result.stderr)
        self.assertEqual("", result.stderr)
        payload = json.loads(result.stdout)
        self.assertEqual(1, payload["schemaVersion"])
        self.assertIn("operation", payload)
        self.assertIn("ok", payload)
        return payload

    def text_cli(
        self,
        *arguments: str,
        expected_code: int = 0,
        pass_fds: tuple[int, ...] = (),
    ) -> str:
        result = self.run_cli("--format", "text", *arguments, pass_fds=pass_fds)
        self.assertEqual(expected_code, result.returncode, result.stderr)
        self.assertEqual("", result.stderr)
        self.assertNotEqual("", result.stdout)
        self.assertFalse(result.stdout.lstrip().startswith("{"))
        return result.stdout

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

    def test_version_is_text_by_default_and_json_is_explicit(self) -> None:
        result = self.run_cli("--version")
        self.assertEqual(0, result.returncode)
        self.assertEqual(f"{__version__}\n", result.stdout)
        self.assertEqual("", result.stderr)

        for arguments in (
            ("--json", "--version"),
            ("--format", "json", "--version"),
        ):
            with self.subTest(arguments=arguments):
                result = self.run_cli(*arguments)
                self.assertEqual(0, result.returncode)
                payload = json.loads(result.stdout)
                self.assertEqual("version", payload["operation"])
                self.assertEqual(True, payload["ok"])
                self.assertEqual(__version__, payload["version"])

        command_local = self.run_cli(
            "key",
            "--json",
            "--provider",
            "linear",
            "--source",
            "acme",
            "--item",
            "T1",
        )
        self.assertEqual(0, command_local.returncode)
        self.assertEqual(1, json.loads(command_local.stdout)["schemaVersion"])

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

    def test_bundle_operation_inspection_and_reconciliation_cli(self) -> None:
        resources = ("repo:reconcile-a", "repo:reconcile-b")
        acquired = self.json_cli(
            "acquire-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--claim-id",
            "bundle-reconcile-cli",
            "--agent-id",
            "agent-cli",
            "--session-id",
            "session-cli",
            "--owner-id",
            "owner-cli",
            "--work-key",
            "bundle-reconcile",
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        target = BundleMutationRequest(
            resources=resources,
            claim_id=str(claim["claimId"]),
            token=str(claim["token"]),
            revision=int(claim["revision"]),
            operation_id="bundle-unknown-cli",
        )
        operation_request = target.request_dict(argv=["deploy", "bundle"])
        store = LeaseStore(self.home.name)
        self.assertIsNone(
            store.begin_bundle_operation(target, "exec-bundle", operation_request)
        )
        request_sha256 = hashlib.sha256(
            json.dumps(operation_request, sort_keys=True, separators=(",", ":")).encode(
                "utf-8"
            )
        ).hexdigest()

        inspected = self.json_cli(
            "inspect-operation-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--operation-id",
            "bundle-unknown-cli",
        )
        self.assertEqual(list(resources), inspected["resources"])
        self.assertEqual("unknown-outcome", inspected["state"])
        self.assertNotIn(str(claim["token"]), json.dumps(inspected))
        text = self.text_cli(
            "inspect-operation-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--operation-id",
            "bundle-unknown-cli",
        )
        self.assertIn("OK inspect-operation-bundle\n", text)
        self.assertIn('RESOURCES\t["repo:reconcile-a","repo:reconcile-b"]\n', text)

        mutation = (
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--claim-id",
            str(claim["claimId"]),
            "--token",
            str(claim["token"]),
            "--revision",
            str(claim["revision"]),
            "--operation-id",
            "bundle-reconcile-cli",
            "--target-operation-id",
            "bundle-unknown-cli",
            "--expected-request-sha256",
            request_sha256,
            "--outcome",
            "observed-success",
            "--evidence",
            '{"provider":"verified"}',
        )
        reconciled = self.json_cli("reconcile-operation-bundle", *mutation)
        self.assertEqual("reconciled", reconciled["state"])
        self.assertEqual(list(resources), reconciled["resources"])
        self.assertNotIn(str(claim["token"]), json.dumps(reconciled))
        replay = self.json_cli("reconcile-operation-bundle", *mutation)
        self.assertTrue(replay["idempotent"])
        reconciled_text = self.text_cli(
            "inspect-operation-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--operation-id",
            "bundle-unknown-cli",
        )
        self.assertIn('STATE\t"reconciled"\n', reconciled_text)

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

    def test_malformed_input_is_text_by_default_and_json_is_explicit(self) -> None:
        result = self.run_cli(
            "status", "--resource", "repo:cli", "--token", "secret-token"
        )
        self.assertEqual(64, result.returncode)
        self.assertEqual("ERROR status: invalid-arguments\n", result.stdout)
        self.assertNotIn("secret-token", result.stdout)
        self.assertEqual("", result.stderr)

        for invalid_ttl_value in ("nan", "inf", "not-a-number"):
            invalid_result = self.run_cli(
                "--json",
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
        self.assertEqual("ERROR status: invalid-arguments\n", result.stdout)
        self.assertEqual("", result.stderr)

    def test_json_and_format_conflicts_are_order_independent(self) -> None:
        text_cases = (
            ("--json", "--format", "text", "status", "--resource", "repo:cli"),
            ("--format", "text", "status", "--json", "--resource", "repo:cli"),
            ("status", "--json", "--format", "text", "--resource", "repo:cli"),
        )
        for arguments in text_cases:
            with self.subTest(arguments=arguments):
                result = self.run_cli(*arguments)
                self.assertEqual(64, result.returncode)
                self.assertEqual("ERROR status: invalid-arguments\n", result.stdout)
                self.assertEqual("", result.stderr)

        json_cases = (
            ("--json", "--format", "json", "status", "--resource", "repo:cli"),
            ("--format", "json", "status", "--json", "--resource", "repo:cli"),
        )
        for arguments in json_cases:
            with self.subTest(arguments=arguments):
                result = self.run_cli(*arguments)
                self.assertEqual(64, result.returncode)
                payload = json.loads(result.stdout)
                self.assertEqual(1, payload["schemaVersion"])
                self.assertEqual("status", payload["operation"])
                self.assertEqual("invalid-arguments", payload["error"])

    def test_child_arguments_do_not_select_or_conflict_output(self) -> None:
        result = self.run_cli("exec", "--", "--json", "--format", "text")
        self.assertEqual(64, result.returncode)
        self.assertEqual("ERROR exec: invalid-arguments\n", result.stdout)
        self.assertEqual("", result.stderr)

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

            replace_resource = "repo:replace-file-source"
            replace_claim = self.json_cli(
                *self.acquire_arguments(
                    resource=replace_resource, claim_id="replace-file-source"
                )
            )["claim"]
            assert isinstance(replace_claim, dict)
            replace_token = str(replace_claim["token"])
            target = Path(directory) / "target"
            candidate = Path(directory) / "candidate"
            target.write_text("old\n", encoding="utf-8")
            candidate.write_text("new\n", encoding="utf-8")
            expected_sha256 = hashlib.sha256(target.read_bytes()).hexdigest()
            token_file.write_text(f"{replace_token}\n", encoding="utf-8")
            replace_args = list(
                self.mutation_arguments(
                    "replace-file",
                    replace_resource,
                    replace_claim,
                    "replace-file-source",
                )
            )
            token_index = replace_args.index("--token")
            del replace_args[token_index : token_index + 2]
            replace_args.extend(
                (
                    "--token-file",
                    str(token_file),
                    "--path",
                    str(target),
                    "--expected-sha256",
                    expected_sha256,
                    "--content-file",
                    str(candidate),
                )
            )
            replaced = self.json_cli(*replace_args)
            self.assertTrue(replaced["ok"])
            self.assertEqual("new\n", target.read_text(encoding="utf-8"))
            self.assertNotIn(replace_token, json.dumps(replaced))

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
            "--json",
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

    def test_text_renderers_cover_read_only_commands_and_failures(self) -> None:
        version = self.text_cli("--version")
        self.assertEqual(f"{__version__}\n", version)

        missing = self.text_cli(expected_code=64)
        self.assertEqual("ERROR parse: missing-command\n", missing)

        key = self.text_cli(
            "key",
            "--provider",
            "linear",
            "--source",
            "team",
            "--item",
            "TEXT-KEY",
        )
        self.assertIn("OK key\n", key)
        self.assertIn('PROVIDER\t"linear"\n', key)
        self.assertNotIn("schemaVersion", key)

        policies = self.text_cli("policy", "list")
        self.assertIn("NAME\tORIGIN\tORIGIN_VERSION\tCONTRACT_VERSION", policies)
        described = self.text_cli("policy", "describe", "--name", "generic")
        self.assertIn("name: generic\n", described)
        self.assertIn("keyPolicyVersion: 1\n", described)
        self.assertIn("genericExecutionGuarantee: local-coordination\n", described)
        policy_error = self.text_cli(
            "policy", "describe", "--name", "missing", expected_code=2
        )
        self.assertEqual(
            'ERROR policy-describe: resource-policy-not-found\nPROVIDER\t"missing"\n',
            policy_error,
        )

        free_status = self.text_cli("status", "--resource", "repo:text-free")
        self.assertIn(
            'OK status\nRESOURCE\t"repo:text-free"\nSTATE\tfree\n',
            free_status,
        )
        free_bundle = self.text_cli(
            "status-bundle",
            "--resource",
            "repo:text-free-a",
            "--resource",
            "repo:text-free-b",
        )
        self.assertIn(
            'OK status-bundle\nRESOURCES\t["repo:text-free-a","repo:text-free-b"]\n'
            "STATE\tfree\n",
            free_bundle,
        )

        control_resource = "repo:text-escape-\x1b[31m"
        self.json_cli(
            *self.acquire_arguments(resource=control_resource, claim_id="text-escape")
        )
        escaped = self.text_cli("list", "--resource", control_resource)
        self.assertIn('"repo:text-escape-\\u001b[31m"', escaped)
        self.assertNotIn("\x1b", escaped)

        del_resource = "repo:text-del-\x7f"
        self.json_cli(
            *self.acquire_arguments(resource=del_resource, claim_id="text-del")
        )
        del_escaped = self.text_cli("list", "--resource", del_resource)
        self.assertIn('"repo:text-del-\\u007f"', del_escaped)
        self.assertNotIn("\x7f", del_escaped)
        acquired = self.json_cli(
            *self.acquire_arguments(resource="repo:text-read", claim_id="text-read")
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        token = str(claim["token"])
        status = self.text_cli("status", "--resource", "repo:text-read")
        self.assertIn("OK status\nSTATE\tactive\n", status)
        self.assertNotIn(token, status)
        verbose = self.text_cli("status", "--resource", "repo:text-read", "--verbose")
        self.assertIn('RESOURCE\t"repo:text-read"\nSTATE\tactive\n', verbose)
        self.assertNotIn(token, verbose)
        listed = self.text_cli("list", "--resource", "repo:text-read")
        self.assertIn("STATE\tRESOURCE\tCLAIM_ID\tOWNER_ID\tEXPIRES_AT\n", listed)
        self.assertNotIn(token, listed)

        resources = ("repo:text-read-a", "repo:text-read-b")
        self.json_cli(
            "acquire-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--claim-id",
            "text-read-bundle",
            "--agent-id",
            "agent",
            "--session-id",
            "session",
            "--owner-id",
            "owner",
            "--work-key",
            "text:read-bundle",
        )
        bundle_status = self.text_cli(
            "status-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
        )
        self.assertIn("OK status-bundle\nSTATE\tactive\n", bundle_status)
        self.assertNotIn(token, bundle_status)

        operation_resource = "repo:text-inspect"
        operation_acquired = self.json_cli(
            *self.acquire_arguments(
                resource=operation_resource, claim_id="text-inspect"
            )
        )
        operation_claim = operation_acquired["claim"]
        assert isinstance(operation_claim, dict)
        operation_request = MutationRequest(
            resource=operation_resource,
            claim_id=str(operation_claim["claimId"]),
            token=str(operation_claim["token"]),
            revision=int(operation_claim["revision"]),
            operation_id="text-inspect-operation",
        )
        store = LeaseStore(self.home.name)
        self.assertIsNone(
            store.begin_operation(
                operation_request,
                "exec",
                {"revision": operation_request.revision, "argv": ["text"]},
            )
        )
        inspected = self.text_cli(
            "inspect-operation",
            "--resource",
            operation_resource,
            "--operation-id",
            operation_request.operation_id,
        )
        self.assertIn("OK inspect-operation\n", inspected)
        self.assertIn('STATE\t"unknown-outcome"\n', inspected)
        inspect_error = self.text_cli(
            "inspect-operation",
            "--resource",
            "repo:text-missing",
            "--operation-id",
            "missing",
            expected_code=3,
        )
        self.assertEqual(
            'ERROR inspect-operation: operation-not-found\nOPERATION_ID\t"missing"\n',
            inspect_error,
        )

        gc = self.text_cli("gc")
        self.assertIn("OK gc\nDRY_RUN\ttrue\n", gc)
        gc_error = self.text_cli("gc", "--cutoff", "not-a-timestamp", expected_code=64)
        self.assertIn("ERROR gc: invalid-gc-cutoff\n", gc_error)

    def test_text_renderers_cover_mutations_aliases_and_child_failures(self) -> None:
        acquired_text = self.text_cli(
            *self.acquire_arguments(
                resource="repo:text-acquire", claim_id="text-acquire"
            )
        )
        self.assertIn("OK acquire\n", acquired_text)
        self.assertIn("TOKEN\t", acquired_text)

        resource = "repo:text-mutation"
        acquired = self.json_cli(
            *self.acquire_arguments(resource=resource, claim_id="text-mutation")
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        token = str(claim["token"])
        heartbeat = self.text_cli(
            *self.mutation_arguments("heartbeat", resource, claim, "text-heartbeat")
        )
        self.assertIn("OK heartbeat\n", heartbeat)
        self.assertIn("TOKEN\t", heartbeat)
        current = self.json_cli("status", "--resource", resource)["claim"]
        assert isinstance(current, dict)
        current = {**current, "token": token}
        checkpoint = self.text_cli(
            *self.mutation_arguments(
                "checkpoint", resource, current, "text-checkpoint"
            ),
            "--checkpoint",
            '{"step":"text","unicode":"café"}',
        )
        self.assertIn('CHECKPOINT\t{"step":"text","unicode":"café"}\n', checkpoint)
        current = self.json_cli("status", "--resource", resource)["claim"]
        assert isinstance(current, dict)
        current = {**current, "token": token}

        executed = self.text_cli(
            *self.mutation_arguments("exec", resource, current, "text-exec"),
            "--provider-directory",
            self.home.name,
            "--",
            sys.executable,
            "-c",
            "print('text-child')",
        )
        self.assertIn("OK exec\n", executed)
        self.assertIn('STDOUT\t"text-child\\n"\n', executed)
        current = self.json_cli("status", "--resource", resource)["claim"]
        assert isinstance(current, dict)
        current = {**current, "token": token}
        released = self.text_cli(
            *self.mutation_arguments("release", resource, current, "text-release"),
            "--reason",
            "text complete",
        )
        self.assertIn("OK release\n", released)
        self.assertIn("RELEASED_CLAIM_ID\t", released)

        failed_resource = "repo:text-exec-failure"
        failed_claim = self.json_cli(
            *self.acquire_arguments(
                resource=failed_resource, claim_id="text-exec-failure"
            )
        )["claim"]
        assert isinstance(failed_claim, dict)
        failed = self.text_cli(
            *self.mutation_arguments(
                "exec", failed_resource, failed_claim, "text-exec-failure"
            ),
            "--",
            sys.executable,
            "-c",
            "import sys; print('child-stderr', file=sys.stderr); raise SystemExit(7)",
            expected_code=7,
        )
        self.assertIn("ERROR exec: child-process-failed\n", failed)
        self.assertIn('STDERR\t"child-stderr\\n"\n', failed)
        self.assertNotIn(str(failed_claim["token"]), failed)

        transfer_resource = "repo:text-transfer"
        transfer_claim = self.json_cli(
            *self.acquire_arguments(
                resource=transfer_resource, claim_id="text-transfer"
            )
        )["claim"]
        assert isinstance(transfer_claim, dict)
        transferred = self.text_cli(
            "transfer",
            "--resource",
            transfer_resource,
            "--claim-id",
            str(transfer_claim["claimId"]),
            "--token",
            str(transfer_claim["token"]),
            "--revision",
            str(transfer_claim["revision"]),
            "--operation-id",
            "text-transfer",
            "--successor-claim-id",
            "text-successor",
            "--successor-agent-id",
            "agent-successor",
            "--successor-session-id",
            "session-successor",
            "--successor-owner-id",
            "owner-successor",
            "--successor-work-key",
            "text:successor",
        )
        self.assertIn("OK transfer\n", transferred)
        self.assertIn("TOKEN\t", transferred)

        reconcile_resource = "repo:text-reconcile"
        reconcile_acquired = self.json_cli(
            *self.acquire_arguments(
                resource=reconcile_resource, claim_id="text-reconcile"
            )
        )
        reconcile_claim = reconcile_acquired["claim"]
        assert isinstance(reconcile_claim, dict)
        reconcile_request = MutationRequest(
            resource=reconcile_resource,
            claim_id=str(reconcile_claim["claimId"]),
            token=str(reconcile_claim["token"]),
            revision=int(reconcile_claim["revision"]),
            operation_id="text-unknown",
        )
        store = LeaseStore(self.home.name)
        self.assertIsNone(
            store.begin_operation(
                reconcile_request,
                "exec",
                {"revision": reconcile_request.revision, "argv": ["text"]},
            )
        )
        request_sha256 = hashlib.sha256(
            json.dumps(
                {"revision": reconcile_request.revision, "argv": ["text"]},
                sort_keys=True,
                separators=(",", ":"),
            ).encode("utf-8")
        ).hexdigest()
        reconciled = self.text_cli(
            *self.mutation_arguments(
                "reconcile-operation",
                reconcile_resource,
                reconcile_claim,
                "text-reconcile",
            ),
            "--target-operation-id",
            "text-unknown",
            "--expected-request-sha256",
            request_sha256,
            "--outcome",
            "observed-failure",
            "--evidence",
            '{"source":"text"}',
        )
        self.assertIn("OK reconcile-operation\n", reconciled)

        directory = Path(self.home.name)
        target = directory / "text-target"
        candidate = directory / "text-candidate"
        target.write_text("old\n")
        candidate.write_text("new\n")
        replace_resource = "repo:text-replace"
        replace_claim = self.json_cli(
            *self.acquire_arguments(resource=replace_resource, claim_id="text-replace")
        )["claim"]
        assert isinstance(replace_claim, dict)
        replaced = self.text_cli(
            *self.mutation_arguments(
                "replace-file", replace_resource, replace_claim, "text-replace"
            ),
            "--path",
            str(target),
            "--expected-sha256",
            hashlib.sha256(target.read_bytes()).hexdigest(),
            "--content-file",
            str(candidate),
        )
        self.assertIn("OK replace-file\n", replaced)
        self.assertEqual("new\n", target.read_text())

        resources = ("repo:text-alias-a", "repo:text-alias-b")
        bundle_acquired = self.json_cli(
            "bundle-acquire",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--claim-id",
            "text-alias",
            "--agent-id",
            "agent",
            "--session-id",
            "session",
            "--owner-id",
            "owner",
            "--work-key",
            "text:alias",
        )
        bundle_claim = bundle_acquired["claim"]
        assert isinstance(bundle_claim, dict)
        bundle_token = str(bundle_claim["token"])
        bundle_status = self.text_cli(
            "bundle-status",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
        )
        self.assertIn("OK status-bundle\n", bundle_status)
        bundle_args = (
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--claim-id",
            str(bundle_claim["claimId"]),
            "--token",
            bundle_token,
        )
        bundle_heartbeat = self.text_cli(
            "bundle-heartbeat",
            *bundle_args,
            "--revision",
            str(bundle_claim["revision"]),
            "--operation-id",
            "text-bundle-heartbeat",
        )
        self.assertIn("OK heartbeat-bundle\n", bundle_heartbeat)
        bundle_claim = self.json_cli(
            "status-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
        )["claim"]
        assert isinstance(bundle_claim, dict)
        bundle_exec = self.text_cli(
            "bundle-exec",
            *bundle_args,
            "--revision",
            str(bundle_claim["revision"]),
            "--operation-id",
            "text-bundle-exec",
            "--provider-directory",
            self.home.name,
            "--",
            sys.executable,
            "-c",
            "print('bundle-text-child')",
        )
        self.assertIn("OK exec-bundle\n", bundle_exec)
        self.assertIn('STDOUT\t"bundle-text-child\\n"\n', bundle_exec)
        bundle_claim = self.json_cli(
            "status-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
        )["claim"]
        assert isinstance(bundle_claim, dict)
        bundle_release = self.text_cli(
            "bundle-release",
            *bundle_args,
            "--revision",
            str(bundle_claim["revision"]),
            "--operation-id",
            "text-bundle-release",
            "--reason",
            "text bundle complete",
        )
        self.assertIn("OK release-bundle\n", bundle_release)

        parser_error = self.run_cli(
            "--format",
            "text",
            "status",
            "--resource",
            "repo:text-read",
            "--bad",
        )
        self.assertEqual(64, parser_error.returncode)
        self.assertEqual("ERROR status: invalid-arguments\n", parser_error.stdout)
        self.assertEqual("", parser_error.stderr)

    def test_text_renderers_cover_canonical_bundle_operations(self) -> None:
        empty = self.text_cli("list")
        self.assertEqual("STATE\tRESOURCE\tCLAIM_ID\tOWNER_ID\tEXPIRES_AT\n", empty)

        acquire_text = self.text_cli(
            *(
                "acquire-bundle",
                "--resource",
                "repo:text-canonical-acquire-a",
                "--resource",
                "repo:text-canonical-acquire-b",
                "--claim-id",
                "text-canonical-acquire",
                "--agent-id",
                "agent",
                "--session-id",
                "session",
                "--owner-id",
                "owner",
                "--work-key",
                "text:canonical-acquire",
            )
        )
        self.assertIn("OK acquire-bundle\n", acquire_text)
        self.assertIn("TOKEN\t", acquire_text)

        resources = ("repo:text-canonical-a", "repo:text-canonical-b")
        acquired = self.json_cli(
            "acquire-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--claim-id",
            "text-canonical",
            "--agent-id",
            "agent",
            "--session-id",
            "session",
            "--owner-id",
            "owner",
            "--work-key",
            "text:canonical",
        )
        claim = acquired["claim"]
        assert isinstance(claim, dict)
        token = str(claim["token"])
        bundle_list = self.text_cli("list", "--resource", resources[0])
        self.assertIn(
            '["repo:text-canonical-a","repo:text-canonical-b"]',
            bundle_list,
        )
        bundle_args = (
            "--resource",
            resources[0],
            "--resource",
            resources[1],
            "--claim-id",
            str(claim["claimId"]),
            "--token",
            token,
        )
        inspected = self.text_cli(
            "inspect-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
        )
        self.assertIn("OK status-bundle\n", inspected)
        heartbeat = self.text_cli(
            "heartbeat-bundle",
            *bundle_args,
            "--revision",
            str(claim["revision"]),
            "--operation-id",
            "text-canonical-heartbeat",
        )
        self.assertIn("OK heartbeat-bundle\n", heartbeat)
        self.assertIn("TOKEN\t", heartbeat)
        claim = self.json_cli(
            "status-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
        )["claim"]
        assert isinstance(claim, dict)
        executed = self.text_cli(
            "exec-bundle",
            *bundle_args,
            "--revision",
            str(claim["revision"]),
            "--operation-id",
            "text-canonical-exec",
            "--provider-directory",
            self.home.name,
            "--",
            sys.executable,
            "-c",
            "print('canonical-bundle')",
        )
        self.assertIn("OK exec-bundle\n", executed)
        self.assertIn('STDOUT\t"canonical-bundle\\n"\n', executed)
        claim = self.json_cli(
            "status-bundle",
            "--resource",
            resources[0],
            "--resource",
            resources[1],
        )["claim"]
        assert isinstance(claim, dict)
        released = self.text_cli(
            "release-bundle",
            *bundle_args,
            "--revision",
            str(claim["revision"]),
            "--operation-id",
            "text-canonical-release",
            "--reason",
            "canonical complete",
        )
        self.assertIn("OK release-bundle\n", released)

    def test_text_parser_errors_cover_every_command_and_alias(self) -> None:
        commands = (
            "key",
            "policy",
            "acquire",
            "acquire-bundle",
            "bundle-acquire",
            "status",
            "status-bundle",
            "bundle-status",
            "inspect-bundle",
            "inspect-operation",
            "inspect-operation-bundle",
            "reconcile-operation",
            "reconcile-operation-bundle",
            "gc",
            "list",
            "heartbeat",
            "heartbeat-bundle",
            "bundle-heartbeat",
            "checkpoint",
            "transfer",
            "release",
            "release-bundle",
            "bundle-release",
            "exec",
            "exec-bundle",
            "bundle-exec",
            "replace-file",
        )
        for command in commands:
            with self.subTest(command=command):
                arguments = (
                    (command, "--bad") if command in {"gc", "list"} else (command,)
                )
                failure = self.text_cli(*arguments, expected_code=64)
                self.assertTrue(
                    failure.startswith(f"ERROR {command}: invalid-arguments\n"),
                    failure,
                )
        version_error = self.run_cli("--format", "text", "--version", "--bad")
        self.assertEqual(64, version_error.returncode)
        self.assertEqual("ERROR parse: invalid-arguments\n", version_error.stdout)
        self.assertEqual("", version_error.stderr)


if __name__ == "__main__":
    unittest.main()
