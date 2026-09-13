---
id: TASK-85.10
title: Implement contextual handles and credential sources
status: Done
assignee:
  - '@pi'
created_date: '2026-09-12 03:23'
updated_date: '2026-09-13 00:20'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.7
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/lease_context.py
  - src/worklease/lease_file.py
  - src/worklease/credentials.py
  - tests/test_lease_context.py
  - tests/test_credentials.py
  - tests/test_cli.py
  - TASK-67
modified_files:
  - internal/handle/handle.go
  - internal/handle/handle_test.go
  - internal/cli/lease_commands.go
  - internal/cli/resource_commands_test.go
  - internal/lease/service.go
  - CHANGELOG.md
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 102000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement private client handles and exclusive credential selection per contract sections 4, 6.2 and 9. Own internal/handle and lifecycle CLI wiring. Independent loops must have stable session-scoped contexts or explicit handles; login agent identity is only metadata.

Persist generated acquire/transfer credentials and exact pending requests before authority dispatch. Use stable cross-process sibling locks and atomic fsync/rename updates, bind authorityId, and preserve recoverable pending state through crashes or failed final writes. Explicit credentials bypass contextual lookup and never mix with handle fields. Read-only inspection/verification does not create files or automatically dispatch pending mutations. No error path prints a token.

Evidence and patterns (the amended contract is normative): `src/worklease/lease_context.py` (context root through git rev-parse with GIT_* stripped), `lease_file.py` (size cap, atomic write, permission checks), `credentials.py` (file and descriptor rules). Tests: all of `tests/test_lease_context.py` and `tests/test_credentials.py`; in `tests/test_cli.py`: test_unwritable_lease_file_fails_before_the_claim_commits, test_late_lease_file_failure_still_returns_the_claim_token (the Go contract never prints tokens; keep the recovery intent only), test_acquire_refuses_to_clobber_a_handle_holding_a_live_claim, test_idempotent_replay_does_not_rewind_the_lease_handle, test_lease_file_singleton_lifecycle_tracks_revision_and_clears, test_lease_file_bundle_and_transfer_handoff, test_non_argv_token_sources_cover_lifecycle_and_prevent_state_changes. Pattern: hum `internal/cli/project_dir_test.go` for working-directory-dependent tests. TASK-67 (Done) shipped the Python version of this idea.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Context tests cover subdirectories, symlinks, linked worktrees and non-Git directories, two sessions in one checkout, and identical contention resources despite distinct sessions/handles.
- [x] #2 Security tests reject unsafe/symlinked/hard-linked/foreign/oversized handles and credentials, validate the pending/ready schema, enforce strict token encoding and produce durable owner-only writes without secret diagnostics.
- [x] #3 Two real processes acquiring different resources into one destination cannot orphan a grant or overwrite an active/pending handle; concurrent mutations reload under a stable lock, and transfer locks two distinct destinations in canonical order.
- [x] #4 Crash tests at pre-dispatch, post-commit/pre-handle-write, release cleanup and both transfer-handle boundaries recover the original request and bearer, never rewind revision, never mint new intent and never leak tokens. Handle persistence tests keep a separate recoveryRequest beside a pending external request and clear both only after confirmed resolution; 85.9 owns end-to-end reconciliation crash tests.
- [x] #5 Selector tests prove explicit credentials bypass even malformed unrelated contextual files, mixed modes fail, authority mismatch cannot mutate, pending recovery works, with explicit reconciliation integration owned by 85.9, stateless acquire retains required inputs, and mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci-go` passes on the final commit
- [x] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented private Go contextual and explicit handles with authority binding, owner-only atomic persistence, strict file/descriptor credentials, duplicate-key/schema validation, stable sibling locks, exact pending requests, conservative committed/unknown failure handling, automatic replay, monotonic revision synchronization, and canonical transfer locking/recovery.

Validation evidence:
- AC1: TestContextRootAndContextualPathAreStableAndSessionScoped; TestContextRootResolvesSymlinksGitSubdirectoriesAndLinkedWorktrees; TestProcessesSerializeOneHandleAndConcurrentMutations.
- AC2: TestHandleAtomicPrivateRoundTripAndRejectsUnsafe; TestCredentialReaderStrictEncodingAndBounded; TestHandleSchemaRejectsTrailingPendingAndAuthorityErrors; TestExistingMalformedHandleIsNeverReplaceable.
- AC3: TestProcessesSerializeOneHandleAndConcurrentMutations; TestReverseTransfersUseCanonicalLocksWithoutDeadlock; TestContextualTransferPersistsSuccessorAndSupportsGeneratedOperationIDs.
- AC4: TestPendingLifecycleRecoversBeforeAndAfterAuthorityDispatch; TestHandleSynchronizationNeverRewindsRevision; TestHandleSchemaRejectsTrailingPendingAndAuthorityErrors; TestContextualTransferPersistsSuccessorAndSupportsGeneratedOperationIDs.
- AC5: TestContextualDefaultRunsCompleteLifecycle; TestExplicitCredentialsCanTransferIntoPrivateSuccessorHandle; TestAcquireRejectsMixedHandleAndStatelessSelection; TestStatusRejectsMixedPrivateAndPublicSelection; TestAcquireDerivesInputBeforeDispatch; mise run ci-go.
- Required repository gates also passed: mise run lint, format-check, test (339 Python tests), and typecheck (0 errors).
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: @C3
created: 2026-09-13 00:20
---
Correction after TASK-88: the no-file-creation statement applies to handle inspection/verification itself. Its read-only SQLite authority open may update shared-memory coordination state and recreate absent owner-private -wal/-shm sidecars for an existing WAL database in a writable directory; amended contract sections 8 and 13 are authoritative.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added secure session-scoped and explicit Go handles, strict credentials, stable locks, exact pending-request recovery, and lifecycle/transfer CLI integration. AC1 is proven by context/symlink/linked-worktree and multi-process tests; AC2 by handle/credential/schema safety tests; AC3 by process serialization and reverse-transfer lock tests; AC4 by pre/post-dispatch replay, release cleanup, transfer, and revision-monotonicity tests; AC5 by contextual/stateless/explicit selector tests and `mise run ci-go`. Full lint, format, Python test, and typecheck gates also pass.
<!-- SECTION:FINAL_SUMMARY:END -->
