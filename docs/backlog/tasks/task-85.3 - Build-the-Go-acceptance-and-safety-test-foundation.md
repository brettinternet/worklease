---
id: TASK-85.3
title: Build the Go test foundation
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 05:51'
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
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Clock, ID/token and isolated-home helpers are exercised under go test and -race; environment helpers isolate WORKLEASE_* and GIT_* values without concurrent process-global mutation.
- [ ] #2 CLI helpers exercise the version command through injected writers and parse its result; subprocess helpers re-execute the test binary with explicit markers and bounded cleanup.
- [ ] #3 Bounded subprocess failure diagnostics identify the helper and timeout, and no child survives test cleanup.
- [ ] #4 Git fixtures actually used by resource/handle tests are documented with main/linked/symlink context expectations; downstream tasks own adding unused fixtures when needed.
- [ ] #5 Every exported helper has a demonstrated consumer or its own meaningful acceptance test; mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
