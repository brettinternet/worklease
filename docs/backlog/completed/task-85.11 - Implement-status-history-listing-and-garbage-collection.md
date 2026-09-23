---
id: TASK-85.11
title: Implement read views and garbage collection
status: Done
assignee:
  - '@pi-01a095b3'
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 13:42'
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
modified_files:
  - CHANGELOG.md
  - internal/gc/gc.go
  - internal/gc/gc_test.go
  - internal/cli/commands.go
  - internal/cli/lease_commands.go
  - internal/cli/ledger_commands.go
  - internal/cli/gc_commands.go
  - internal/cli/gc_commands_test.go
  - internal/cli/text.go
  - internal/cli/text_test.go
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 103000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement read views and retention from contract section 11. Own internal/gc and gc/status/list/history/events text rendering. Preserve existing transactional semantics, private payload boundaries and authority-bound cursor encoding.

Retain from recorded termination/reconciliation time and request replay deadlines, protect active/unresolved work, and prune event history only as a contiguous prefix. A newly retired old claim keeps fresh recovery evidence for the retention window. Recompute apply eligibility within its transaction. Prefer simple deterministic summaries and full identifiers over a new Unicode layout dependency.

Evidence and patterns (the amended contract is normative): `src/worklease/garbage_collection.py` (cutoff rules, protection predicates, atomic apply, category summaries) and the `projections.py` text renderers (what to avoid). Tests: all 29 in `tests/test_gc.py`, especially test_apply_is_atomic_and_preserves_resource_revision, test_cutoff_is_strict_and_excludes_boundary_records, test_expired_claim_is_retired_only_after_strict_cutoff, test_old_expired_claim_with_unresolved_operation_is_explained, test_active_claim_protects_old_records, test_gc_serializes_expired_retirement_acquire_and_heartbeat, test_dry_run_does_not_mutate_records_or_revisions, test_history_coverage_tracks_partial_and_complete_epoch_gc; in `tests/test_cli.py`: test_text_status_uses_compact_operational_summary, test_text_list_aligns_columns_across_multiple_rows, test_display_width_counts_terminal_columns, test_resource_shortening_bounds_opaque_values, test_gc_text_dry_run_is_compact_and_actionable, test_gc_cli_fixed_clock_covers_dry_run_apply_noop_and_json.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Dry-run tests prove strict cutoff behavior, accurate eligible/protected counts and timestamps, and no writes to rows/revisions/watermarks or filesystem.
- [x] #2 Apply tests prove atomic rollback, multi-resource retirement, ended_recorded_at-based retention and that newly retired epochs survive the same run.
- [x] #3 Interleaved old/current/new epochs prove no middle event rows are pruned, replay/authentication is retained through requestNotAfter, and expired retries then fail replay-expired without redispatch, unresolved predecessors remain visible, and prunedThrough reflects only a real contiguous prefix. An old protected epoch pins newer epoch/history/receipt deletion too, so history cannot lose a row without a prefix gap.
- [x] #4 Concurrent acquire/heartbeat/reconcile during GC observes a complete valid state; last_event_seq survives full eligible pruning and continuations report gaps instead of silently skipping.
- [x] #5 Empty/populated default/full text is deterministic and readable for long/Unicode resources; all public JSON/full views and GC redact private payloads; mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci-go` passes on the final commit
- [x] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add an internal/gc service that previews strict-cutoff retention and atomically applies expired retirement plus contiguous epoch/event prefix pruning inside one immediate transaction.
2. Wire gc into the CLI and add deterministic redacted text rendering for gc, status, list, history, and events.
3. Add focused Go tests for cutoff/protection/replay retention, atomicity/concurrency, cursor gaps/watermarks, redaction, and long/Unicode text.
4. Run focused tests and mise run ci-go, review the diff, fix findings, then record objective acceptance evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented transactional Go GC and deterministic read-view text. Added focused tests for strict cutoff/dry-run immutability, multi-resource retirement/newly-retired retention, rollback, replay deadlines, unresolved predecessors, contiguous prefix/watermark gaps, concurrent lifecycle operations, active protection, Unicode width, and public/full redaction. Validation: mise run ci-go passed in the task worktree.

Delivery: implementation commit 5201e50; merged to main as a9b73b2. Independent reviewer found no source defects; independent verifier passed all five acceptance criteria after evidence gaps were corrected. Post-merge mise run ci-go passed. No remaining blocker; TASK-85.14 is now dependency-ready.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented strict-cutoff transactional Go garbage collection plus deterministic redacted status/list/history/events/GC text. AC1: TestCollectStrictCutoffAndDryRunDoesNotMutate proves cutoff and full state/filesystem immutability. AC2: TestCollectRetiresExpiredClaimAndKeepsItUntilNextRun and TestApplyRollbackLeavesRetirementAndWatermarksUntouched prove multi-resource retirement, recorded-end retention, fresh recovery retention, and rollback. AC3: TestInterleavedEpochsPruneOnlyEventPrefixAndProtectUnresolved, TestUnresolvedPredecessorIsProtected, and TestReplayReceiptAndAuthenticationRetainedThroughDeadlineThenPruned prove contiguous pruning, explicit gaps, unresolved protection, bounded replay, and replay-expired after collection. AC4: TestConcurrentGCAcquireHeartbeatAndReconcileSerialize proves serialized valid state with integrity_check=ok; interleaved coverage proves watermarks and cursor gaps. AC5: TestPublicFullHistoryAndEventsRedactCheckpointAndCredentials, TestGCEmptyPreviewJSONAndText, TestOpaqueTextWidthAndShortening, and TestListTextAlignsByTerminalCells prove redaction and deterministic Unicode-safe text. Verified with mise run ci-go, mise run lint, mise run format-check, mise run test, and mise run typecheck.
<!-- SECTION:FINAL_SUMMARY:END -->
