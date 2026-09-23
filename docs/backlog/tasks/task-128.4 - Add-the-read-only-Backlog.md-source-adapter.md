---
id: TASK-128.4
title: Add the read-only Backlog.md source adapter
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 21:58'
labels:
  - work-queue
  - backlog-md
  - reviewed
milestone: m-1
dependencies:
  - TASK-128.2
  - TASK-128.3
references:
  - skills/worklease-workflow/references/source-providers/backlog-md.md
  - backlog.config.yml
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-128
priority: high
type: feature
ordinal: 14000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
D13 and plan section 3 fix how the queue reads Backlog.md. It uses only versioned `backlog ... --json` commands, run against one explicitly configured checkout. The Backlog.md MCP server returns formatted text, and parsing task Markdown files directly is forbidden. Summaries come from one `task list --json` call. Dependencies and details come from one `task view --json` call per item, about 0.5 s each, so in this slice hydrate only the requested items; the background edge cache is TASK-129.4.

The adapter also reports source freshness (branch, HEAD, dirty state), duplicate task IDs, and Git side effects from the project configuration (plan section 5), using the TASK-126.6 findings. It stays read-only here; writes arrive in TASK-132.2. Register it in the TASK-128.3 adapter registry under `backlog-md`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The adapter resolves the configured checkout and fails with structured diagnostics when the directory is not a Backlog.md project, the `backlog` CLI is missing, or its version is outside the recorded supported range (tested against 1.52.0)
- [x] #2 Summaries come from a single `task list --json` call, and an unexpected `kind` or `schemaVersion` is rejected with a diagnostic. The adapter never parses plain-text output, MCP responses, or task Markdown files
- [x] #3 Details and dependencies come from `task view --json` only for requested items, with at most 4 concurrent processes, a per-call timeout, cancellation, and a bounded output size. A dependency closure counts as complete only when the provider reports no missing dependencies
- [x] #4 Capabilities match the Backlog.md column of the plan section 7 table. Effects are read with `backlog config get`: remote_operations and check_active_branches mean network effects, and auto_commit means commit effects
- [x] #5 When remote_operations or check_active_branches is enabled and the source lacks `allowGitNetwork: true`, the adapter performs no reads and reports why
- [x] #6 Backlog.md `dependencies` map to hard prerequisites with the caller-declared terminal condition, `parentTaskId` maps to hierarchy only, and `isReady` is exposed only as provider-reported readiness
- [x] #7 Source diagnostics include branch, HEAD, dirty state, and any task ID that appears more than once in list output
- [x] #8 Every subprocess runs with an explicit cwd and argv and no shell, and provider stderr is sanitized before display
- [x] #9 Unit tests use golden JSON fixtures. An integration test creates a scratch project in a temporary directory with the real CLI, or skips with a clear message when the CLI is absent
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a read-only Backlog.md adapter with bounded CLI execution, configuration/effect gates, structured diagnostics, and source freshness.
2. Map versioned list/view JSON into queue summaries and requested dependency details; register the adapter.
3. Add golden-fixture and scratch-CLI tests; run focused and repository gates, review, commit and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented read-only adapter with versioned JSON list/view, bounded subprocesses, project effects consent, duplicate detection, dependency mapping and scratch CLI tests. Focused tests pass; running full checks.

Verified: golden JSON unit tests cover schema, effect consent, mapping, duplicate IDs, timeout/cancellation/output cap, sanitized stderr and Git freshness; scratch real-CLI init/list/view integration passes. mise run lint, format-check, test, typecheck, hooks pass. One item-scoped review found and fixed on-demand hydration and hook Git environment leakage. No D1-D27 or plan contradiction. Commits 9259fbc and 744e295 fast-forward merged to main. Recovery: an initial scratch Git test inherited hook Git variables and briefly altered the owned worktree branch; restored its original base and main Git configuration, then reran all checks with sanitized environment.

Review: process-group cancellation, fsmonitor-disabled git status, no stale fallback detail reuse, 4-worker ReadItems pool, absent missingDependencies => partial, real stderr sanitization test; GIT_*-sanitized scratch git in tests (b33180f, merged 7268f9a).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added read-only Backlog.md queue adapter with bounded JSON reads, effect consent and dependency diagnostics; verified by golden and scratch CLI tests plus all repository gates. Merged 9259fbc and 744e295 to main.
<!-- SECTION:FINAL_SUMMARY:END -->
