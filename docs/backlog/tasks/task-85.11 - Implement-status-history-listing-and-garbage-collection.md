---
id: TASK-85.11
title: Implement read views and garbage collection
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 05:55'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.9
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/garbage_collection.py
  - src/worklease/projections.py
  - tests/test_gc.py
  - tests/test_cli.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 103000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement read views and retention from contract section 11. Own internal/gc and gc/status/list/history/events text rendering. Preserve existing transactional semantics, private payload boundaries and authority-bound cursor encoding.

Retain from recorded termination/reconciliation time and request replay deadlines, protect active/unresolved work, and prune event history only as a contiguous prefix. A newly retired old claim keeps fresh recovery evidence for the retention window. Recompute apply eligibility within its transaction. Prefer simple deterministic summaries and full identifiers over a new Unicode layout dependency.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Dry-run tests prove strict cutoff behavior, accurate eligible/protected counts and timestamps, and no writes to rows/revisions/watermarks or filesystem.
- [ ] #2 Apply tests prove atomic rollback, multi-resource retirement, ended_recorded_at-based retention and that newly retired epochs survive the same run.
- [ ] #3 Interleaved old/current/new epochs prove no middle event rows are pruned, replay/authentication is retained through requestNotAfter, and expired retries then fail replay-expired without redispatch, unresolved predecessors remain visible, and prunedThrough reflects only a real contiguous prefix. An old protected epoch pins newer epoch/history/receipt deletion too, so history cannot lose a row without a prefix gap.
- [ ] #4 Concurrent acquire/heartbeat/reconcile during GC observes a complete valid state; last_event_seq survives full eligible pruning and continuations report gaps instead of silently skipping.
- [ ] #5 Empty/populated default/full text is deterministic and readable for long/Unicode resources; all public JSON/full views and GC redact private payloads; mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
