---
id: TASK-128.6
title: Add `worklease queue query --json`
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
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
- [ ] #1 `worklease queue query --view NAME --json` emits one versioned envelope. It contains the view; the authority (profile, authorityId, and local or remote scope); per-source coverage, freshness, and diagnostics; and items. Each item has ref, display ID, title, raw and normalized state, readiness with reasons, provider-reported readiness, assignment, claim observation, native claim state, `resources`, `keyInputs`, and per-action availability with reasons
- [ ] #2 `resources` and `keyInputs` are byte-identical to `worklease key` output for the same inputs, as tested against the TASK-126.4 vectors
- [ ] #3 `--limit` and an opaque `--cursor` give bounded pagination. The cursor is bound to view, query, sort, sources, principal, and generation, and any mismatch (changed filter, changed credential scope, stale generation) returns a structured cursor error
- [ ] #4 `--require-complete` exits non-zero with a structured `incomplete` result when source coverage or dependency edges are incomplete
- [ ] #5 Without `--json`, the command prints a compact human-readable table consistent with existing list output
- [ ] #6 Output passes through the existing internal/output normalization and redaction
- [ ] #7 The command is covered by the help, man-page, and zero-flag audit tests like other commands, and docs/queue.md and docs/cli-reference.md document it and its JSON schema
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
