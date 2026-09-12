---
id: TASK-85.3
title: Build the Go test foundation
status: Done
assignee:
  - '@pi-01a0945b'
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 07:13'
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
Provide the small shared test foundation used by the Go tasks: isolated private homes, controllable wall/monotonic clocks, deterministic identifiers/credentials, CLI execution and bounded subprocess helpers. Read contract sections 3, 14 and 18, and the cited Python concurrency/process/Git fixtures as behavior evidence.

Own internal/testkit. Add helpers only when this task or an actual downstream test exercises them; do not introduce skipped database fixtures or a broad unused framework. Later tasks may extend helpers for their concrete crash, permissions, Git and process scenarios. Tests should prove behavior, not reproduce implementation structure.

Evidence and patterns (the amended contract is normative): hum test shapes in `cmd/hum/integration_test.go` (built-binary integration), `internal/cli/root_test.go` and siblings (in-process NewRootCommand with injected writers), `internal/daemon/runtime_test.go` (bounded waits and cleanup). Python fixture shapes: `tests/test_store.py` test_concurrent_acquire_has_one_winner_and_independent_resources_proceed and test_overlapping_bundles_across_processes_have_one_winner (multi-process contention), `tests/test_execution.py` test_exec_timeout_kills_inherited_pipe_descendant_and_is_inspectable (grandchild holding an inherited pipe), all nine tests in `tests/test_lease_context.py` (Git root, nested symlink, linked worktree, foreign-owned directory), all eight tests in `tests/test_credentials.py` (permission fixtures).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Clock, ID/token and isolated-home helpers are exercised under go test and -race; environment helpers isolate WORKLEASE_* and GIT_* values without concurrent process-global mutation.
- [x] #2 CLI helpers exercise the version command through injected writers and parse its result; subprocess helpers re-execute the test binary with explicit markers and bounded cleanup.
- [x] #3 Bounded subprocess failure diagnostics identify the helper and timeout, and no child survives test cleanup.
- [x] #4 Git fixtures actually used by resource/handle tests are documented with main/linked/symlink context expectations; downstream tasks own adding unused fixtures when needed.
- [x] #5 Every exported helper has a demonstrated consumer or its own meaningful acceptance test; mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci-go` passes on the final commit
- [x] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect the referenced Go/Python test patterns and define only the internal/testkit helpers exercised by this task: deterministic clock/identity data, isolated homes/environment maps, CLI invocation, and bounded test-binary subprocesses.
2. Implement helpers with race-safe instance state and focused acceptance tests, including timeout diagnostics, child cleanup, and documented Git fixture expectations without unused fixtures.
3. Run ci-go and repository gates, review the diff, merge the committed work to main, and record objective completion evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Resumed under a fresh Worklease local-coordination claim.

Implemented in ce8ecb6 and fast-forwarded to main. Verification passed: TestClockAndGeneratorAreDeterministicAndRaceSafe; TestHomeAndEnvironmentArePrivateAndProcessIsolated; TestRunCLIParsesInjectedVersionResult; TestRunTestProcessSuccessAndBoundedCleanup; go test and go test -race for internal/testkit; mise run ci-go; repository lint, format-check, test, and typecheck.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added the focused Go test foundation. AC1: TestClockAndGeneratorAreDeterministicAndRaceSafe and TestHomeAndEnvironmentArePrivateAndProcessIsolated prove race-safe clocks, deterministic IDs/tokens, private homes, and isolated env maps. AC2: TestRunCLIParsesInjectedVersionResult and the echo helper prove injected CLI and marked test-binary execution. AC3: TestRunTestProcessSuccessAndBoundedCleanup proves timeout diagnostics, process-group termination, and reaping. AC4: package documentation records main/linked/symlink Git context expectations while deferring unused fixtures. AC5/DoD: all exported helpers are consumed by acceptance tests and mise run ci-go passed on ce8ecb6.
<!-- SECTION:FINAL_SUMMARY:END -->
