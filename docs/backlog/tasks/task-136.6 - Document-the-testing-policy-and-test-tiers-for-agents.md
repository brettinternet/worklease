---
id: TASK-136.6
title: Document the testing policy and test tiers for agents
status: Done
assignee:
  - '@brett'
created_date: '2026-09-24 15:05'
updated_date: '2026-09-24 23:23'
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
- [x] #3 `scripts/test-e2e.sh` no longer reruns named Go tests, and `mise run e2e` passes on linux-x64
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Preserve the testing policy and e2e simplification already merged to main (6194a1b, 12bc608). 2. Verify native linux-x64 CI run 36053786416 passed mise run ci, including e2e, on descendant df51cab; check the remaining criterion. 3. Finalize Backlog state from primary checkout, commit the generated task update on an isolated worktree, fast-forward main, and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AGENTS.md testing policy and tier table were committed in 6194a1b; managed Backlog.md block remained intact. Redundant named Go test reruns were removed from scripts/test-e2e.sh in 12bc608 and merged to main. Earlier macOS arm64 lint, format-check, test, typecheck and staged hooks passed; Linux arm64 built-binary smoke passed. Emulated Linux amd64 attempts crashed during compilation and did not provide acceptance evidence. Native GitHub Actions CI run 36053786416, Quality (linux-x64) job 107815610349 at df51cab (descendant of 12bc608), passed mise run ci: e2e ran scripts/test-e2e.sh, remote smoke group 5 passed, documentation examples passed, and e2e finished in 65.12s. The current script has no named Go test reruns. Current macOS arm64 lint, format-check, test and typecheck passed. All three acceptance criteria are checked.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Documented the agent testing policy/tier table (6194a1b) and removed redundant named-test reruns from e2e (12bc608). Verified linux-x64 e2e via passing CI run 36053786416 on descendant df51cab and reran local quality gates; all acceptance criteria met.
<!-- SECTION:FINAL_SUMMARY:END -->
