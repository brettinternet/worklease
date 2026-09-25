#!/usr/bin/env python3
"""Probe bd 1.3.0 list/show on disposable Dolt fixtures.

Usage: python3 scripts/beads-queue-benchmark.py /path/to/bd /tmp/worklease-beads-probe-123
The destination must be empty and under the OS temporary directory.
"""
import json
from pathlib import Path
import subprocess
import sys
import time


def command(argv, cwd, **kwargs):
    return subprocess.run(argv, cwd=cwd, check=True, capture_output=True, text=True, **kwargs)


def main():
    binary = Path(sys.argv[1]).resolve()
    root = Path(sys.argv[2]).resolve()
    if not root.is_relative_to(Path('/tmp').resolve()) or not root.name.startswith('worklease-beads-probe-'):
        raise SystemExit('expected an OS-temp path with worklease-beads-probe- prefix')
    if root.exists() and any(root.iterdir()):
        raise SystemExit('destination must be empty')
    if 'bd version 1.3.0 ' not in command([str(binary), 'version'], root.parent).stdout:
        raise SystemExit('expected bd 1.3.0')
    root.mkdir(parents=True, exist_ok=True)
    for count in (102, 1000, 10000):
        project = root / str(count)
        project.mkdir()
        command(['git', 'init', '-q'], project)
        command([str(binary), 'init', '--non-interactive', '--skip-hooks', '--skip-agents', '--prefix', 'probe'], project)
        fixture = project / 'fixture.jsonl'
        with fixture.open('w') as out:
            for i in range(1, count + 1):
                issue = {'id': f'probe-{i:05d}', 'title': f'Fixture {i:05d}', 'status': 'open', 'issue_type': 'task', 'priority': 2}
                if i % 10 != 1:
                    issue['dependencies'] = [{'issue_id': issue['id'], 'depends_on_id': f'probe-{i-1:05d}', 'type': 'blocks'}]
                out.write(json.dumps(issue) + '\n')
        start = time.monotonic()
        command([str(binary), 'import', str(fixture), '--json', '--sandbox'], project)
        imported = round(time.monotonic() - start, 3)
        timings = {}
        for label, argv in [('list', ['list', '--all', '--limit', '0', '--brief']), ('show', ['show', 'probe-00002'])]:
            samples = []
            for _ in range(3):
                start = time.monotonic()
                result = command([str(binary), '--readonly', '--sandbox', '--json', *argv], project)
                samples.append(round(time.monotonic() - start, 3))
            timings[label] = {'seconds': samples, 'records': len(json.loads(result.stdout))}
        print(json.dumps({'count': count, 'importSeconds': imported, **timings}), flush=True)


if __name__ == '__main__':
    main()
