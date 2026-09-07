---
id: TASK-52
title: Bound guarded exec duration
status: Done
assignee:
  - '@pi-codex'
created_date: '2026-09-07 03:28'
updated_date: '2026-09-07 12:54'
labels:
  - exec
dependencies: []
references:
  - src/worklease/execution.py
modified_files:
  - README.md
  - src/worklease/cli.py
  - src/worklease/cli_dispatch.py
  - src/worklease/execution.py
  - src/worklease/models.py
  - src/worklease/schemas/v1/commands.json
  - tests/test_cli.py
  - tests/test_execution.py
priority: medium
type: enhancement
ordinal: 53000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`exec` drains child output to EOF with no upper bound while renewing the lease indefinitely and holding the non-blocking resource flock, so every other command on that resource fails `resource-guarded` for as long as the child (or a grandchild holding the inherited pipes) runs. Add an explicit maximum duration (flag with a documented default or opt-in), terminate the process group on expiry, record the operation as failed or unknown-outcome as appropriate, and document the caveat about grandchildren inheriting pipes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 exec and exec-bundle accept a maximum duration; exceeding it terminates the child process group and returns a documented reason and exit code
- [x] #2 The operation ledger records the timed-out operation so inspect-operation shows its state
- [x] #3 README and help text document the bound and the grandchild pipe caveat; tests cover a child that outlives the bound
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a validated --max-duration option with a finite 3600-second default to exec and exec-bundle, and include the bound in each idempotent operation request.
2. Stop the child process group when the bound expires, persist a completed timeout receipt with reason child-process-timeout and conventional exit code 124, and preserve bounded captured output.
3. Add singleton and bundle coverage for timeout, operation inspection, validation, and help; document the default, exit behavior, and inherited-pipe/process-group caveat in README.
4. Run focused tests and all repository quality gates, review the diff, finalize TASK-52 with evidence, commit, merge to main, and remove the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented finite exec/exec-bundle duration enforcement, process-group termination, completed timeout receipts, CLI/schema/docs updates, and focused singleton/bundle/CLI coverage. Focused tests, lint, formatting, and typecheck pass; full tests exposed two manually seeded operation requests that needed the new maxDuration fingerprint, now corrected.

Adversarial review found deadline races and uncancellable pipe-reader leaks. Replaced threaded pipe draining with selector-based nonblocking reads, added an independent watchdog for blocked heartbeats, and added regression coverage for escaped grandchildren, pre-timeout output capture, blocked renewal, and partial EOF. Full lint, format, test (229 core + 19 SDK), and typecheck gates pass after the refactor.

Final verification: mise run lint, mise run format-check, mise run test (239 core and 19 SDK tests), and mise run typecheck all pass on current main integration. Reviewer findings were fixed and the final pass identified only an oversized-integer validation edge, which is now covered and resolved.

Delivered in implementation commit b3b8445 after merge with current main; final adversarial review findings were all resolved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added a finite 3600-second default --max-duration to exec and exec-bundle. Deadline-aware nonblocking pipe draining and an independent watchdog terminate process groups on expiry, preserve available bounded output, return child-process-timeout/124, and complete operation-ledger receipts for inspection. README, command help, JSON schema, CLI dispatch, and tests cover singleton/bundle timeouts, escaped grandchildren, continuous inherited-pipe writers, blocked heartbeats, partial EOF, idempotency, and invalid bounds. Verified with all repository quality gates and adversarial review.
<!-- SECTION:FINAL_SUMMARY:END -->
