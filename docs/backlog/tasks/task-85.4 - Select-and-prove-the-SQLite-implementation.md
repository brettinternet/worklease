---
id: TASK-85.4
title: Select and prove the SQLite driver
status: Done
assignee:
  - '@pi-01a09478'
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 08:01'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.3
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/sqlite.py
  - tests/test_store.py
  - tests/test_gc.py
  - 'https://pkg.go.dev/modernc.org/sqlite'
  - 'https://pkg.go.dev/github.com/ncruces/go-sqlite3'
  - 'https://pkg.go.dev/github.com/mattn/go-sqlite3'
modified_files:
  - internal/store/driver.go
  - internal/store/driver_test.go
  - go.mod
  - go.sum
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-85
priority: high
type: spike
ordinal: 96000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Prove a SQLite driver against the actual authority boundary before selecting it. Read D2/D3/D8 and contract sections 8, 14 and 20. Prefer modernc.org/sqlite; evaluate alternatives only if executable evidence requires it. Own internal/store/driver.go and driver tests plus driver dependency/amendment.

Prove WAL/FULL, writer serialization, rollback, cancellation, crash durability, AUTOINCREMENT, safe actual driver opens of main/WAL/SHM, and read-only observation without creating files or ignoring committed WAL. An lstat check followed by an ordinary path-based driver open is not equivalent to no-follow safety. Record capability limits and amend unsafe assumptions before later tasks rely on them.

Evidence and patterns (the amended contract is normative): `src/worklease/sqlite.py` shows the pragmas the proof of concept relied on (WAL, synchronous FULL, busy_timeout, BEGIN IMMEDIATE) and `connect_readonly` opening `?mode=ro` over a WAL database, which is the read-only behavior section 8 requires the spike to prove. Tests to reproduce in shape: `tests/test_store.py` test_concurrent_acquire_has_one_winner_and_independent_resources_proceed and test_epoch_schema_migration_rolls_back_atomically, `tests/test_gc.py` test_apply_rolls_back_on_injected_sqlite_interruption. Candidate drivers in order: `modernc.org/sqlite` (preferred, pure Go), `github.com/ncruces/go-sqlite3` (pure Go), `github.com/mattn/go-sqlite3` (CGO fallback requiring a D3 amendment).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Driver tests read back every required pragma and prove cross-process BEGIN IMMEDIATE serialization, rollback, bounded cancellation and AUTOINCREMENT non-reuse.
- [x] #2 Crash tests distinguish killed uncommitted writes from durable commits; ambiguous commit errors are not assumed to mean rollback.
- [x] #3 Tests exercise main/WAL/SHM symlink and hard-link rejection, unsafe-path races, private modes, and read-only opens that see committed WAL without creating state; any required design deviation is explicitly amended.
- [x] #4 All four target builds succeed with CGO_ENABLED=0, or an evidence-backed amendment defines CGO/native builds; the selected dependency is pinned and license/vulnerability checks recorded.
- [x] #5 The contract amendment and TASK-85 comment record the driver, settings, evidence and limitations; OpenDriver is available to 85.6 and mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci-go` passes on the final commit
- [x] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a modernc.org/sqlite-backed internal/store driver that opens write and read-only databases with the required pragmas, serialized BEGIN IMMEDIATE writes, context cancellation, and explicit commit outcome classification.
2. Add hermetic driver tests for pragmas, serialization, rollback, cancellation, AUTOINCREMENT, crash durability, filesystem safety constraints, private modes, and WAL-visible read-only observation.
3. Record the selected driver, executable evidence, and residual path-open/no-follow limitations in the product contract and parent task.
4. Pin/audit the dependency, cross-build all four targets with CGO disabled, run mise run ci-go and repository gates, then finalize the task with objective evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented modernc.org/sqlite v1.58.0 OpenDriver and executable driver proofs. Independent review found and prompted fixes for deferred reads, canceled-context commit read-back, post-commit error classification, sidecar TOCTOU, subprocess serialization, read-only mutation snapshots, permissive umask, complete sidecar link coverage, and deterministic race evidence.

Validation: mise run ci-go passed (format, vet/staticcheck, unit, race, govulncheck, CGO-disabled build); repository mise run lint, format-check, test (339 Python tests), and typecheck passed. Explicit CGO_ENABLED=0 builds passed for linux/amd64, linux/arm64, darwin/amd64, and darwin/arm64. modernc.org/sqlite v1.58.0 includes BSD-3-Clause and SQLite license files; govulncheck reported no vulnerabilities.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Selected and pinned modernc.org/sqlite v1.58.0 and added internal/store OpenDriver with WAL/FULL/foreign-key/busy-timeout settings, immediate writes, deferred reads, bounded cancellation, rollback, durable commit outcome classification, read-only WAL observation, and explicit pathname-open limits. Evidence: TestDriverPragmasAndPrivateModes; TestDriverWriteRollbackAndAutoincrementDoesNotReuseCommittedIDs; TestDriverReadIsDeferredAndHeldReadsDoNotBlockWriters; TestDriverCrossProcessBeginImmediateSerializesWriters; TestDriverKilledTransactionsHaveDistinctDurabilityOutcomes; TestCommitErrorOutcomeUsesFreshDurableReadback; TestPostCommitValidationFailureIsDefinitelyCommitted; TestDriverCreatesPrivateStateWithPermissiveUmask; TestDriverRejectsEveryUnsafeMainAndSidecarVariant; TestDriverDeterministicPathSwapIsDetected; TestDriverReadOnlySeesCommittedWALWithoutFilesystemMutation. mise run ci-go and all repository gates passed; four CGO-disabled target builds passed; govulncheck found no vulnerabilities. The product contract and TASK-85 record the selected driver and residual lstat-to-path-open race owned by TASK-85.6.
<!-- SECTION:FINAL_SUMMARY:END -->
