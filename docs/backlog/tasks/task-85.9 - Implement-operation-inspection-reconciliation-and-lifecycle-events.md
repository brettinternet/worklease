---
id: TASK-85.9
title: 'Implement operation inspection, reconciliation, events, and history'
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.8
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/reconciliation.py
  - src/worklease/operations.py
  - src/worklease/projections.py
  - tests/test_store.py
  - tests/test_history.py
  - TASK-69
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 101000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Guarded work can fail after intent is durable but before its result is known, and operators need to see what happened without reading the database. The store already appends events (D9) and the lease service records operations (TASK-85.7). This task adds the read models and the single reconciliation path over them: `op inspect`, `op reconcile`, `events` with seq cursors and honest gaps, and `history` per resource, as specified in contract sections 7.4, 7.5, and 11.

Read first: contract sections 4 (op, history, events rows), 6.3, 7.4, 7.5, 7.12, 8, 11, 18. Python evidence: `src/worklease/reconciliation.py` (authorization, outcome vocabulary, idempotent replay, append-only), `operations.py` (inspect redaction), `projections.py` (history and events shape to simplify, not copy); `tests/test_store.py`: test_inspect_operation_redacts_unknown_and_completed_receipts, test_reconcile_operation_is_authorized_idempotent_and_append_only, test_reconcile_rejects_fingerprint_and_malformed_evidence, test_reconcile_storage_failure_rolls_back_claim_and_audit_record, test_inspect_operation_rejects_reused_operation_id_as_ambiguous; `tests/test_history.py`: test_exact_singleton_and_bundle_member_histories, test_termination_reasons_and_stored_snapshots, test_projection_never_reads_secret_blob_columns_or_unrelated_rows, test_events_feed_is_redacted_paginated_and_has_timestamp_indexes, test_events_cursor_preserves_ties_and_concurrent_insert_semantics, test_events_rejects_invalid_cursor_without_opening_database.

Deliver in `internal/ledger`: `Inspect` (by claim id or by resources) returning kind, state, expectedRevision, timestamps, and a non-secret receipt summary (exit status, byte counts, hashes), adding the receipt and evidence only with `--full`; ambiguity (`-r` matching several epochs with the same operation id) fails `operation-ambiguous` listing the claim ids; `Reconcile` under current credentials with request-hash verification against `--expected-request-sha256`, outcome `observed-success` or `observed-failure`, JSON evidence up to 8 KiB, state started to reconciled, revision plus one, one reconciliations row, a `reconciled` event, idempotent replay, and `reconciliation-conflict` on differing input; `Events(cursor, limit)` and `History(resource, cursor, limit)` per contract 11 including gap results, coverage, and epoch status derived from the injected clock. CLI: `op inspect`, `op reconcile`, `events`, `history` in text and JSON with cursors printed copyably.

Owned paths: `internal/ledger`, `internal/cli/op.go`, `events.go`, `history.go` and tests. Out of scope: garbage collection (TASK-85.11), watch (TASK-85.13), text polish beyond deterministic output.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Inspect tests prove a completed exec operation shows state, exit status, and byte counts but no stdout or stderr without --full, a started operation reports unknown-outcome with its start time, an operation id present on two epochs of the same resource fails operation-ambiguous listing both claim ids, and inspecting by an explicit claim id resolves it.
- [ ] #2 Reconcile tests prove success requires current credentials (stale-claim, invalid-token, and stale-revision paths), a wrong --expected-request-sha256 fails operation-request-mismatch, a non-started operation fails reconciliation-conflict, and a valid reconciliation writes one reconciliations row, sets state reconciled, increments the revision, appends a reconciled event, replays idempotently, and rolls back entirely on an injected storage failure.
- [ ] #3 Events tests prove ascending pagination with nextCursor across three pages without duplicates or skips while a subprocess appends events concurrently, the no-cursor default returns the most recent limit rows, a cursor below pruned_through_seq (set directly in meta for the test) returns the gap result, and a malformed cursor fails cursor-invalid before the store is opened.
- [ ] #4 History tests prove per-resource epochs for a resource held alone and as a member of a three-resource claim, statuses open, expired-open, and complete under the injected clock, operations and reconciliation summaries attached to the right epoch, coverage fields, and that neither default output nor JSON contains tokens, token hashes, checkpoints, evidence, or child output (asserted with testkit.AssertNoSecret).
- [ ] #5 CLI text and JSON for all four commands are deterministic under a fixed clock and golden-tested, and `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
