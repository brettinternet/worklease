---
id: TASK-85.9
title: Implement operation inspection, reconciliation, events, and history
status: done
type: feature
priority: high
assignees: ['@pi-01a0959c']
labels: [go-rewrite]
milestone: m-0
parent: TASK-85
blocked_by: [TASK-85.8, TASK-85.10]
rank: ab658072f9b657fd2
refs: [docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md, src/worklease/reconciliation.py, src/worklease/operations.py, src/worklease/projections.py, tests/test_store.py, tests/test_history.py, TASK-69]
files: [CHANGELOG.md, internal/cli/commands.go, internal/cli/ledger_commands.go, internal/cli/ledger_commands_test.go, internal/handle/handle.go, internal/lease/reconciliation.go, internal/lease/reconciliation_test.go, internal/ledger/ledger.go, internal/ledger/ledger_test.go]
created: 2026-09-12T03:23:00Z
updated: 2026-09-12T12:58:00Z
---

Implement operation inspection/reconciliation, events and history from contract sections 6.3, 7.4/7.5 and 11. Own internal/ledger, op/events/history CLI and minimal shared lease/handle wiring. Depend on multi-resource claims and handles so the actual recovery path can be exercised end to end.

Public inspection exposes metadata only; private full inspection authenticates the target epoch. Reconciliation identifies its own request separately from the target claim/operation, supports current-owner recovery of ended predecessors covering the full original resource set, and requires caller evidence of outcome and executor cessation. It never adopts an expired owner. Use authority/feed/filter-bound cursors with honest retention gaps.

Evidence and patterns (the amended contract is normative): `src/worklease/reconciliation.py` (authorization, outcome vocabulary, idempotent replay, append-only), `operations.py` (inspect redaction), `projections.py` (history and events shapes to simplify, not copy). Tests in `tests/test_store.py`: test_inspect_operation_redacts_unknown_and_completed_receipts, test_reconcile_operation_is_authorized_idempotent_and_append_only, test_reconcile_rejects_fingerprint_and_malformed_evidence, test_reconcile_storage_failure_rolls_back_claim_and_audit_record, test_inspect_operation_rejects_reused_operation_id_as_ambiguous. Tests in `tests/test_history.py`: test_exact_singleton_and_bundle_member_histories, test_termination_reasons_and_stored_snapshots, test_projection_never_reads_secret_blob_columns_or_unrelated_rows, test_events_feed_is_redacted_paginated_and_has_timestamp_indexes, test_events_cursor_preserves_ties_and_concurrent_insert_semantics, test_events_rejects_invalid_cursor_without_opening_database.

## Acceptance criteria

- [x] Inspect tests cover public redaction, authenticated full receipt/evidence access, started-state reporting, historical credentials and ambiguous operation IDs resolved by explicit target claim ID.
- [x] Reconciliation tests cover expired predecessor recovery using a current covering claim, full-resource coverage checks, distinct target/resolver IDs, request-hash and evidence validation, idempotency/conflicts and transactional rollback.
- [x] Cursor tests cover bound authority/feed/resource identity, malformed input before storage open, ascending pages without skips under concurrent append, empty-feed watermark preservation and explicit GC gaps.
- [x] History correctly includes singleton and multi-resource epochs with public summaries, status and coverage; --full never exposes checkpoint/output/evidence without the separate authenticated inspection path.
- [x] CLI recovery from a pending handle is executable, including safe predecessor reconciliation without replaying external effects; text/JSON and mise run ci-go pass. Reconciliation from a pending exec retains both exact requests and survives pre-dispatch/post-commit crashes before atomically clearing the two slots. A definitive wrong-hash reconciliation clears only recoveryRequest so corrected evidence can be submitted while the original exec request remains; uncertain reconciliation retains both, confirmed resolution clears both.

## Definition of done

- [x] `mise run ci-go` passes on the final commit
- [x] Final summary names the Go test functions or commands that prove each acceptance criterion

## Plan

1. Add internal/ledger read models for redacted operation inspection, authenticated full inspection, authority/filter-bound cursors, events pages, and exact-resource epoch history.
2. Add transactional lease reconciliation for current or ended predecessor operations, validating credentials, full resource coverage, target request hash, bounded evidence, replay identity, and resolver revision/event updates.
3. Wire op inspect/reconcile, events, and history CLI actions, including pending-handle recoveryRequest persistence and exact crash-safe clearing semantics.
4. Add focused ledger, lease, handle, and CLI tests for every acceptance scenario; append the Go rewrite changelog entry and run mise run ci-go.

## Notes

Implemented the Go ledger read models, authenticated reconciliation, cursor-bound events/history commands, and pending-handle recovery semantics with focused tests. Local go test ./... and go vet ./... pass; running the full ci-go gate next.

Validation passed on the merged main branch: mise run ci-go (gofmt, go vet, staticcheck, go test, go test -race, govulncheck, build). Repository gates also passed: mise run lint, format-check, test, and typecheck after mise run sync.

## Summary

Implemented the Go operation ledger, authenticated current/predecessor reconciliation, redacted event/history projections, authority/feed/filter-bound cursors, and crash-safe pending-handle recovery.

Evidence:
- AC1: TestInspectPublicRedactionAuthenticatedFullStartedAndAmbiguousEpochs.
- AC2: TestReconcileExpiredPredecessorRequiresCoveringCurrentClaimAndIsIdempotent, TestReconcileRejectsPartialCoverageHashAndMalformedEvidence, and TestReconcileStorageFailureRollsBackTargetResolverAndAudit.
- AC3: TestEventsCursorBindingPaginationWatermarkAndGap, TestEmptyFeedsPreserveDurableWatermark, and TestEventsCLIRejectsMalformedCursorBeforeOpeningStorage.
- AC4: TestHistoryExactResourceCoverageAndNeverReturnsPrivatePayloads.
- AC5: TestLedgerCLIJSONAndPendingHandleReconciliationRecovery.
- Final gate: mise run ci-go passed on merged main, including race tests and vulnerability scan.
