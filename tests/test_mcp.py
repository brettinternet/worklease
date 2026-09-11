from __future__ import annotations

import asyncio
import contextlib
import json
import os
import stat
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from importlib.resources import files
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator, RefResolver
from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

from worklease import LeaseError, LeaseStore, agent_instructions, read_lease_file
from worklease.mcp_server import create_server


class MCPTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self) -> None:
        self.directory = tempfile.TemporaryDirectory()
        self.home = Path(self.directory.name)
        self.server = create_server(self.home, agent_id="test-agent")
        schema_root = files("worklease").joinpath("schemas", "v1")
        self.mcp_schema = json.loads(schema_root.joinpath("mcp.json").read_text())
        common = json.loads(schema_root.joinpath("common.json").read_text())
        commands = json.loads(schema_root.joinpath("commands.json").read_text())
        self.mcp_validator = Draft202012Validator(self.mcp_schema)
        self.commands_validator = Draft202012Validator(
            commands,
            resolver=RefResolver(
                "https://worklease.dev/schemas/v1/commands.json",
                commands,
                store={
                    "https://worklease.dev/schemas/v1/common.json": common,
                },
            ),
        )

    async def asyncTearDown(self) -> None:
        await self.server.shutdown()
        self.directory.cleanup()

    def run_cli(self, *arguments: str) -> tuple[int, dict[str, Any]]:
        result = subprocess.run(
            [sys.executable, "-m", "worklease", "--json", *arguments],
            env={**os.environ, "WORKLEASE_HOME": str(self.home)},
            check=False,
            capture_output=True,
            text=True,
        )
        self.assertEqual("", result.stderr)
        return result.returncode, json.loads(result.stdout)

    def handle_path(self, reference: str) -> Path:
        return self.home / "mcp-leases" / f"{reference}.lease"

    def assert_token_free(self, value: Any, *secrets: str) -> None:
        encoded = json.dumps(value, sort_keys=True)
        self.assertNotIn('"token"', encoded.lower())
        for secret in secrets:
            self.assertNotIn(secret, encoded)

    def assert_result(self, result: Any, *, error: bool | None = None) -> None:
        if error is not None:
            self.assertEqual(error, result.isError)
        payload = result.structuredContent
        self.assertIsInstance(payload, dict)
        self.assertEqual([], list(self.mcp_validator.iter_errors(payload)))
        self.assertEqual([], list(self.commands_validator.iter_errors(payload)))
        self.assert_token_free(payload, *(self._known_secrets()))
        for content in result.content:
            text = getattr(content, "text", "")
            for secret in self._known_secrets():
                self.assertNotIn(secret, text)

    def _known_secrets(self) -> list[str]:
        secrets: list[str] = []
        for path in (self.home / "mcp-leases").glob("*.lease"):
            with contextlib.suppress(LeaseError):
                secrets.append(read_lease_file(path).token)
        return secrets

    async def acquire(
        self,
        resources: list[str],
        *,
        ttl: float = 10,
        auto_heartbeat: bool = False,
        max_hold: float | None = None,
    ) -> Any:
        arguments: dict[str, Any] = {
            "resources": resources,
            "ttl": ttl,
            "auto_heartbeat": auto_heartbeat,
        }
        if max_hold is not None:
            arguments["max_hold"] = max_hold
        result = await self.server.call("acquire", arguments)
        self.assert_result(result, error=False)
        return result

    async def test_stdio_tool_surface_schemas_instructions_and_protocol_errors(
        self,
    ) -> None:
        stderr = tempfile.TemporaryFile(mode="w+")  # noqa: SIM115
        self.addCleanup(stderr.close)
        parameters = StdioServerParameters(
            command=sys.executable,
            args=["-m", "worklease.mcp_server", "--home", str(self.home)],
            env={**os.environ, "WORKLEASE_AGENT_ID": "stdio-test"},
        )
        async with (
            stdio_client(parameters, errlog=stderr) as (read_stream, write_stream),
            ClientSession(read_stream, write_stream) as session,
        ):
            initialized = await session.initialize()
            expected_instructions = "\n".join(
                (
                    "Worklease agent instructions:",
                    *agent_instructions("loop"),
                    "",
                    "Safety:",
                    *agent_instructions("safety"),
                )
            )
            self.assertEqual(expected_instructions, initialized.instructions)
            listed = await session.list_tools()
            self.assertEqual(
                [tool.name for tool in listed.tools],
                [
                    "key",
                    "acquire",
                    "status",
                    "list",
                    "heartbeat",
                    "checkpoint",
                    "release",
                ],
            )
            for tool in listed.tools:
                self.assertTrue(tool.description)
                self.assertEqual(self.mcp_schema, tool.outputSchema)
            annotations = {tool.name: tool.annotations for tool in listed.tools}
            assert annotations["key"] is not None
            assert annotations["status"] is not None
            assert annotations["list"] is not None
            assert annotations["release"] is not None
            self.assertTrue(annotations["key"].readOnlyHint)
            self.assertTrue(annotations["key"].idempotentHint)
            self.assertTrue(annotations["status"].readOnlyHint)
            self.assertTrue(annotations["list"].readOnlyHint)
            self.assertTrue(annotations["release"].destructiveHint)

            invalid = await session.call_tool("acquire", {"resources": []})
            self.assert_result(invalid, error=True)
            for field, value, reason in (
                ("ttl", True, "invalid-ttl"),
                ("ttl", "1", "invalid-ttl"),
                ("wait_timeout", True, "invalid-wait-timeout"),
                ("wait_timeout", "1", "invalid-wait-timeout"),
                ("max_hold", True, "invalid-max-hold"),
                ("max_hold", "1", "invalid-max-hold"),
            ):
                typed = await session.call_tool(
                    "acquire", {"resources": [f"typed-{field}"], field: value}
                )
                self.assert_result(typed, error=True)
                assert typed.structuredContent is not None
                self.assertEqual(reason, typed.structuredContent["error"])
            unknown = await session.call_tool("not-a-worklease-tool", {})
            self.assertTrue(unknown.isError)
        stderr.seek(0)
        self.assertNotIn("token", stderr.read().lower())

    async def test_every_tool_success_and_failure_matches_published_schemas(
        self,
    ) -> None:
        key = await self.server.call(
            "key", {"provider": "github", "source": "owner/repo", "item": "1"}
        )
        self.assert_result(key, error=False)
        acquired = await self.acquire(["schema-resource"])
        reference = acquired.structuredContent["lease"]
        for name, arguments in (
            ("status", {"resources": ["schema-resource"]}),
            ("list", {}),
            ("heartbeat", {"lease": reference, "ttl": 10}),
            (
                "checkpoint",
                {"lease": reference, "checkpoint": {"step": 1}, "ttl": 10},
            ),
            ("release", {"lease": reference, "reason": "schema complete"}),
        ):
            self.assert_result(await self.server.call(name, arguments), error=False)

        failures = (
            ("key", {"provider": "", "source": "x", "item": "1"}),
            ("acquire", {"resources": []}),
            ("status", {"resources": []}),
            ("list", {"resource": ""}),
            ("heartbeat", {"lease": "bad"}),
            ("checkpoint", {"lease": "bad", "checkpoint": {}}),
            ("release", {"lease": "bad", "reason": "x"}),
        )
        for name, arguments in failures:
            self.assert_result(await self.server.call(name, arguments), error=True)

    async def test_private_handles_restart_and_reference_validation(self) -> None:
        acquired = await self.acquire(["restart"])
        reference = acquired.structuredContent["lease"]
        path = self.handle_path(reference)
        self.assertTrue(path.is_file())
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(path.parent.stat().st_mode), 0o700)
        self.assertEqual(reference + ".lease", path.name)

        await self.server.shutdown()
        assert create_server is not None
        restarted = create_server(self.home, agent_id="fresh-session")
        result = await restarted.call("heartbeat", {"lease": reference, "ttl": 10})
        self.assert_result(result, error=False)
        self.assertEqual("stopped", result.structuredContent["autoHeartbeat"])

        blocked = await restarted.call(
            "acquire",
            {"resources": ["restart"], "ttl": 10, "auto_heartbeat": False},
        )
        self.assert_result(blocked, error=True)
        self.assertEqual("already-claimed", blocked.structuredContent["error"])
        await restarted.shutdown()
        self.server = create_server(self.home, agent_id="test-agent")

        for malformed in ("../x", reference + "/x", "wl1-nope", 3):
            failed = await self.server.call(
                "heartbeat", {"lease": malformed, "ttl": 10}
            )
            self.assert_result(failed, error=True)
            self.assertEqual(
                "invalid-lease-reference", failed.structuredContent["error"]
            )

        with tempfile.TemporaryDirectory() as foreign_home:
            foreign = create_server(foreign_home, agent_id="foreign")
            foreign_acquire = await foreign.call(
                "acquire",
                {"resources": ["foreign"], "ttl": 10, "auto_heartbeat": False},
            )
            foreign_ref = foreign_acquire.structuredContent["lease"]
            failed = await self.server.call(
                "heartbeat", {"lease": foreign_ref, "ttl": 10}
            )
            self.assert_result(failed, error=True)
            self.assertEqual("foreign-authority", failed.structuredContent["error"])
            await foreign.shutdown()

    async def test_singleton_cli_mcp_interoperability_and_contention(self) -> None:
        cli_handle = self.home / "cli-singleton.lease"
        code, acquired = self.run_cli(
            "acquire",
            "--resource",
            "cli-singleton",
            "--agent-id",
            "cli",
            "--lease-file",
            str(cli_handle),
        )
        self.assertEqual(0, code)
        self.assertTrue(acquired["ok"])
        visible = await self.server.call("status", {"resources": ["cli-singleton"]})
        self.assertEqual("active", visible.structuredContent["state"])
        conflict = await self.server.call(
            "acquire",
            {"resources": ["cli-singleton"], "auto_heartbeat": False},
        )
        self.assert_result(conflict, error=True)
        self.assertEqual("already-claimed", conflict.structuredContent["error"])
        self.run_cli(
            "release",
            "--lease-file",
            str(cli_handle),
            "--reason",
            "handoff",
        )

        mcp = await self.acquire(["mcp-singleton"])
        reference = mcp.structuredContent["lease"]
        path = self.handle_path(reference)
        code, status = self.run_cli("status", "--resource", "mcp-singleton")
        self.assertEqual(0, code)
        self.assertEqual("active", status["state"])
        code, _ = self.run_cli("heartbeat", "--lease-file", str(path), "--ttl", "10")
        self.assertEqual(0, code)
        checkpoint = await self.server.call(
            "checkpoint",
            {"lease": reference, "checkpoint": {"from": "mcp"}, "ttl": 10},
        )
        self.assert_result(checkpoint, error=False)
        code, released = self.run_cli(
            "release", "--lease-file", str(path), "--reason", "cli release"
        )
        self.assertEqual(0, code)
        self.assertTrue(released["ok"])

    async def test_bundle_cli_mcp_interoperability_checkpoint_and_contention(
        self,
    ) -> None:
        cli_handle = self.home / "cli-bundle.lease"
        code, _ = self.run_cli(
            "acquire-bundle",
            "--resource",
            "cli-bundle-a",
            "--resource",
            "cli-bundle-b",
            "--agent-id",
            "cli",
            "--lease-file",
            str(cli_handle),
        )
        self.assertEqual(0, code)
        visible = await self.server.call(
            "status", {"resources": ["cli-bundle-a", "cli-bundle-b"]}
        )
        self.assertEqual("active", visible.structuredContent["state"])
        conflict = await self.server.call(
            "acquire",
            {
                "resources": ["cli-bundle-a", "cli-bundle-b"],
                "auto_heartbeat": False,
            },
        )
        self.assert_result(conflict, error=True)
        self.run_cli(
            "release-bundle",
            "--lease-file",
            str(cli_handle),
            "--reason",
            "handoff",
        )

        mcp = await self.acquire(["mcp-bundle-a", "mcp-bundle-b"])
        reference = mcp.structuredContent["lease"]
        path = self.handle_path(reference)
        code, status = self.run_cli(
            "status-bundle",
            "--resource",
            "mcp-bundle-a",
            "--resource",
            "mcp-bundle-b",
        )
        self.assertEqual(0, code)
        self.assertEqual("active", status["state"])
        verbose = await self.server.call(
            "status",
            {"resources": ["mcp-bundle-a", "mcp-bundle-b"], "verbose": True},
        )
        self.assert_result(verbose, error=False)
        self.assertEqual(
            ["mcp-bundle-a", "mcp-bundle-b"],
            verbose.structuredContent["resources"],
        )
        self.assertIn("unknownOperations", verbose.structuredContent)
        code, _ = self.run_cli(
            "heartbeat-bundle", "--lease-file", str(path), "--ttl", "10"
        )
        self.assertEqual(0, code)
        checkpoint = await self.server.call(
            "checkpoint",
            {"lease": reference, "checkpoint": {"bundle": 1}, "ttl": 10},
        )
        self.assert_result(checkpoint, error=False)
        self.assertEqual(
            ["mcp-bundle-a", "mcp-bundle-b"], checkpoint.structuredContent["resources"]
        )
        code, _ = self.run_cli(
            "release-bundle", "--lease-file", str(path), "--reason", "cli release"
        )
        self.assertEqual(0, code)

    async def test_wait_timeout_is_bounded_and_retries_bundles(self) -> None:
        invalid = await self.server.call(
            "acquire", {"resources": ["wait-cap"], "wait_timeout": 60.1}
        )
        self.assert_result(invalid, error=True)
        self.assertEqual("invalid-wait-timeout", invalid.structuredContent["error"])
        self.assertEqual(60, invalid.structuredContent["maximumInclusive"])

        holder = await self.acquire(["wait-a", "wait-b"])
        holder_ref = holder.structuredContent["lease"]
        assert create_server is not None
        waiter = create_server(self.home, agent_id="waiter")

        async def release_holder() -> None:
            await asyncio.sleep(0.08)
            await self.server.call(
                "release", {"lease": holder_ref, "reason": "allow waiter"}
            )

        release_task = asyncio.create_task(release_holder())
        waited = await waiter.call(
            "acquire",
            {
                "resources": ["wait-a", "wait-b"],
                "wait_timeout": 0.5,
                "auto_heartbeat": False,
            },
        )
        await release_task
        self.assert_result(waited, error=False)
        await waiter.shutdown()

    async def test_token_redaction_checkpoint_rejection_and_failure_paths(self) -> None:
        acquired = await self.acquire(["redaction"])
        reference = acquired.structuredContent["lease"]
        path = self.handle_path(reference)
        secret = read_lease_file(path).token
        self.assert_token_free(acquired.model_dump(), secret)

        for result in (
            await self.server.call("heartbeat", {"lease": reference, "ttl": 10}),
            await self.server.call(
                "checkpoint",
                {"lease": reference, "checkpoint": {"safe": True}, "ttl": 10},
            ),
        ):
            self.assert_result(result, error=False)
            self.assert_token_free(result.model_dump(), secret)

        rejected = await self.server.call(
            "checkpoint",
            {
                "lease": reference,
                "checkpoint": {"nested": {"token": secret}},
                "ttl": 10,
            },
        )
        self.assert_result(rejected, error=True)
        self.assertEqual("secret-in-checkpoint", rejected.structuredContent["error"])
        self.assert_token_free(rejected.model_dump(), secret)

        contents = json.loads(path.read_text())
        contents["token"] = "wrong-bearer"
        path.write_text(json.dumps(contents), encoding="utf-8")
        path.chmod(0o600)
        invalid = await self.server.call("heartbeat", {"lease": reference, "ttl": 10})
        self.assert_result(invalid, error=True)
        self.assertEqual("invalid-token", invalid.structuredContent["error"])
        self.assert_token_free(invalid.model_dump(), secret, "wrong-bearer")

    async def test_cli_checkpoint_cannot_expose_lease_contents_through_mcp(
        self,
    ) -> None:
        acquired = await self.acquire(["malicious-checkpoint"])
        reference = acquired.structuredContent["lease"]
        path = self.handle_path(reference)
        raw_handle = path.read_text(encoding="utf-8").strip()
        secret = read_lease_file(path).token
        code, _ = self.run_cli(
            "checkpoint",
            "--lease-file",
            str(path),
            "--checkpoint",
            json.dumps({"note": raw_handle}),
            "--ttl",
            "10",
        )
        self.assertEqual(0, code)

        for result in (
            await self.server.call("status", {"resources": ["malicious-checkpoint"]}),
            await self.server.call("list", {}),
            await self.server.call("heartbeat", {"lease": reference, "ttl": 10}),
        ):
            self.assert_result(result, error=False)
            self.assert_token_free(result.model_dump(), secret)
            self.assertNotIn(raw_handle, json.dumps(result.model_dump()))
            self.assertNotIn("checkpoint", result.structuredContent)
            claim = result.structuredContent.get("claim")
            if isinstance(claim, dict):
                self.assertNotIn("checkpoint", claim)

    async def test_concurrent_mutation_returns_stable_busy_error(self) -> None:
        acquired = await self.acquire(["busy"])
        reference = acquired.structuredContent["lease"]
        original = self.server.store.heartbeat
        entered = threading.Event()
        proceed = threading.Event()

        def slow_heartbeat(request: Any) -> dict[str, Any]:
            entered.set()
            proceed.wait(timeout=2)
            return original(request)

        self.server.store.heartbeat = slow_heartbeat  # type: ignore[method-assign]
        first_task = asyncio.create_task(
            self.server.call("heartbeat", {"lease": reference, "ttl": 10})
        )
        self.assertTrue(await asyncio.to_thread(entered.wait, 1))
        second = await self.server.call(
            "checkpoint",
            {"lease": reference, "checkpoint": {"step": 2}, "ttl": 10},
        )
        self.assert_result(second, error=True)
        self.assertEqual("lease-busy", second.structuredContent["error"])
        proceed.set()
        self.assert_result(await first_task, error=False)
        self.server.store.heartbeat = original  # type: ignore[method-assign]

    async def test_automatic_heartbeat_runs_before_half_ttl(self) -> None:
        started = time.time()
        acquired = await self.acquire(
            ["automatic"], ttl=0.3, auto_heartbeat=True, max_hold=1
        )
        reference = acquired.structuredContent["lease"]
        self.assertEqual("active", acquired.structuredContent["autoHeartbeat"])
        await asyncio.sleep(0.14)
        status = self.server.store.status("automatic")
        self.assertGreaterEqual(status["claim"]["revision"], 2)
        heartbeat_at = status["claim"]["heartbeatAt"]
        parsed = (
            __import__("datetime")
            .datetime.fromisoformat(heartbeat_at.replace("Z", "+00:00"))
            .timestamp()
        )
        self.assertLess(parsed - started, 0.15)
        await self.server.call("release", {"lease": reference, "reason": "done"})

    async def test_explicit_shorter_ttls_rearm_automatic_heartbeat(self) -> None:
        heartbeat_claim = await self.acquire(
            ["short-heartbeat"], ttl=0.5, auto_heartbeat=True, max_hold=1
        )
        heartbeat_ref = heartbeat_claim.structuredContent["lease"]
        renewed = await self.server.call(
            "heartbeat", {"lease": heartbeat_ref, "ttl": 0.1}
        )
        self.assert_result(renewed, error=False)
        await asyncio.sleep(0.13)
        self.assertEqual("active", self.server.store.status("short-heartbeat")["state"])

        checkpoint_claim = await self.acquire(
            ["short-checkpoint"], ttl=0.5, auto_heartbeat=True, max_hold=1
        )
        checkpoint_ref = checkpoint_claim.structuredContent["lease"]
        checkpointed = await self.server.call(
            "checkpoint",
            {"lease": checkpoint_ref, "checkpoint": {"step": 1}, "ttl": 0.1},
        )
        self.assert_result(checkpointed, error=False)
        await asyncio.sleep(0.13)
        self.assertEqual(
            "active", self.server.store.status("short-checkpoint")["state"]
        )

    async def test_sub_millisecond_ttl_has_no_heartbeat_sleep_floor(self) -> None:
        acquired = await self.acquire(
            ["tiny-ttl"], ttl=0.1, auto_heartbeat=True, max_hold=1
        )
        reference = acquired.structuredContent["lease"]
        runtime = self.server._runtimes[reference]
        renewed = asyncio.Event()
        original = self.server._renew

        async def observe_renewal(observed_reference: str, ttl: float) -> None:
            self.assertEqual(reference, observed_reference)
            self.assertEqual(0.001, ttl)
            runtime.status = "stopped"
            renewed.set()

        self.server._renew = observe_renewal  # type: ignore[method-assign]
        started = asyncio.get_running_loop().time()
        runtime.ttl = 0.001
        runtime.wake.set()
        await asyncio.wait_for(renewed.wait(), timeout=0.02)
        self.assertLess(asyncio.get_running_loop().time() - started, 0.009)
        self.server._renew = original  # type: ignore[method-assign]
        await self.server.call("release", {"lease": reference, "reason": "done"})

    async def test_max_hold_stops_without_release_then_claim_expires(self) -> None:
        acquired = await self.acquire(
            ["max-hold"], ttl=0.2, auto_heartbeat=True, max_hold=0.05
        )
        reference = acquired.structuredContent["lease"]
        await asyncio.sleep(0.08)
        self.assertEqual("stopped", self.server._runtimes[reference].status)
        await asyncio.sleep(0.15)
        status = self.server.store.status("max-hold")
        self.assertEqual("expired", status["state"])
        self.assertIsNotNone(status["claim"])

    async def test_cancellation_and_shutdown_stop_without_release(self) -> None:
        cancelled = await self.acquire(
            ["cancelled"], ttl=0.15, auto_heartbeat=True, max_hold=1
        )
        cancelled_ref = cancelled.structuredContent["lease"]
        task = self.server._runtimes[cancelled_ref].task
        assert task is not None
        task.cancel()
        await asyncio.gather(task, return_exceptions=True)
        await asyncio.sleep(0.18)
        self.assertEqual("expired", self.server.store.status("cancelled")["state"])

        shutdown = await self.acquire(
            ["shutdown"], ttl=0.15, auto_heartbeat=True, max_hold=1
        )
        shutdown_ref = shutdown.structuredContent["lease"]
        await self.server.shutdown()
        await asyncio.sleep(0.18)
        status = LeaseStore(self.home).status("shutdown")
        self.assertEqual("expired", status["state"])
        self.assertTrue(self.handle_path(shutdown_ref).exists())
        assert create_server is not None
        self.server = create_server(self.home, agent_id="test-agent")

    async def test_stdio_eof_stops_renewal_without_releasing(self) -> None:
        parameters = StdioServerParameters(
            command=sys.executable,
            args=["-m", "worklease.mcp_server", "--home", str(self.home)],
            env={**os.environ, "WORKLEASE_AGENT_ID": "eof-test"},
        )
        stderr = tempfile.TemporaryFile(mode="w+")  # noqa: SIM115
        self.addCleanup(stderr.close)
        async with (
            stdio_client(parameters, errlog=stderr) as (
                read_stream,
                write_stream,
            ),
            ClientSession(read_stream, write_stream) as session,
        ):
            await session.initialize()
            acquired = await session.call_tool(
                "acquire",
                {
                    "resources": ["stdio-eof"],
                    "ttl": 0.15,
                    "auto_heartbeat": True,
                    "max_hold": 1,
                },
            )
            self.assertFalse(acquired.isError)
        await asyncio.sleep(0.2)
        status = self.server.store.status("stdio-eof")
        self.assertEqual("expired", status["state"])
        self.assertIsNotNone(status["claim"])

    async def test_restart_does_not_renew_existing_handle_and_list_cleans_it(
        self,
    ) -> None:
        acquired = await self.acquire(["restart-expiry"], ttl=0.12)
        reference = acquired.structuredContent["lease"]
        path = self.handle_path(reference)
        await self.server.shutdown()
        assert create_server is not None
        self.server = create_server(self.home, agent_id="restart")
        await asyncio.sleep(0.15)
        self.assertEqual("expired", self.server.store.status("restart-expiry")["state"])
        listed = await self.server.call("list", {})
        self.assert_result(listed, error=False)
        self.assertFalse(path.exists())

    async def test_release_stops_heartbeat_and_removes_handle_only_on_success(
        self,
    ) -> None:
        acquired = await self.acquire(["release"], auto_heartbeat=True)
        reference = acquired.structuredContent["lease"]
        path = self.handle_path(reference)
        failed = await self.server.call("release", {"lease": reference, "reason": ""})
        self.assert_result(failed, error=True)
        self.assertTrue(path.exists())
        released = await self.server.call(
            "release", {"lease": reference, "reason": "provider verified"}
        )
        self.assert_result(released, error=False)
        self.assertEqual("stopped", released.structuredContent["autoHeartbeat"])
        self.assertFalse(path.exists())
        self.assertEqual("free", self.server.store.status("release")["state"])

    async def test_explicit_home_and_environment_use_cli_precedence(self) -> None:
        with tempfile.TemporaryDirectory() as environment_home:
            previous = os.environ.get("WORKLEASE_HOME")
            os.environ["WORKLEASE_HOME"] = environment_home
            try:
                assert create_server is not None
                environment_server = create_server(agent_id="environment")
                self.assertEqual(
                    Path(environment_home).resolve(), environment_server.store.home
                )
                await environment_server.shutdown()
                blank_server = create_server("", agent_id="blank")
                self.assertEqual(
                    Path(environment_home).resolve(), blank_server.store.home
                )
                await blank_server.shutdown()
                explicit = create_server(self.home / "explicit", agent_id="explicit")
                self.assertEqual(
                    (self.home / "explicit").resolve(), explicit.store.home
                )
                await explicit.shutdown()
            finally:
                if previous is None:
                    os.environ.pop("WORKLEASE_HOME", None)
                else:
                    os.environ["WORKLEASE_HOME"] = previous
