#!/usr/bin/env python3
"""Run reproducible queue microbenchmarks and report sample distributions.

The fixed-size fixtures live in the Go benchmarks; a benchmark invocation is a
new process and cannot reuse provider state from a previous sample. The report
includes peak child RSS and per-operation allocation. It is not a terminal
input-to-paint or full-source-sync measurement.
"""

import json
import os
import platform
import re
import resource
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SAMPLES = int(os.environ.get("QUEUE_BENCH_SAMPLES", "20"))
if not 1 <= SAMPLES <= 100:
    raise SystemExit("QUEUE_BENCH_SAMPLES must be between 1 and 100")
BENCHMARKS = (
    ("./internal/queueui", "BenchmarkQueueInputToRender/items-10000$"),
    ("./internal/queueui", "BenchmarkQueueInputToRender/items-50000$"),
    ("./internal/queueui", "BenchmarkQueueInputToRender/items-100000$"),
    ("./internal/queueui", "BenchmarkQueueWarmFirstView$"),
    ("./internal/queue", "BenchmarkGitHubEnumeration"),
    ("./internal/queueindex", "BenchmarkIndexedSearch10000"),
)
LINE = re.compile(r"^(Benchmark\S+?)-\d+\s+1\s+([\d.]+) ns/op(.*)$")
METRIC = re.compile(r"([\d.]+) ([\w/-]+)")


def percentile(values, percentage):
    ordered = sorted(values)
    # Nearest rank retains measured (rather than interpolated) samples.
    return ordered[max(0, (len(ordered) * percentage + 99) // 100 - 1)]


def main():
    results = {}
    for package, pattern in BENCHMARKS:
        count = min(SAMPLES, 3) if package == "./internal/queueindex" else SAMPLES
        command = ["go", "test", package, "-run", "^$", "-bench", pattern,
                   "-benchtime=1x", f"-count={count}", "-benchmem"]
        if platform.system() == "Darwin":
            command = ["/usr/bin/time", "-l", *command]
        completed = subprocess.run(command, cwd=ROOT, check=True, text=True, capture_output=True)
        resident = re.search(r"(\d+)\s+maximum resident set size", completed.stderr)
        if resident:
            results.setdefault("_rss", {})[pattern] = int(resident.group(1))
        for line in completed.stdout.splitlines():
            match = LINE.match(line)
            if not match:
                continue
            name, nanos, extra = match.groups()
            metrics = {key: float(value) for value, key in METRIC.findall(extra)}
            results.setdefault(name, []).append({"ms": float(nanos) / 1e6, **metrics})
    report = {
        "host": {"machine": platform.machine(), "processor": platform.processor(),
                 "system": platform.system()},
        "samples": SAMPLES,
        "benchmarks": {},
    }
    fixture_path = os.environ.get("QUEUE_BENCH_FIXTURE")
    if fixture_path is None:
        fixture_path = tempfile.mkdtemp(prefix="worklease-queue-bench-", dir="/tmp")
        subprocess.run([sys.executable, str(ROOT / "scripts/backlog-queue-fixture.py"), fixture_path, "10000"],
                       cwd=ROOT, check=True, stdout=subprocess.DEVNULL)
    print(f"Backlog fixture retained at {fixture_path}", file=sys.stderr)
    backlog_run = subprocess.run([sys.executable, str(ROOT / "scripts/queue-backlog-benchmark.py"),
                                  fixture_path, "--samples", str(SAMPLES)],
                                 cwd=ROOT, check=True, text=True, capture_output=True)
    report["backlog"] = json.loads(backlog_run.stdout)
    report["peak_child_rss_bytes"] = resource.getrusage(resource.RUSAGE_CHILDREN).ru_maxrss
    for name, observations in sorted(results.items()):
        if name == "_rss":
            continue
        entry = {"samples": len(observations),
                 "p50_ms": percentile([o["ms"] for o in observations], 50),
                 "p95_ms": percentile([o["ms"] for o in observations], 95),
                 "p99_ms": percentile([o["ms"] for o in observations], 99)}
        for key in ("B/op", "allocs/op", "requests/op", "bytes/op", "index-bytes"):
            if key in observations[0]:
                entry[key] = percentile([o[key] for o in observations], 95)
        entry["processes"] = 1
        if name.startswith("BenchmarkQueueInputToRender/"):
            rss_key = name.replace("BenchmarkQueueInputToRender/", "BenchmarkQueueInputToRender/") + "$"
        else:
            rss_key = name + "$"
        if rss_key in results.get("_rss", {}):
            entry["peak_rss_bytes"] = results["_rss"][rss_key]
        report["benchmarks"][name] = entry
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
