#!/usr/bin/env python3
"""Report relative queue benchmark changes from two same-runner samples.

No absolute D22 thresholds are applied on CI's shared runners.
"""
import json
import sys

if len(sys.argv) != 3:
    raise SystemExit("usage: queue-benchmark-compare.py BASE.json HEAD.json")
with open(sys.argv[1], encoding="utf-8") as stream:
    baseline_report = json.load(stream)
with open(sys.argv[2], encoding="utf-8") as stream:
    current_report = json.load(stream)
baseline = baseline_report["benchmarks"]
current = current_report["benchmarks"]
if "backlog" in baseline_report and "backlog" in current_report:
    for name in ("summary_list", "selected_edge_view"):
        baseline[name] = baseline_report["backlog"][name]
        current[name] = current_report["backlog"][name]
for name, data in sorted(current.items()):
    if name not in baseline:
        continue
    old, new = baseline[name]["p95_ms"], data["p95_ms"]
    if old <= 0:
        continue
    print(f"{name}: p95 {old:.2f} -> {new:.2f} ms ({(new / old - 1) * 100:+.1f}%)")
for size, data in sorted(current_report.get("standalone_memory", {}).items()):
    previous = baseline_report.get("standalone_memory", {}).get(size)
    if previous is None:
        continue
    for metric in ("first_view_ms_p95", "refresh_ms_p95", "peak_rss_bytes_p95"):
        old, new = previous[metric], data[metric]
        if old > 0:
            print(f"standalone {size} {metric}: {old:.2f} -> {new:.2f} ({(new / old - 1) * 100:+.1f}%)")
