---
id: TASK-85.3
title: Build the Go test foundation
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.2
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - ../hum/cmd/hum/integration_test.go
  - ../hum/internal/cli/root_test.go
  - ../hum/internal/daemon/runtime_test.go
  - tests/test_store.py
  - tests/test_execution.py
  - tests/test_lease_context.py
  - tests/test_credentials.py
parent_task_id: TASK-85
priority: high
type: task
ordinal: 95000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Every later task must prove behavior with hermetic, race-safe tests, and unattended loops cannot afford each task inventing its own fixtures, clocks, or subprocess helpers. This task provides the shared kit and the conventions of contract section 14 ("Test conventions") so implementation tasks only add focused tests.

Read first: contract sections 2 (D15), 3, 14 (test conventions), and 18. Patterns: `../hum/cmd/hum/integration_test.go` (built-binary integration), `../hum/internal/cli/*_test.go` (in-process NewRootCommand tests with injected writers), `../hum/internal/daemon/*_test.go` (bounded waits and cleanup). Python evidence for fixture shapes: `tests/test_store.py` test_concurrent_acquire_has_one_winner_and_independent_resources_proceed and test_overlapping_bundles_across_processes_have_one_winner (multi-process contention), `tests/test_execution.py` test_exec_timeout_kills_inherited_pipe_descendant_and_is_inspectable (descendant holding an inherited pipe), `tests/test_lease_context.py` (Git root, nested symlink, linked worktree), `tests/test_credentials.py` (permission fixtures).

Deliver in `internal/testkit` (imported only by `_test.go` files and test helper mains):

- `Home(t)` creating an isolated 0700 home under `t.TempDir()` and returning the environment for subprocesses; `Env(t)` helpers that clear `WORKLEASE_*` and `GIT_*` variables.
- `Clock`: a controllable clock with `Now`, `Monotonic`, `Advance`, `Set`, and a documented regression helper, satisfying the contract lease.Clock shape.
- `IDs`: deterministic identifier and token generator producing 32-hex values from a seed, plus `TokenOf(i)` so redaction tests know which secrets to search for.
- `Run(t, args...)` executing `cli.NewRootCommand` in-process with injected writers and returning exit code, stdout, stderr, and parsed JSON when `--json` is present; `RunBinary(t, ...)` running a prebuilt binary named by an environment variable and skipping when unset.
- Subprocess helpers: `Helper(t, name, env...)` re-executing the test binary (`os.Args[0]`) with `WORKLEASE_TEST_HELPER=name`; `WaitFor` helpers with deadlines that fail with goroutine dumps instead of hanging; a process-group fixture that starts a child whose grandchild holds an inherited pipe and asserts full termination.
- Git fixtures: `Repo(t)` initializing a repository with one commit, `Worktree(t, repo)` adding a linked worktree, symlink helpers, and `CommonDir(path)` returning the resolved git common dir.
- Filesystem assertions: `AssertMode`, `AssertOwnerOnly`, `AssertNoSymlink`, `AssertNoSecret(t, text, secrets...)`.
- Database assertions: `OpenReadOnly(t, home)` returning `*sql.DB` for row counts; until TASK-85.6 lands it may skip with a documented message.
- `internal/testkit/README.md` listing each helper and when to use it.

Owned paths: `internal/testkit`. Out of scope: store, lease, or CLI behavior beyond the existing version command.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 internal/testkit compiles and its own tests pass under `go test` and `go test -race`, covering the clock (advance, set, regression), deterministic IDs and tokens, isolated homes with 0700 permissions and a cleared environment, and JSON parsing of Run output using the version command.
- [ ] #2 Helper(t, name) re-executes the test binary as a subprocess; a test proves two helper subprocesses run concurrently with bounded waits, and a deliberately hanging helper is killed at the deadline with a failure message naming the helper and the elapsed time.
- [ ] #3 The process-group fixture proves a child with a grandchild holding an inherited pipe is fully terminated by the kill helper within 3 seconds on Linux and macOS, with no surviving processes in the group.
- [ ] #4 Git fixtures create a repository, a linked worktree, and a symlinked path, and a test proves CommonDir returns the same value from the main worktree and the linked worktree and a different value for a second repository.
- [ ] #5 internal/testkit/README.md documents every exported helper, and `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
