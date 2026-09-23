---
id: TASK-68
title: Simplify default worklease list output
status: Done
assignee:
  - '@pi-01a09351'
created_date: '2026-09-12 01:56'
updated_date: '2026-09-12 02:03'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
  - docs/cli-reference.md
modified_files:
  - src/worklease/cli.py
  - tests/test_cli.py
  - docs/cli-reference.md
priority: medium
type: enhancement
ordinal: 72000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The compact list still spans five dense columns: shortened lifecycle identifiers remain visually noisy, and absolute repository paths obscure the work item operators are trying to identify. The chosen default should optimize quick human scanning while keeping complete diagnostics available on demand.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Default text list shows only state, a concise resource label, and relative lease timing.
- [x] #2 Git-backed resource labels collapse to provider, repository name, and item identifier for the current Backlog.md resource shape.
- [x] #3 `worklease list --full` and JSON output preserve all complete identifiers, resources, and timestamps.
- [x] #4 CLI tests and human-readable output documentation cover the summary and full modes.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a bounded summary resource formatter for Git-backed resources with the existing generic compact fallback. 2. Render the default list as STATE, RESOURCE, and LEASE while retaining the existing five-column --full table and unchanged JSON. 3. Update focused CLI tests and documentation, exercise the real machine output, then run project quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the selected summary table: default text now shows STATE, concise RESOURCE, and LEASE; Git-backed resource keys collapse to provider:repository:item; relative values read as “left” or “ago.” Kept the existing complete five-column --full view and unchanged JSON payloads. Added POSIX/Windows resource summary coverage and updated list contract tests and documentation.

Validation: focused list formatter tests passed; the complete core suite (303 tests) and SDK suite (19 tests) passed; lint, format-check, typecheck, and staged pre-commit hooks passed. A direct `mise run cli -- list` against the machine’s current claims produced the three-column summary, while `--full` retained all five complete columns and `--json` retained the complete claim object.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Simplified default `worklease list` output to STATE, concise RESOURCE, and LEASE. Git-backed keys now read like `backlog-md:worklease:TASK-68`; lifecycle IDs remain available through `--full` and JSON. Verified against this machine’s claims, focused regressions, all 322 tests, lint, formatting, type checking, and pre-commit hooks.
<!-- SECTION:FINAL_SUMMARY:END -->
