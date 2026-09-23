---
id: TASK-85.12
title: 'Implement guarded exec, replace-file, and verify'
status: Done
assignee:
  - '@pi-01a095f7'
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 15:18'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.9
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/execution.py
  - src/worklease/execution_context.py
  - src/worklease/replacement.py
  - tests/test_execution.py
  - ../hum/internal/daemon/runtime.go
  - ../hum/internal/app/app.go
  - TASK-79
modified_files:
  - internal/cli/commands.go
  - internal/cli/guard_commands.go
  - internal/cli/guard_commands_test.go
  - internal/cli/lease_commands.go
  - internal/cli/root.go
  - internal/guard/guard.go
  - internal/guard/guard_test.go
  - internal/guard/replace_test.go
  - internal/handle/handle.go
  - internal/handle/handle_test.go
  - internal/lease/helpers.go
  - internal/lease/service.go
  - internal/lease/verify_regression_test.go
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 104000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement supervised exec, serialized expected-hash replacement and read-only verification from contract section 10. Own internal/guard, lease.Verify and exec/replace-file/verify CLI. Use the real handle and reconciliation paths before finalizing tests.

Exec records intent before effects, renews within known lease deadlines, captures bounded output, kills the process group on timeout/ownership uncertainty and reports honest completion/unknown state. It is coordination, not arbitrary-process fencing. Replacement additionally requires the canonical target path resource and localReplaceAllowed. Hash equality after an uncertain rename is evidence for explicit reconciliation, never automatic proof that the old executor ceased.

Native edit hooks verify by default that the selected context or session holds a current valid claim (`--coverage claim`); with `--coverage path` they additionally require the exact canonical path resource of every edited file to be a member of the claim. Both modes fail closed for malformed/unsupported input. Do not parse shell text or implement a Bash first-word exemption.

Evidence and patterns (the amended contract is normative): `src/worklease/execution.py` (bounded capture, deadline covering pipe draining, ownership-loss termination, storage failure handling), `execution_context.py` (`--git-primary` resolution rules), `replacement.py` (symlink rejection, fsync and rename, replay). All 33 tests in `tests/test_execution.py` are edge cases to preserve, notably test_exec_timeout_kills_inherited_pipe_descendant_and_is_inspectable, test_exec_timeout_does_not_wait_for_escaped_grandchild_pipes, test_ownership_loss_terminates_running_process_group, test_exec_storage_failure_terminates_child_as_unknown_outcome, test_exec_decodes_invalid_output_and_normalizes_signal_status, test_git_primary_resolves_linked_symlink_and_separate_git_dir, test_git_primary_ignores_prunable_linked_worktree, test_replacement_is_atomic_preserves_mode_and_replays, test_replacement_rejects_wrong_hash_and_symlink, test_replacement_keeps_ownership_during_atomic_write. hum patterns: `internal/daemon/runtime.go` and `internal/app/app.go` (Setpgid, stop grace, SIGTERM then SIGKILL, bounded output), `internal/cli/run_args_test.go` (argv after `--`). TASK-79 (closed as superseded) records the verify intent.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Exec tests cover literal argv, cwd/git-primary environment isolation, /dev/null stdin, bounded/invalid-UTF8 output, child/signal status and token-free environment/diagnostics.
- [x] #2 Process tests cover timeout including inherited/escaped pipes, ownership/clock/storage failure, uncertain completion and killed-supervisor/orphan limits; no controllable process-group members leak.
- [x] #3 Intent tests prove one guarded operation at a time, started-before-effects and final revision reporting, exact replay without re-execution and changed maxDuration rejection; predecessor unknowns block new guards until explicit reconciliation.
- [x] #4 Replacement tests cover canonical claimed-path membership, symlink/hard-link/parent-swap rejection, expected/content hashes, mode/fsync/rename, completed no-effect failures, and uncertain rename requiring reconciliation even when bytes match.
- [x] #5 Verify/hook tests cover all ownership/authority/pending failures with no writes, native Edit/Write/MultiEdit/NotebookEdit events, default --coverage claim allowing an edit of any file under a valid claim and blocking missing/expired/pending claims, --coverage path denying an unrelated path and allowing a claimed path, unsupported Bash input failing closed, plus MCP reference selection; mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci-go` passes on the final commit
- [x] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Port the Python guarded operation behavior into Go lease/guard services, preserving started-operation, replay, timeout, ownership, and bounded-output semantics.
2. Add canonical target resolution and atomic expected-hash replacement with claim coverage and filesystem safety checks.
3. Add read-only claim/path verification and wire exec, replace-file, and verify into the CLI and native hook inputs.
4. Add focused Go tests matching the task edge cases, then run repository quality gates and record evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Selected as the earliest dependency-ready backlog item. Claimed with Worklease resource backlog-md:/Users/brett/dev/me/worklease/.git:docs/backlog:TASK-85.12 under local-coordination scope; provider mutations are not fenced.

Implemented Go guarded operation services and CLI wiring. Guarded exec uses literal argv, isolated environments, /dev/null stdin, bounded UTF-8-safe capture, process groups, asynchronous renewal, deadline/ownership termination, replay, and handle recovery. Replace-file commits started intent before effects, binds exact request/content hashes, verifies path claim membership, uses pinned directory descriptors and no-follow opens, performs serialized expected-hash rename/fsync, and leaves uncertain outcomes pending. Verify fails closed, supports contextual/explicit/MCP handles, uses existing shared locks without creating them, validates claim or path coverage, and rejects malformed/unsupported native hooks.

Validation: mise run lint; mise run format-check; mise run test (339 Python tests); mise run typecheck; mise run ci-go; go test -count=1 ./internal/guard ./internal/lease ./internal/handle ./internal/cli; git diff --check. Independent verifier review completed after fixes.

Delivered implementation commit ae02d43 and merge commit b4e468a. Post-merge mise run ci-go passed; review findings were fixed and independent verification was rerun.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented Go exec, replace-file, and verify guards with CLI integration and durable handle/replay behavior.

Acceptance evidence:
1. Exec argv/cwd/git-primary/environment/output behavior: TestExecLiteralArgvBoundedInvalidUTF8SignalAndEnvironment, TestExecUsesRequestedCWDAndIsolatesGitEnvironment, TestGitPrimaryResolvesLinkedWorktree, and TestExecUsesArgvDevNullAndCompletesReceipt.
2. Process supervision and uncertain outcomes: TestExecLeaderExitReapsProcessGroupDescendant, TestExecTimeoutLeavesOperationUnresolved, and go test -race ./....
3. Intent/replay/revision rules: TestStartedOperationExclusivityAndCompletionRevision, TestGuardReplayUsesOriginalEpochBeforeCurrentAuthorization, TestSuppliedGuardHashMustMatchCanonicalIntent, and TestExpiredPredecessorUnknownOperationBlocksVerification.
4. Serialized safe replacement: TestReplaceFileExpectedHashPreservesModeAndReplay and TestReplaceFileRejectsSymlinkTargetBeforeCanonicalization.
5. Verify/hooks: TestVerifyEmptyReadOnlyAuthorityFailsClosed, TestExistingSharedLockDoesNotCreateMissingLock, TestHookTargetsAcceptNativeFileEditorsAndRejectBash, and TestHookTargetsRejectMalformedInput.

Repository gates passed: mise run lint, format-check, test (339 tests), typecheck, and ci-go.
<!-- SECTION:FINAL_SUMMARY:END -->
