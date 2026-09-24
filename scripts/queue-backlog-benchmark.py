#!/usr/bin/env python3
"""Measure the real Backlog.md CLI against an existing 10,000-task fixture.

Generate an empty OS-temp fixture first with scripts/backlog-queue-fixture.py.
This runner is read-only; it never edits or cleans up the fixture.
"""

import argparse
import json
import os
from pathlib import Path
import platform
import resource
import subprocess
import time


def percentile(values, percentage):
    ordered = sorted(values)
    return ordered[max(0, (len(ordered) * percentage + 99) // 100 - 1)]


def run(fixture, args):
    started = time.perf_counter_ns()
    result = subprocess.run(
        ["backlog", *args], cwd=fixture, env={**os.environ, "BACKLOG_CWD": str(fixture)},
        capture_output=True, check=True,
    )
    return (time.perf_counter_ns() - started) / 1e6, len(result.stdout) + len(result.stderr)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("fixture", type=Path)
    parser.add_argument("--samples", type=int, default=20)
    options = parser.parse_args()
    fixture = options.fixture.resolve()
    if not fixture.is_dir() or not (fixture / "backlog" / "tasks").is_dir():
        parser.error("expected an existing Backlog.md fixture with backlog/tasks")
    if not 1 <= options.samples <= 100:
        parser.error("samples must be between 1 and 100")
    fixture_count = len(list((fixture / "backlog" / "tasks").glob("*.md")))
    if fixture_count != 10000:
        parser.error(f"expected 10,000 fixture tasks; found {fixture_count}")
    times, sizes = [], []
    for _ in range(options.samples):
        elapsed, size = run(fixture, ["task", "list", "--json"])
        times.append(elapsed)
        sizes.append(size)
    # Report a selected edge read separately; unlike list, it does not scale to
    # a complete dependency graph without one subprocess per task.
    edge_times, edge_sizes = [], []
    for _ in range(options.samples):
        elapsed, size = run(fixture, ["task", "view", "TASK-2", "--json"])
        edge_times.append(elapsed)
        edge_sizes.append(size)
    disk = sum(p.stat().st_size for p in (fixture / "backlog").rglob("*") if p.is_file())
    def row(values, transferred):
        return {"p50_ms": percentile(values, 50), "p95_ms": percentile(values, 95),
                "p99_ms": percentile(values, 99), "processes_per_operation": 1,
                "provider_api_requests": 0, "bytes_transferred_p95": percentile(transferred, 95),
                "provider_quota_cost": 0}
    print(json.dumps({"host": {"machine": platform.machine(), "system": platform.system()},
                      "samples": options.samples, "fixture_tasks": fixture_count,
                      "fixture_disk_bytes": disk, "peak_child_rss_bytes": resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss,
                      "summary_list": row(times, sizes), "selected_edge_view": row(edge_times, edge_sizes)}, indent=2))


if __name__ == "__main__":
    main()
