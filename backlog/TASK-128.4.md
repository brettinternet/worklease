---
id: TASK-128.4
title: Add the read-only Backlog.md source adapter
status: done
type: feature
priority: high
assignees: ['@brett']
labels: [work-queue, backlog-md, reviewed]
milestone: m-1
parent: TASK-128
blocked_by: [TASK-128.2, TASK-128.3]
rank: a19269fe3419269e8
refs: [skills/worklease-workflow/references/source-providers/backlog-md.md, backlog.config.yml, docs/work-queue-tui-proposal.md]
created: 2026-09-23T04:29:00Z
updated: 2026-09-23T21:58:00Z
---

D13 and plan section 3 fix how the queue reads Backlog.md. It uses only versioned `backlog ... --json` commands, run against one explicitly configured checkout. The Backlog.md MCP server returns formatted text, and parsing task Markdown files directly is forbidden. Summaries come from one `task list --json` call. Dependencies and details come from one `task view --json` call per item, about 0.5 s each, so in this slice hydrate only the requested items; the background edge cache is TASK-129.4.

The adapter also reports source freshness (branch, HEAD, dirty state), duplicate task IDs, and Git side effects from the project configuration (plan section 5), using the TASK-126.6 findings. It stays read-only here; writes arrive in TASK-132.2. Register it in the TASK-128.3 adapter registry under `backlog-md`.

## Acceptance criteria

- [x] The adapter resolves the configured checkout and fails with structured diagnostics when the directory is not a Backlog.md project, the `backlog` CLI is missing, or its version is outside the recorded supported range (tested against 1.52.0)
- [x] Summaries come from a single `task list --json` call, and an unexpected `kind` or `schemaVersion` is rejected with a diagnostic. The adapter never parses plain-text output, MCP responses, or task Markdown files
- [x] Details and dependencies come from `task view --json` only for requested items, with at most 4 concurrent processes, a per-call timeout, cancellation, and a bounded output size. A dependency closure counts as complete only when the provider reports no missing dependencies
- [x] Capabilities match the Backlog.md column of the plan section 7 table. Effects are read with `backlog config get`: remote_operations and check_active_branches mean network effects, and auto_commit means commit effects
- [x] When remote_operations or check_active_branches is enabled and the source lacks `allowGitNetwork: true`, the adapter performs no reads and reports why
- [x] Backlog.md `dependencies` map to hard prerequisites with the caller-declared terminal condition, `parentTaskId` maps to hierarchy only, and `isReady` is exposed only as provider-reported readiness
- [x] Source diagnostics include branch, HEAD, dirty state, and any task ID that appears more than once in list output
- [x] Every subprocess runs with an explicit cwd and argv and no shell, and provider stderr is sanitized before display
- [x] Unit tests use golden JSON fixtures. An integration test creates a scratch project in a temporary directory with the real CLI, or skips with a clear message when the CLI is absent

## Definition of done

- [x] `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit

## Plan

1. Add a read-only Backlog.md adapter with bounded CLI execution, configuration/effect gates, structured diagnostics, and source freshness.
2. Map versioned list/view JSON into queue summaries and requested dependency details; register the adapter.
3. Add golden-fixture and scratch-CLI tests; run focused and repository gates, review, commit and integrate.

## Notes

Implemented read-only adapter with versioned JSON list/view, bounded subprocesses, project effects consent, duplicate detection, dependency mapping and scratch CLI tests. Focused tests pass; running full checks.

Verified: golden JSON unit tests cover schema, effect consent, mapping, duplicate IDs, timeout/cancellation/output cap, sanitized stderr and Git freshness; scratch real-CLI init/list/view integration passes. mise run lint, format-check, test, typecheck, hooks pass. One item-scoped review found and fixed on-demand hydration and hook Git environment leakage. No D1-D27 or plan contradiction. Commits 9259fbc and 744e295 fast-forward merged to main. Recovery: an initial scratch Git test inherited hook Git variables and briefly altered the owned worktree branch; restored its original base and main Git configuration, then reran all checks with sanitized environment.

Review: process-group cancellation, fsmonitor-disabled git status, no stale fallback detail reuse, 4-worker ReadItems pool, absent missingDependencies => partial, real stderr sanitization test; GIT_*-sanitized scratch git in tests (b33180f, merged 7268f9a).

## Summary

Added read-only Backlog.md queue adapter with bounded JSON reads, effect consent and dependency diagnostics; verified by golden and scratch CLI tests plus all repository gates. Merged 9259fbc and 744e295 to main.
