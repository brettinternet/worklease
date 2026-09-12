---
id: TASK-85.12
title: 'Implement guarded exec, replace-file, and verify'
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.8
  - TASK-85.10
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/execution.py
  - src/worklease/execution_context.py
  - src/worklease/replacement.py
  - tests/test_execution.py
  - ../hum/internal/daemon/runtime.go
  - ../hum/internal/app/app.go
  - TASK-79
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 104000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A lease is only useful if local work can be tied to it. Guarded exec runs one exact command under renewed ownership and records intent before effects; replace-file performs an expected-hash atomic write; verify (the TASK-79 intent) lets agent tools that mutate through their own editors check ownership cheaply first. Contract section 10 fixes the semantics and hum provides the POSIX process-group patterns.

Read first: contract sections 4 (exec, replace-file, verify rows), 6.1 (exit 124, child status, ownership-lost), 7.4, 7.5, 7.11, 10, 18 (guard and Verify sketches). Patterns: `../hum/internal/daemon/runtime.go` and `../hum/internal/app` (process start with Setpgid, stop grace, SIGTERM then SIGKILL escalation, bounded output capture), `../hum/internal/cli/run_args_test.go` (argv after `--`). Python evidence: `src/worklease/execution.py` (bounded capture, deadline covering pipe draining, ownership-loss termination, storage failure handling), `execution_context.py` (`--git-primary` resolution rules), `replacement.py` (symlink rejection, fsync and rename, replay); all 33 tests in `tests/test_execution.py` are the edge cases to preserve, notably test_exec_timeout_kills_inherited_pipe_descendant_and_is_inspectable, test_exec_timeout_does_not_wait_for_escaped_grandchild_pipes, test_ownership_loss_terminates_running_process_group, test_exec_storage_failure_terminates_child_as_unknown_outcome, test_exec_decodes_invalid_output_and_normalizes_signal_status, test_git_primary_resolves_linked_symlink_and_separate_git_dir, test_git_primary_ignores_prunable_linked_worktree, test_replacement_is_atomic_preserves_mode_and_replays, test_replacement_rejects_wrong_hash_and_symlink, test_replacement_keeps_ownership_during_atomic_write; TASK-79 acceptance criteria for verify.

Deliver in `internal/guard`: `Exec` and `ReplaceFile` per contract 10.1 and 10.2 using `lease.Service.BeginOperation` and `CompleteOperation`; the git-primary resolution helper; environment sanitization; bounded capture with byte counts and truncation flags; a heartbeat goroutine at ttl/2 with ownership-loss termination; a deadline covering draining; SIGTERM, 2 s grace, SIGKILL on the process group; exit-status normalization (signals to 128 plus n). Deliver `lease.Service.Verify` in `internal/lease` per contract 10.3 (read-only, ordered checks, cause codes). Deliver in `internal/cli`: `exec`, `replace-file`, and `verify` (including `--hook claude-code` stdin handling and exit 2 block semantics) using the TASK-85.10 claim selection; help text states that verify is a cooperative precondition, not a fence.

Owned paths: `internal/guard`, `internal/lease/verify.go`, `internal/cli/exec.go`, `replace_file.go`, `verify.go` and tests. Out of scope: MCP exposure (TASK-85.15), hook installation (TASK-85.16).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Exec tests prove exact argv without shell interpretation (an argument containing $HOME and ; arrives literally), child statuses 0, 3, and signal termination map to 0, 3, and 128 plus the signal number, stdout and stderr over 1 MiB are truncated with byte counts and flags, invalid UTF-8 is replaced, stdin is /dev/null, WORKLEASE_CLAIM_ID and WORKLEASE_OPERATION_ID are set with no token in the child environment, and --cwd and --git-primary strip the GIT_* routing variables.
- [ ] #2 Termination tests prove --max-duration kills a child whose grandchild holds the inherited stdout pipe within 3 s and exits 124 with timedOut true and the operation completed, a heartbeat failure injected mid-run (claim expired through the clock or transferred by a subprocess) terminates the process group and fails ownership-lost, a storage failure injected after spawn terminates the child and leaves the operation started (unknown-outcome on replay), and no processes remain in the group afterwards.
- [ ] #3 Ledger tests prove exec writes started before spawn (a child that reads the database sees its own started row), writes completed with the receipt, replays the receipt without re-running (a counter file is unchanged), and a claim over three resources executes once with a single operation row.
- [ ] #4 replace-file tests prove atomic replacement preserving mode, rejection of a symlinked target or content file and of a wrong expected hash with actualSha256 (exit 3), rejection under a local-coordination claim (64), deterministic replay, completion of a started operation whose rename already happened, and --git-primary resolution across a linked worktree, a symlinked worktree path, and a separate git dir while ignoring a prunable worktree.
- [ ] #5 verify tests prove the success fields, each failure cause (missing-handle, stale-claim, invalid-token, claim-expired, stale-revision, resource-mismatch, unknown-outcome-pending) with exit 2, that the database mtime, handle mtime, and events count are unchanged after 100 verify calls, and that --hook claude-code consumes stdin JSON and blocks with exit 2 and a one-line stderr message; `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
