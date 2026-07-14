from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import unittest
from importlib.resources import files
from typing import Any

from jsonschema import Draft202012Validator, RefResolver

READ_ONLY = {
    "parse",
    "version",
    "key",
    "policy-list",
    "policy-describe",
    "status",
    "status-verbose",
    "bundle-status",
    "inspect-bundle",
    "status-bundle",
    "inspect-operation",
    "reconcile-operation",
    "list",
}

RELEASED_OPERATIONS = {
    "parse",
    "version",
    "key",
    "policy-list",
    "policy-describe",
    "acquire",
    "bundle-acquire",
    "acquire-bundle",
    "status",
    "status-verbose",
    "bundle-status",
    "inspect-bundle",
    "status-bundle",
    "inspect-operation",
    "reconcile-operation",
    "list",
    "heartbeat",
    "checkpoint",
    "transfer",
    "release",
    "bundle-heartbeat",
    "heartbeat-bundle",
    "bundle-release",
    "release-bundle",
    "exec",
    "bundle-exec",
    "exec-bundle",
    "replace-file",
    "gc",
}


class SchemaContractTests(unittest.TestCase):
    def setUp(self) -> None:
        schema_root = files("worklease").joinpath("schemas", "v1")
        self.common = json.loads(schema_root.joinpath("common.json").read_text())
        self.commands = json.loads(schema_root.joinpath("commands.json").read_text())
        self.validator = Draft202012Validator(
            self.commands,
            resolver=RefResolver(
                "https://worklease.dev/schemas/v1/commands.json",
                self.commands,
                store={"https://worklease.dev/schemas/v1/common.json": self.common},
            ),
        )
        self.index = json.loads(schema_root.joinpath("index.json").read_text())
        self.home = tempfile.TemporaryDirectory()
        self.environment = os.environ.copy()
        self.environment["WORKLEASE_HOME"] = self.home.name

    def tearDown(self) -> None:
        self.home.cleanup()

    def run_cli(self, *arguments: str) -> dict[str, Any]:
        result = subprocess.run(
            [sys.executable, "-m", "worklease.cli", *arguments],
            check=False,
            capture_output=True,
            text=True,
            env=self.environment,
        )
        self.assertEqual("", result.stderr)
        return json.loads(result.stdout)

    def assert_matches_commands_schema(self, payload: dict[str, Any]) -> None:
        errors = list(self.validator.iter_errors(payload))
        self.assertEqual([], errors, [error.message for error in errors])
        self.assertEqual(1, payload.get("schemaVersion"))
        operation = payload.get("operation")
        self.assertIn(operation, RELEASED_OPERATIONS)
        self.assertIn(
            operation, self.commands["allOf"][1]["properties"]["operation"]["enum"]
        )
        self.assertIsInstance(payload.get("ok"), bool)
        if payload["ok"] is False:
            self.assertIsInstance(payload.get("error"), str)
        claim = payload.get("claim")
        if isinstance(claim, dict):
            self.assertIn("claimId", claim)
            self.assertIn("revision", claim)
            if operation in READ_ONLY:
                self.assertNotIn("token", claim)
            else:
                self.assertIn("token", claim)
        claims = payload.get("claims")
        if isinstance(claims, list):
            for item in claims:
                self.assertIsInstance(item, dict)
                self.assertNotIn("token", item)

    def test_index_lists_every_released_operation_and_artifacts_are_parseable(
        self,
    ) -> None:
        self.assertEqual(1, self.index["properties"]["version"]["const"])
        commands = self.index["properties"]["commands"]["items"]["enum"]
        self.assertEqual(RELEASED_OPERATIONS, set(commands))
        for path in files("worklease").joinpath("schemas", "v1").iterdir():
            if path.name.endswith(".json"):
                json.loads(path.read_text())

    def test_success_and_error_payloads_match_schema_and_redact_tokens(self) -> None:
        success = self.run_cli(
            "key", "--provider", "linear", "--source", "team", "--item", "ITEM-1"
        )
        self.assert_matches_commands_schema(success)

        acquired = self.run_cli(
            "acquire",
            "--resource",
            "schema:resource",
            "--claim-id",
            "schema-claim",
            "--agent-id",
            "schema-agent",
            "--session-id",
            "schema-session",
            "--owner-id",
            "schema-owner",
            "--work-key",
            "schema-work",
        )
        self.assert_matches_commands_schema(acquired)
        token = acquired["claim"]["token"]

        verbose = self.run_cli("status", "--resource", "schema:resource", "--verbose")
        self.assert_matches_commands_schema(verbose)
        self.assertEqual("status-verbose", verbose["operation"])
        self.assertNotIn(token, json.dumps(verbose))

        error = self.run_cli(
            "key", "--provider", "unknown", "--source", "team", "--item", "ITEM-1"
        )
        self.assert_matches_commands_schema(error)
        gc = self.run_cli("gc")
        self.assert_matches_commands_schema(gc)


if __name__ == "__main__":
    unittest.main()
