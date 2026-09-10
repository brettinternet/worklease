---
id: TASK-2
title: Add Ruff and Lefthook checks
status: Done
assignee:
  - '@brett'
created_date: '2026-07-13 21:15'
updated_date: '2026-07-16 15:30'
labels:
  - tooling
  - quality
dependencies: []
priority: medium
type: chore
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add project-wide Python linting and formatting with Ruff, and staged-file pre-commit checks with Lefthook managed through mise. Update agent guidance so contributors fix checks and tests before handoff.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Ruff configuration and project dependency are committed and run through mise
- [x] #2 Lefthook configuration checks staged Python files without bypassing test failures
- [x] #3 AGENTS.md tells agents to fix lint, format, hook, and test failures
- [x] #4 Targeted verification passes and the changes are committed
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
### Implementation tasks
- [x] T1 — Add Ruff/Lefthook configuration, mise quality tasks, AGENTS guidance, and deterministic validation (AC1-AC4).
- [x] T2 — Gate the Lefthook test command on staged Python files (AC2).

### Implementation approach
1. Add the staged-Python glob to the Lefthook test command, including nested paths.
2. Verify staged non-Python files skip tests and staged Python files run the Python suite.
3. Run required quality checks and finalize the task with objective evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented Ruff 0.15.21 as a uv dev dependency, pyproject lint/format policy, mise lint/format tasks, and Lefthook 2.1.10 managed by mise. Lefthook validation passed; mise run hooks passed staged Ruff check, staged format check, and all 13 unit tests. mise run typecheck, mise run build, mise run sync, mise run lint, and mise run format-check all passed. TASK-1 changes remain intentionally unstaged.

Implementation checkpoint T1: existing implementation commit 925ca18 verified in this pass with mise run lint, mise run format-check, mise run test, mise run typecheck, and mise run hooks. Canonical checklist normalized after verification.

T1 complete. Implementation commit: 925ca18; canonical implementation-checkpoint commit: f8bd69f. Remaining: one REVIEW pass for the accumulated implementation and durable reviewed marker. Next task: REVIEW TASK-2.

Reopened to correct the pre-commit test trigger: tests currently run for every staged file instead of only staged Python changes.

Validation T2: `mise run hooks` with no staged files skipped tests; a staged non-Python fixture skipped tests; a staged nested Python fixture at `packages/worklease-source-sdk/tests/.staged-hook-check.py` ran Ruff and both test suites successfully. Explicit `mise run lint`, `mise run format-check`, `mise run test`, and `mise run typecheck` all passed. Temporary fixtures were removed and only `lefthook.yml` plus this task record remain modified.

T2 implementation committed as e830c677e8b1cf4ef807028998641bc4921063e0.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added `glob: ["*.py", "**/*.py"]` to the Lefthook test command so staged Python changes, including nested package files, run `mise run test`; staged non-Python changes skip it. Verified no-staged and staged non-Python skips, staged nested Python execution, `mise run lint`, `mise run format-check`, `mise run test`, and `mise run typecheck`. Implementation commit: e830c677e8b1cf4ef807028998641bc4921063e0.
<!-- SECTION:FINAL_SUMMARY:END -->
