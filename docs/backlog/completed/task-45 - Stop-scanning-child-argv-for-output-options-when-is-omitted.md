---
id: TASK-45
title: Stop scanning child argv for output options when -- is omitted
status: Done
assignee:
  - '@codex-task-45'
created_date: '2026-09-07 03:27'
updated_date: '2026-09-07 14:13'
labels:
  - cli
dependencies: []
references:
  - src/worklease/cli.py
priority: medium
type: bug
ordinal: 46000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`_visible_output_options` truncates argv only at a bare `--`, but `exec`/`exec-bundle` accept child argv without it. Any exact `--json`, `-j`, `--format`, `-f`, `--format=...`, or `-f...` token in the child command then either rejects the whole command as conflicting-output-format (`worklease --json exec ... git log --format=oneline` exits 64) or flips the error output mode. Either require `--` for exec commands with an actionable hint, or truncate the pre-parse scan at the first positional after the exec subcommand, and prefer deriving the json/format conflict from parsed args rather than argv scanning where possible.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 worklease --json exec <claim flags> git log --format=oneline runs the child and returns JSON
- [x] #2 A parser error for exec is rendered in the format chosen by worklease options, unaffected by tokens in child argv
- [x] #3 Behavior with and without -- is documented in exec help and README; tests cover both
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Bound raw output-option scanning at the first child argv token for exec, exec-bundle, and bundle-exec while preserving worklease options before that boundary.
2. Add singleton and bundle CLI regressions for child output-like flags with and without `--`, including parser-error format isolation.
3. Document optional `--` separation in command help and README, then run focused and repository quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented child-argv boundary detection for output-format scanning, documented optional `--` behavior, and added regressions for singleton/bundle aliases plus an executing child with `--format=oneline`. Repository gates pass: lint, format-check, test (232 tests), and typecheck.

Post-rebase validation on main base passed: lint, format-check, 233 tests (214 core + 19 SDK), and typecheck. Direct acceptance script ran `git log --format=oneline -1` through separator-free JSON exec and verified text parser-error isolation plus exec help. Adversarial review found the JSON test helper still used the old boundary rule; fixed it to reuse `_visible_output_options` and added no-separator coverage for exec and bundle aliases.

Post-delivery review (TASK-56) found and fixed defects in this work; see TASK-56 for the specific defect, the fix, and its regression test.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Exec output-format scanning now stops at the child executable even when `--` is omitted, so child `--json`/`--format` flags pass through unchanged and cannot alter Worklease errors. Documented both invocation forms and added singleton/bundle regressions. Verified with the full lint, format, test, and typecheck gates plus a direct JSON `git log --format=oneline` acceptance run.
<!-- SECTION:FINAL_SUMMARY:END -->
