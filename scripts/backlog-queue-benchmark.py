#!/usr/bin/env python3
"""Measure Backlog.md fixture list, view, and watch (JSON output)."""

import argparse
import json
import os
from pathlib import Path
import re
import select
import subprocess
import time


def percentile(values, fraction):
    values = sorted(values)
    rank = (len(values) - 1) * fraction
    lower = int(rank)
    return round(values[lower] + (values[min(lower + 1, len(values) - 1)] - values[lower]) * (rank - lower), 3)


def run_command(root, command):
    env = {**os.environ, "BACKLOG_CWD": str(root)}
    start = time.monotonic()
    result = subprocess.run(["/usr/bin/time", "-l", "backlog", *command], env=env,
                            stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True, check=True, timeout=120)
    match = re.search(r"(\d+)\s+maximum resident set size", result.stderr)
    if not match:
        raise RuntimeError(result.stderr)
    return {"seconds": round(time.monotonic() - start, 3), "peakRSSBytes": int(match.group(1))}


def watch_latency(root):
    env = {**os.environ, "BACKLOG_CWD": str(root)}
    watcher = subprocess.Popen(["backlog", "task", "list", "--json", "--watch"], env=env,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        # Read raw bytes: TextIO.readline can buffer the entire JSON response and
        # leave select() waiting on an empty pipe despite buffered lines.
        def receive(deadline):
            chunks = []
            while time.monotonic() < deadline:
                if not select.select([watcher.stdout], [], [], max(0, deadline - time.monotonic()))[0]:
                    break
                chunk = os.read(watcher.stdout.fileno(), 65536)
                if not chunk:
                    raise RuntimeError(f"watcher exited: {watcher.poll()}")
                chunks.append(chunk)
                try:
                    return json.loads(b"".join(chunks))
                except json.JSONDecodeError:
                    pass
            raise TimeoutError("watcher did not emit complete JSON")

        receive(time.monotonic() + 20)
        fixture = root / "backlog" / "tasks" / "task-1 - Fixture-00001.md"
        start = time.monotonic()
        current = fixture.read_text()
        fixture.write_text(current.replace("title: Fixture 00001 changed", "title: Fixture 00001") if "changed" in current else current.replace("title: Fixture 00001", "title: Fixture 00001 changed"))
        receive(time.monotonic() + 30)
        return round(time.monotonic() - start, 3)
    finally:
        watcher.kill()
        watcher.wait(timeout=5)
        watcher.stdout.close()
        watcher.stderr.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("projects", type=Path, nargs="+")
    parser.add_argument("--samples", type=int, default=5)
    args = parser.parse_args()
    for root in args.projects:
        count = int(root.name.rsplit("-", 1)[-1])
        output = {"count": count, "samples": args.samples, "commands": {}}
        for label, command in (("list", ["task", "list", "--json"]),
                               ("view", ["task", "view", f"TASK-{count // 2}", "--json"])):
            samples = [run_command(root, command) for _ in range(args.samples)]
            output["commands"][label] = {
                "p50Seconds": percentile([s["seconds"] for s in samples], 0.5),
                "p95Seconds": percentile([s["seconds"] for s in samples], 0.95),
                "p50PeakRSSMiB": percentile([s["peakRSSBytes"] / 1048576 for s in samples], 0.5),
                "p95PeakRSSMiB": percentile([s["peakRSSBytes"] / 1048576 for s in samples], 0.95),
                "raw": samples,
            }
        output["watchChangeToEmitSeconds"] = [watch_latency(root) for _ in range(args.samples)]
        output["watchP50Seconds"] = percentile(output["watchChangeToEmitSeconds"], 0.5)
        output["watchP95Seconds"] = percentile(output["watchChangeToEmitSeconds"], 0.95)
        print(json.dumps(output), flush=True)


if __name__ == "__main__":
    main()
