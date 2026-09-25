---
id: TASK-136
title: 'Test suite health: determinism, speed, and scope'
status: Done
assignee:
  - '@pi'
created_date: '2026-09-24 15:04'
updated_date: '2026-09-25 23:07'
labels:
  - reviewed
dependencies: []
priority: medium
type: task
ordinal: 51000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
A review of the test strategy on 2026-09-24 (commit a6509c3) found the suite broadly useful and not bloated, but slow, occasionally nondeterministic, and ungoverned. This parent groups the follow-up work so it can be picked up independently.

Findings:

- Size and coverage are healthy: 32.5k lines of Go tests against 47.3k lines of production code (0.69), 787 top-level tests, 62.9% per-package statement coverage and 77.6% cross-package (`go test -coverpkg=./internal/... ./...`). Copy-paste between tests is low; tests are behavior-named and reuse `internal/testkit` helpers. No wholesale deletion is warranted.
- `cmd/worklease-remote-smoke` (6.3k lines plus a 788-line self-test) is the only test of the shipped binary as a real TLS authority with multiple clients. Keep it; fix its reliability instead.
- Nondeterminism: main CI failed twice in the week before the review because of wall-clock races (doctor clock-skew test, lease remote test under `-race`), and one test fails locally depending on how `backlog` is installed.
- The remote smoke hangs forever on macOS; CI runs it only on linux-x64, so nobody notices.
- Slow feedback: `go test ./...` takes about 81 s wall locally. `internal/cli` (74 s) runs fully serially (18 `t.Parallel()` calls exist in the whole repo), and a few tests spend 1–18 s sleeping, waiting out a 10 s SQLite busy timeout, or running `go build`. The lefthook pre-commit hook runs the full suite on every commit.
- A little dead or test-only code sits in production packages, for example `authority.FakeAuthority`.
- No written testing policy exists, so tests pile up by the review round that found them (`review_regression_test.go`, `review_findings_test.go`, `acceptance_regression_test.go`), sleeps stand in for controlled time, and new tests default to serial.

Rerun the measurements before starting a subtask; numbers drift as the code changes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every subtask is Done or explicitly closed with a recorded reason
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Verify all seven child statuses and acceptance criteria from the authoritative Backlog list; record the evidence, close this coordination-only parent, and commit the provider record on main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Provider evidence (backlog task list --json, 2026-09-25): TASK-136.1 through TASK-136.7 are all Done with all acceptance criteria checked (6/6, 5/5, 6/6, 3/3, 4/4, 3/3, 3/3 respectively). Latest child TASK-136.5 was merged via 8c13a09 and finalized in 0d2ceb5; all four quality gates and focused race checks passed there. This parent has no code change; only the authoritative Backlog record is committed on main.

Post-completion review: TASK-136.1–136.7 Done. No follow-up beyond TASK-136's children.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
All seven subtasks are Done with every acceptance criterion checked; parent verified from Backlog task list JSON and closed.
<!-- SECTION:FINAL_SUMMARY:END -->
