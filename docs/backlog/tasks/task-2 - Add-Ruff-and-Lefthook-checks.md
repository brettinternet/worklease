---
id: TASK-2
title: Add Ruff and Lefthook checks
status: In Progress
assignee:
  - '@codex-loop-fresh-20260714-worklease-pass'
created_date: '2026-07-13 21:15'
updated_date: '2026-07-15 05:44'
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

### Implementation approach
1. Inspect repository Python, mise, and hook conventions plus installed tool versions.
2. Add Ruff config and mise tasks for check/format, add Lefthook config and mise bootstrap task for staged Python checks.
3. Update AGENTS.md with mandatory fix-and-verify guidance.
4. Run targeted Ruff, Lefthook, and mise checks, finalize task evidence, and commit all changes.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented Ruff 0.15.21 as a uv dev dependency, pyproject lint/format policy, mise lint/format tasks, and Lefthook 2.1.10 managed by mise. Lefthook validation passed; mise run hooks passed staged Ruff check, staged format check, and all 13 unit tests. mise run typecheck, mise run build, mise run sync, mise run lint, and mise run format-check all passed. TASK-1 changes remain intentionally unstaged.

Implementation checkpoint T1: existing implementation commit 925ca18 verified in this pass with mise run lint, mise run format-check, mise run test, mise run typecheck, and mise run hooks. Canonical checklist normalized after verification.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added Ruff 0.15.21 configuration and mise lint/format tasks, Lefthook 2.1.10 staged-file pre-commit checks with tests, and AGENTS.md quality-gate guidance. Verified with mise run sync, lint, format-check, typecheck, build, and hooks; Lefthook validation passed and all 13 unit tests passed. Changes are committed.
<!-- SECTION:FINAL_SUMMARY:END -->
