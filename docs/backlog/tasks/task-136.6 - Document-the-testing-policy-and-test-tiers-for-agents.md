---
id: TASK-136.6
title: Document the testing policy and test tiers for agents
status: To Do
assignee: []
created_date: '2026-09-24 15:05'
updated_date: '2026-09-24 15:10'
labels: []
dependencies: []
parent_task_id: TASK-136
priority: medium
type: docs
ordinal: 57000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The repo has no written testing guidance; AGENTS.md lists only the quality gates. Tests pile up by the review round that found them: files such as `review_regression_test.go`, `review_findings_test.go`, `acceptance_regression_test.go`, and `regression_test.go` in queue, lease, mcp, guard, authority, and queueui group tests by origin rather than by behavior. Sleeps stand in for controlled time, one test builds the binary, and almost nothing runs in parallel (see TASK-136). Agents doing backlog tasks follow AGENTS.md (CLAUDE.md is a symlink to it), so a short policy there is the cheapest way to keep the suite useful without bloat.

Test tiers exist, but their gating is spread across `mise.toml`, `lefthook.yml`, and `.github/workflows/ci.yml`. Document them in one place:
- Package tests: `mise run test`. Run by the pre-commit hook and by CI on four platforms.
- Race: `mise run race`. CI linux-x64.
- End-to-end: `mise run e2e`, which runs `scripts/test-e2e.sh`: the built-binary smoke, the remote smoke, and the doc test. CI linux-x64 only.
- Opt-in benchmarks: `mise run queue-benchmark` and the latency probes gated by `QUEUE_*_SAMPLES` environment variables. Pull requests get a relative comparison.
- Manual VM remote smoke: `mise run remote-smoke-vm`.

`scripts/test-e2e.sh` also reruns 12 named cli and mcp tests with `-run`. `mise run test` already runs them in the same `mise run ci` invocation, and the Go test cache cannot reuse the earlier result because the `-run` flag differs. Delete that line.

The policy should cover these points in about 40 lines or fewer:
- Put a test in the file for the behavior it covers. Do not create new files named after where a finding came from, and do not mass-rename the existing ones.
- Add a regression test for a fixed bug at the lowest layer that reproduces it.
- Control time with `testkit.Clock` or an injected clock. Never sleep to wait for a TTL or timeout.
- Prefer in-process CLI and MCP calls (`testkit.RunCLI`) to building binaries.
- Run subprocesses through `testkit.RunTestProcess` with bounded timeouts.
- Call `t.Parallel()` when a test touches no process-global state.
- Write benchmarks as `Benchmark*` functions or gate them behind an environment variable, and never assert absolute latency in the default run.
- Reserve the remote smoke for shipped-binary, multi-process behavior that in-process tests cannot reach.
- Tests of test harnesses cover only their verification logic.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AGENTS.md has a Testing section that covers the policy points above in about 40 lines or fewer, plus a table of the test tiers showing where each runs
- [x] #2 The Backlog.md managed block in AGENTS.md is intact (refresh it with `backlog agents --update-instructions` if needed)
- [ ] #3 `scripts/test-e2e.sh` no longer reruns named Go tests, and `mise run e2e` passes on linux-x64
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Testing policy and tier table added to AGENTS.md (Testing section, outside the Backlog.md managed block, which is unchanged). Remaining: delete the named-test rerun from scripts/test-e2e.sh and confirm `mise run e2e` passes on linux-x64 (CI); the remote smoke currently hangs on macOS (TASK-136.2), so run it locally only after that is fixed.
<!-- SECTION:NOTES:END -->
