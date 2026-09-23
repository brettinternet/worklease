---
id: TASK-128.6
title: Add `worklease queue query --json`
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 19:09'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-128.2
  - TASK-128.3
  - TASK-128.7
references:
  - internal/output/output.go
  - internal/cli/root.go
  - internal/cli/zero_flag_audit_test.go
  - internal/cli/man_test.go
  - docs/cli-reference.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-128
priority: high
type: feature
ordinal: 16000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Agents need the queue's information without screen scraping, and no workflow may be safer in the TUI than in automation (plan section 13). Each item must carry its exact claim resources and key inputs, so a CLI caller acquires the same bytes the queue would (plan section 6).

The command builds sources from queue.yaml through the TASK-128.3 adapter registry, so it works with whichever built-in adapters are registered; tests use the fake adapter. `--max-age` belongs to TASK-129.1 because it needs the index, and `queue next` belongs to TASK-130.4. This task owns the command, its JSON schema v1, and its documentation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `worklease queue query --view NAME --json` emits one versioned envelope. It contains the view; the authority (profile, authorityId, and local or remote scope); per-source coverage, freshness, and diagnostics; and items. Each item has ref, display ID, title, raw and normalized state, readiness with reasons, provider-reported readiness, assignment, claim observation, native claim state, `resources`, `keyInputs`, and per-action availability with reasons
- [x] #2 `resources` and `keyInputs` are byte-identical to `worklease key` output for the same inputs, as tested against the TASK-126.4 vectors
- [x] #3 `--limit` and an opaque `--cursor` give bounded pagination. The cursor is bound to view, query, sort, sources, principal, and generation, and any mismatch (changed filter, changed credential scope, stale generation) returns a structured cursor error
- [x] #4 `--require-complete` exits non-zero with a structured `incomplete` result when source coverage or dependency edges are incomplete
- [x] #5 Without `--json`, the command prints a compact human-readable table consistent with existing list output
- [x] #6 Output passes through the existing internal/output normalization and redaction
- [x] #7 The command is covered by the help, man-page, and zero-flag audit tests like other commands, and docs/queue.md and docs/cli-reference.md document it and its JSON schema
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add queue query command using configured sources, registry, snapshot loader and authority overlay. 2. Define versioned normalized output, bounded cursor validation, completeness enforcement and text table. 3. Cover schema, cursor, key vectors, help/man/audit and document command. 4. Run focused and repository gates, review, integrate and finalize.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed for implementation in isolated worktree.

Implemented on task-128-6-queue-query, integrated to main as 2a007a6 and edfc385. End-to-end queue tests exercise schema, pagination, credential/principal/config fingerprint, filtered completeness, mixed failed/healthy sources, key vector equality, redaction and text output; existing CLI help/man/zero-flag tests pass. Reviewer found six concrete issues; corrected all, preserving concurrent queue TUI. mise run lint, format-check, test, typecheck, hooks passed after corrections. No D1-D27 or plan contradiction introduced; fresh snapshot query explicitly defers index/max-age to S3.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added read-only queue query JSON v1 and text output with source/claim evidence, bounded generation-bound pagination, structured incomplete/cursor errors, exact key inputs, redaction, tests and docs; integrated to main, all gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
