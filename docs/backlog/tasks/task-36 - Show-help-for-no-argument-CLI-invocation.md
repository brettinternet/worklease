---
id: TASK-36
title: Show help for no-argument CLI invocation
status: Done
assignee:
  - '@codex-loop-fresh-20260716-worklease-pass'
created_date: '2026-07-16 14:25'
updated_date: '2026-07-16 14:33'
labels: []
dependencies: []
ordinal: 37000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the worklease CLI more approachable when invoked without a command, while preserving actionable failures for genuinely invalid or incomplete commands. Survey existing parser entry points and apply only adjacent ergonomics supported by current behavior.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Running worklease with no arguments prints the full top-level help menu to stdout and exits successfully.
- [x] #2 Unknown commands and incomplete nested commands remain parse errors with their existing nonzero exit behavior.
- [x] #3 Regression coverage distinguishes the no-argument help path from invalid-command parser failures.
- [x] #4 The focused CLI behavior and repository quality gates pass before commit.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add an exact no-argument branch that prints the existing full top-level help and exits 0.
2. Preserve parser failures for unknown commands and incomplete nested commands; verify adjacent entry points do not warrant broader behavior changes.
3. Add subprocess regression coverage and run focused CLI smoke checks plus all project quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the exact no-argument branch in src/worklease/cli.py to print parser.format_help() and return 0. Added subprocess coverage for no-argument help equality with --help, unknown command and incomplete policy failures, plus explicit JSON missing-command behavior. Smoke-tested with mise run cli and the focused regression test in the isolated worktree.

Formatted tests; mise run lint, mise run format-check, mise run test, mise run typecheck, and staged mise run hooks all pass in .worktrees/task-36-no-args.

Independent verifier PASS on commit 9a02f0aff6f558733661a80496b0cbdbb4687667: exact no-args help matches --help and exits 0; unknown, nested policy, and explicit JSON failures retain nonzero parse behavior; focused regression and manual checks pass.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added an exact no-argument CLI path that prints the existing full top-level help to stdout and exits 0. Preserved invalid-command, incomplete policy, and explicit JSON missing-command failures. Added subprocess coverage distinguishing all paths. Verified with focused tests, direct smoke checks, independent verifier PASS, mise run lint, format-check, test, typecheck, and staged hooks. Committed as 9a02f0aff6f558733661a80496b0cbdbb4687667 and fast-forward integrated into main.
<!-- SECTION:FINAL_SUMMARY:END -->
