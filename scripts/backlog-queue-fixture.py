#!/usr/bin/env python3
"""Create deterministic Backlog.md scale fixtures in an empty OS-temp directory.

Usage: python3 scripts/backlog-queue-fixture.py /tmp/worklease-queue-NNN 1000
The seed record is created with the installed Backlog.md CLI; subsequent records
reuse its supported on-disk format. Never run this against a real project.
"""

import argparse
import os
from pathlib import Path
import re
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("destination", type=Path)
    parser.add_argument("count", type=int, choices=(102, 1000, 10000, 50000, 100000))
    args = parser.parse_args()
    root = args.destination.resolve()
    temporary = Path("/tmp").resolve()
    if not root.is_relative_to(temporary) or not root.name.startswith("worklease-queue-"):
        parser.error("destination must be beneath the OS temp directory with a worklease-queue- prefix")
    if root.exists() and any(root.iterdir()):
        parser.error("destination must be empty; existing fixtures are never overwritten")
    root.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, "BACKLOG_CWD": str(root)}
    subprocess.run(
        ["backlog", "init", "Queue Fixture", "--defaults", "--integration-mode", "none", "--no-git"],
        env=env, check=True, stdout=subprocess.DEVNULL,
    )
    subprocess.run(
        ["backlog", "task", "create", "Fixture seed", "--no-dod-defaults", "--plain"],
        env=env, check=True, stdout=subprocess.DEVNULL,
    )
    tasks = root / "backlog" / "tasks"
    seed = next(tasks.glob("*.md"))
    template = seed.read_text()
    seed.unlink()
    for index in range(1, args.count + 1):
        # Independent ten-item chains model many bounded dependency closures.
        predecessor = f"TASK-{index - 1}" if index % 10 != 1 else None
        record = re.sub(r"id: TASK-1\n", f"id: TASK-{index}\n", template, count=1)
        record = record.replace("title: Fixture seed", f"title: Fixture {index:05d}")
        record = re.sub(r"created_date: '[^']+'", "created_date: '2026-01-01 00:00'", record)
        record = record.replace("dependencies: []", f"dependencies: [{predecessor}]" if predecessor else "dependencies: []")
        record = record.replace("ordinal: 1000", f"ordinal: {index * 1000}")
        (tasks / f"task-{index} - Fixture-{index:05d}.md").write_text(record)
    print(root)


if __name__ == "__main__":
    main()
