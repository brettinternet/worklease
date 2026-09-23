#!/usr/bin/env python3
"""Report relative queue benchmark changes from two same-runner samples.

No absolute D22 thresholds are applied on CI's shared runners.
"""
import json
import sys

if len(sys.argv) != 3:
    raise SystemExit("usage: queue-benchmark-compare.py BASE.json HEAD.json")
with open(sys.argv[1], encoding="utf-8") as stream:
    baseline = json.load(stream)["benchmarks"]
with open(sys.argv[2], encoding="utf-8") as stream:
    current = json.load(stream)["benchmarks"]
for name, data in sorted(current.items()):
    if name not in baseline:
        continue
    old, new = baseline[name]["p95_ms"], data["p95_ms"]
    if old <= 0:
        continue
    print(f"{name}: p95 {old:.2f} -> {new:.2f} ms ({(new / old - 1) * 100:+.1f}%)")
