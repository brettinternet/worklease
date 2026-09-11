"""Compare complete MCP stdio and CLI lifecycle process costs.

This is a repeatable context-cost proxy, not a performance guarantee.  The MCP
side starts one server process and uses a real ClientSession over stdio; the
CLI side starts one process for each of four equivalent lifecycle operations.
"""

from __future__ import annotations

import asyncio
import json
import os
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any


def _bytes(value: Any) -> int:
    return len(json.dumps(value, separators=(",", ":")).encode())


async def _mcp(home: Path) -> dict[str, int]:
    from mcp import ClientSession, StdioServerParameters
    from mcp.client.stdio import stdio_client

    parameters = StdioServerParameters(
        command=sys.executable,
        args=["-m", "worklease.mcp_server", "--home", str(home)],
        env={**os.environ, "WORKLEASE_AGENT_ID": "benchmark"},
    )
    async with (
        stdio_client(parameters) as (read_stream, write_stream),
        ClientSession(read_stream, write_stream) as session,
    ):
        await session.initialize()
        tools = await session.list_tools()
        results: dict[str, int] = {
            "tools/list": _bytes(tools.model_dump(by_alias=True))
        }
        acquired = await session.call_tool(
            "acquire",
            arguments={
                "resources": ["benchmark-resource"],
                "ttl": 30,
                "auto_heartbeat": False,
            },
        )
        results["acquire"] = _bytes(acquired.model_dump(by_alias=True))
        lease = acquired.structuredContent["lease"]
        for name, arguments in (
            ("heartbeat", {"lease": lease, "ttl": 30}),
            (
                "checkpoint",
                {"lease": lease, "checkpoint": {"step": 1}, "ttl": 30},
            ),
            ("release", {"lease": lease, "reason": "benchmark complete"}),
        ):
            result = await session.call_tool(name, arguments=arguments)
            results[name] = _bytes(result.model_dump(by_alias=True))
    results["processCount"] = 1
    return results


def _cli(home: Path, lease_path: Path) -> dict[str, int]:
    env = {**os.environ, "WORKLEASE_HOME": str(home)}
    commands = [
        [
            "acquire",
            "--resource",
            "benchmark-resource",
            "--agent-id",
            "benchmark",
            "--ttl",
            "30",
            "--lease-file",
            str(lease_path),
        ],
        ["heartbeat", "--lease-file", str(lease_path), "--ttl", "30"],
        [
            "checkpoint",
            "--lease-file",
            str(lease_path),
            "--checkpoint",
            '{"step":1}',
            "--ttl",
            "30",
        ],
        ["release", "--lease-file", str(lease_path), "--reason", "benchmark complete"],
    ]
    result_sizes: dict[str, int] = {}
    for command in commands:
        completed = subprocess.run(
            [sys.executable, "-m", "worklease", "--json", *command],
            env=env,
            check=True,
            capture_output=True,
            text=True,
        )
        result_sizes[command[0]] = len(completed.stdout.encode())
    result_sizes["processCount"] = len(commands)
    return result_sizes


def main() -> None:
    with tempfile.TemporaryDirectory(prefix="worklease-mcp-benchmark-") as directory:
        root = Path(directory)
        mcp_home = root / "mcp"
        cli_home = root / "cli"
        started = time.perf_counter()
        mcp_results = asyncio.run(_mcp(mcp_home))
        mcp_results["wallSeconds"] = round(time.perf_counter() - started, 6)
        started = time.perf_counter()
        cli_results = _cli(cli_home, root / "cli.lease")
        cli_results["wallSeconds"] = round(time.perf_counter() - started, 6)
        print(
            json.dumps(
                {"mcp": mcp_results, "cli": cli_results}, indent=2, sort_keys=True
            )
        )


if __name__ == "__main__":
    main()
