---
id: TASK-4.1
title: Transfer active lease ownership atomically
status: Done
assignee:
  - '@codex-loop-pass-20260714-worklease-verifier'
created_date: '2026-07-14 02:34'
updated_date: '2026-07-14 19:02'
labels:
  - coordination
  - lease
  - handoff
dependencies:
  - TASK-4
references:
  - src/worklease/store.py
  - src/worklease/models.py
modified_files:
  - tests/test_store.py
parent_task_id: TASK-4
priority: high
type: feature
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Extend TASK-4 with a true active-owner transfer that does not release the resource between owners. This is distinct from checkpointed release, expiry, and later acquisition: the current owner authorizes one atomic transition to a fresh successor ownership epoch.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The current active owner can atomically transfer one exact resource to supplied successor agent, session, owner, claim, and work identities using the current claim ID, token, and revision.
- [x] #2 A successful transfer creates a fresh random bearer token and higher revision, invalidates the prior token immediately, preserves the latest TASK-4 bounded checkpoint, and never exposes a free or dual-owner interval.
- [x] #3 Transfer requests and receipts are idempotent; replay returns the same successor result, while changed successor data, stale revisions, expired claims, wrong tokens, and reused claim IDs are rejected without changing ownership.
- [x] #4 Read-only status and diagnostics show non-secret handoff metadata without exposing either credential, and only the authorized transfer response returns the successor token.
- [x] #5 Automated concurrency and crash tests prove no contender can acquire during transfer, no stale owner can heartbeat/execute/release afterward, and interruption leaves exactly one valid ownership epoch.
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation pass claimed acceptance-1: add the atomic active-owner transfer API and persistence path with focused lifecycle tests. Remaining acceptance criteria: #1-#5.

Implementation checkpoint (acceptance-1): commit b099d27 integrates the atomic TransferRequest/LeaseStore.transfer API, CLI transfer command, public export, version-1 schema entries, and focused lifecycle/CLI tests. The mutation updates one claim row inside one SQLite transaction, creates the successor epoch, preserves the checkpoint, advances revision, and returns the successor token only in the authorized response. Verification: mise run test (106 tests), mise run lint, mise run format-check, mise run typecheck, mise run hooks all passed. Next task: acceptance-2 successor token/revision/checkpoint guarantees and lifecycle/concurrency coverage. Remaining acceptance criteria: #2-#5.

Implementation pass acceptance-2 complete: added deterministic coverage for successor token/revision/checkpoint preservation, stale replay/wrong-token/stale-revision/expiry/reused-claim rejection, stale-owner heartbeat/checkpoint/exec/release invalidation, contender serialization under the held resource lock, and SQLite interruption rollback. Commit e7419799c8387785fa6c6c3e96424fdedefbe4e3. Verification: mise run test -- -k transfer (6 tests) passed; mise run test (full suite) passed; mise run lint passed; mise run format-check passed; mise run typecheck passed; mise run hooks passed. All implementation tasks and acceptance criteria are now complete; next pass is REVIEW of the accumulated implementation at commit e7419799c8387785fa6c6c3e96424fdedefbe4e3.

Review marker: reviewed: implementation commits b099d27 and e741979; review-fix commit c6c4a6b. Full implementation-review applied for concurrency/transactions, public API/schema compatibility, idempotency, security/redaction, rollback, and failure paths. Finding fixed: transfer replay lookup previously filtered to kind=transfer, allowing the same operation ID to coexist with another operation kind and making inspect-operation ambiguous; replay now rejects cross-kind reuse, with regression coverage in tests/test_store.py. Verification: mise run test -- -k transfer (7 tests) passed; mise run test (121 tests) passed; mise run lint passed; mise run format-check passed; mise run typecheck passed; mise run hooks passed. Integration: c6c4a6b committed on canonical main via guarded control-exec.

Review follow-up: independent verification found two AC3 replay/migration gaps. Review-fix commit a78c72e adds state-aware transfer replay (started becomes unknown-outcome; other non-completed states reject), extends successor-ID reuse checks to legacy claims/bundles, and adds regression coverage; prior cross-kind operation-ID fix remains in c6c4a6b. Final review marker: reviewed: implementation commits b099d27 and e741979; review-fix commits c6c4a6b and a78c72e; full implementation-review applied for concurrency/transactions, public API/schema compatibility, idempotency, migration, security/redaction, rollback, and failure paths. Verification after final fixes: mise run test -- -k transfer (9 tests) passed; mise run test (123 tests) passed; mise run lint passed; mise run format-check passed; mise run typecheck passed; mise run hooks passed. Integration: c6c4a6b and a78c72e committed on canonical main via guarded control-exec.

Final independent verifier recheck PASS at canonical HEAD a78c72e: AC1-AC5 all pass. Focused evidence: store transfer tests 8/8, CLI transfer/verbose/inspect 3/3, diagnostics 7/7, schema checks 2/2; custom legacy-bundle successor probe rejected claim-id-reused. No actionable defect remains.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented and reviewed atomic active-owner transfer across the Python API, CLI, schema, persistence, and lifecycle tests. Transfer preserves checkpoints, creates fresh successor epochs/tokens, rejects stale, changed, cross-kind, unknown, and legacy-reused requests, serializes contenders, redacts read-only views, and rolls back safely. Review fixes c6c4a6b and a78c72e close operation-ID, replay-state, and migration-ID gaps. Verified with 9 focused transfer tests, the full 123-test suite, lint, format-check, typecheck, and hooks; integrated on canonical main.
<!-- SECTION:FINAL_SUMMARY:END -->
