---
id: TASK-88
title: >-
  Fix guard, MCP handle, read-only, and redaction defects from the Go rewrite
  security review
status: Done
assignee:
  - '@brett'
created_date: '2026-09-12 22:22'
updated_date: '2026-09-12 22:31'
labels:
  - go-rewrite
dependencies: []
references:
  - docs/reviews/task-85.18-independent-review.md
priority: high
type: bug
ordinal: 113000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
An independent review of the TASK-85 Go rewrite (ownership, replay, SQLite locking, handle isolation, GC/watch, MCP redaction) reproduced several adapter-edge defects with the built binary. The authority core held; the failures sit in the exec supervisor, the MCP handle lifecycle, read-only storage opens, and output redaction.

Reproduced:
- exec: child exit is selected while the guard's own RenewOperation is in flight; CompleteOperation then fails stale-revision (4/30 runs at TTL 2s, sleep 0.995s). The operation stays started and the CLI Failure hook treats stale-revision as definitive no-commit, rewriting the handle to ready at the pre-exec revision.
- MCP: mutation/recoverPending/renewLoop leave the handle pending after a definitive no-commit failure (claim-expired), so every later release/checkpoint fails operation-request-mismatch. Contended acquire leaves an orphan pending handle and reports commitState unknown.
- Read-only opens (status, list, doctor) recreate worklease.db-wal/-shm when absent; the driver test only proved the no-creation claim while a writer was open.
- RedactString eats any 64-hex run, so contextual pendingPath values print as ctx-[REDACTED].json.
- replace-file wraps raw temp-file errors as uncertain, leaving a started operation for a proven no-effect failure.
- renewLoop reads r.ttl without the mutex and exits without marking status; exec kills the child on any renew error.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Exec waits for an in-flight internal renewal before recording completion; a child that exits during a renewal completes with its real exit status and no stale-revision failure
- [x] #2 A guard failure after the started intent is committed never clears the pending request from the handle and reports commitState unknown
- [x] #3 MCP heartbeat, checkpoint, release, recovery, and automatic renewal clear the pending request on a definitive no-commit failure and keep it on an uncertain one, using the same classification as the CLI
- [x] #4 A contended MCP acquire removes its pending grant, reports commitState not-committed, and dispatches under the handle lock
- [x] #5 Read-only opens either leave the WAL sidecars untouched or the contract, doctor text, and driver test document that SQLite creates private sidecars; a test covers the sidecars-absent case
- [x] #6 Contextual handle paths and SHA-256 hash fields survive output redaction while 64-hex bearer tokens are still redacted
- [x] #7 replace-file records a completed failure for temp-file creation or write errors before rename
- [x] #8 renewLoop reads the runtime TTL under the mutex and marks status stopped on every exit; exec keeps running on non-ownership renewal errors until the lease deadline
- [x] #9 mise run lint, format-check, test, and typecheck pass
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Move definitive-no-commit classification into internal/reason and pending-request clearing into internal/handle so CLI and MCP share them.
2. guard.Exec: drain an in-flight renewal before completion, terminate only on ownership/clock renewal errors, mark post-start failures commitState unknown, and pass a started flag to Lifecycle.Failure; RunGuardedOperation reports when the started intent committed; ReplaceFile wraps pre-rename temp errors as definitive.
3. CLI: Failure hook clears pending only before start; mutationFailure preserves an existing commitState.
4. MCP: mutation/recoverPending/renewLoop clear pending on definitive failures; acquire takes the handle lock, removes a failed pending grant, and classifies commitState; renewLoop reads ttl under the mutex and marks stopped on every exit.
5. output.Redact: skip the 64-hex heuristic for path and SHA-256 keys.
6. Read-only sidecars: add a driver test for the sidecars-absent case documenting private creation; amend contract section 8 and driver comments.
7. Add regression tests for each fix, run mise gates, record evidence, commit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Fixes: reason.DefinitiveNoCommit and reason.OwnershipRenewalFailure hold the shared classification; handle.ClearPending replaces the CLI-only clearPending. guard.Exec drains an in-flight renewal at the done label before CompleteOperation, continues on non-ownership renewal errors, and marks every post-start failure commitState unknown with Lifecycle.Failure(err, started=true); RunGuardedOperation takes an onStarted hook so ReplaceFile can do the same, and temp-file errors before rename are definitive invalid-path failures. CLI Failure hook clears pending only when started=false; mutationFailure preserves an existing commitState. MCP mutation, recoverPending, recoverAcquire, and renewLoop clear pending on definitive failures; acquire takes the handle lock, removes a failed pending grant, and classifies commitState; renewLoop reads ttl under s.mu and marks stopped via defer. output.redact skips the 64-hex heuristic for *sha256 and *path keys. Read-only sidecar creation is a documented SQLite limit (contract sections 8 and 13 amended via backlog doc update, driver comment, new driver test) because modernc cannot refuse -shm creation and immutable/nolock modes are unsafe against concurrent writers.

Evidence: new tests TestExecCompletesWhenChildExitsDuringRenewal, TestExecPostStartFailureReportsUnknownAndStartedLifecycle, TestReplaceFileTempCreationFailureRecordsCompletedFailure, TestDefinitiveMutationFailureClearsPendingRequest, TestContendedAcquireRemovesPendingGrant, TestAutomaticRenewalDefinitiveFailureClearsPendingAndStops, TestRedactKeepsHashAndPathFieldsButRedactsBareTokens, TestGuardLifecycleKeepsPendingRequestAfterStart, TestMutationFailurePreservesGuardCommitState, TestDefinitiveNoCommitAndOwnershipRenewalClassification, TestDriverReadOnlyWithoutSidecarsCreatesOnlyPrivateSidecars all pass. Built-binary sweep (TTL 2s, sleep 0.995-1.02s, 30 runs) went from 4 stale-revision failures before the fix to 0 after. go test -race passes for internal/mcp, internal/guard, internal/cli. mise run lint, format-check, typecheck, and test pass.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Fixed the exec renew/complete race, post-start pending clearing, MCP pending-handle wedging and orphan grants, replace-file no-effect classification, renew-loop status and data race, and over-broad redaction; documented the read-only WAL sidecar limit in the contract with a driver test. Verified with eleven new regression tests, a 30-run built-binary race sweep (4/30 failures before, 0/30 after), go test -race on the changed packages, and the mise lint, format-check, typecheck, and test gates.
<!-- SECTION:FINAL_SUMMARY:END -->
