#!/usr/bin/env python3
"""Measure Backlog.md fixture list, view, and watch (JSON output)."""

import argparse
import codecs
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
    utf8 = codecs.getincrementaldecoder("utf-8")()
    decoder = json.JSONDecoder()
    pending = ""
    try:
        # Read raw bytes: TextIO.readline can buffer the entire JSON response and
        # leave select() waiting on an empty pipe despite buffered lines.
        # Emissions may arrive concatenated, so decode one value at a time.
        def receive(deadline):
            nonlocal pending
            while True:
                pending = pending.lstrip()
                if pending:
                    try:
                        value, end = decoder.raw_decode(pending)
                        pending = pending[end:]
                        return value
                    except json.JSONDecodeError:
                        pass
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not select.select([watcher.stdout], [], [], remaining)[0]:
                    raise TimeoutError("watcher did not emit complete JSON")
                chunk = os.read(watcher.stdout.fileno(), 65536)
                if not chunk:
                    raise RuntimeError(f"watcher exited: {watcher.poll()}")
                pending += utf8.decode(chunk)

        receive(time.monotonic() + 20)
        fixture = root / "backlog" / "tasks" / "task-1 - Fixture-00001.md"
        current = fixture.read_text()
        changed = "title: Fixture 00001 changed" in current
        title = "Fixture 00001" if changed else "Fixture 00001 changed"
        start = time.monotonic()
        fixture.write_text(current.replace("title: Fixture 00001 changed", "title: Fixture 00001") if changed else current.replace("title: Fixture 00001", "title: Fixture 00001 changed"))
        deadline = time.monotonic() + 30
        # Only an emission containing the new title measures this change.
        while not any(task.get("id") == "TASK-1" and task.get("title") == title for task in receive(deadline).get("tasks", [])):
            pass
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
    temporary = Path("/tmp").resolve()
    for project in args.projects:
        root = project.resolve()
        # Watch measurement rewrites a fixture file; never touch a real project.
        if not root.is_relative_to(temporary) or not root.name.startswith("worklease-queue-"):
            parser.error("projects must be generated fixtures beneath the OS temp directory with a worklease-queue- prefix")
    for project in args.projects:
        root = project.resolve()
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
