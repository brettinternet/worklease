---
id: TASK-68
title: Simplify default worklease list output
status: done
type: enhancement
priority: medium
assignees: ['@pi-01a09351']
labels: [cli, ux]
rank: a8158ed2308158e60
refs: [src/worklease/cli.py, tests/test_cli.py, docs/cli-reference.md]
files: [src/worklease/cli.py, tests/test_cli.py, docs/cli-reference.md]
created: 2026-09-12T01:56:00Z
updated: 2026-09-12T02:03:00Z
---

The compact list still spans five dense columns: shortened lifecycle identifiers remain visually noisy, and absolute repository paths obscure the work item operators are trying to identify. The chosen default should optimize quick human scanning while keeping complete diagnostics available on demand.

## Acceptance criteria

- [x] Default text list shows only state, a concise resource label, and relative lease timing.
- [x] Git-backed resource labels collapse to provider, repository name, and item identifier for the current Backlog.md resource shape.
- [x] `worklease list --full` and JSON output preserve all complete identifiers, resources, and timestamps.
- [x] CLI tests and human-readable output documentation cover the summary and full modes.

## Plan

1. Add a bounded summary resource formatter for Git-backed resources with the existing generic compact fallback. 2. Render the default list as STATE, RESOURCE, and LEASE while retaining the existing five-column --full table and unchanged JSON. 3. Update focused CLI tests and documentation, exercise the real machine output, then run project quality gates.

## Notes

Implemented the selected summary table: default text now shows STATE, concise RESOURCE, and LEASE; Git-backed resource keys collapse to provider:repository:item; relative values read as “left” or “ago.” Kept the existing complete five-column --full view and unchanged JSON payloads. Added POSIX/Windows resource summary coverage and updated list contract tests and documentation.

Validation: focused list formatter tests passed; the complete core suite (303 tests) and SDK suite (19 tests) passed; lint, format-check, typecheck, and staged pre-commit hooks passed. A direct `mise run cli -- list` against the machine’s current claims produced the three-column summary, while `--full` retained all five complete columns and `--json` retained the complete claim object.

## Summary

Simplified default `worklease list` output to STATE, concise RESOURCE, and LEASE. Git-backed keys now read like `backlog-md:worklease:TASK-68`; lifecycle IDs remain available through `--full` and JSON. Verified against this machine’s claims, focused regressions, all 322 tests, lint, formatting, type checking, and pre-commit hooks.
