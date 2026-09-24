---
id: TASK-136
title: 'Test suite health: determinism, speed, and scope'
status: To Do
assignee: []
created_date: '2026-09-24 15:04'
labels: []
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
- [ ] #1 Every subtask is Done or explicitly closed with a recorded reason
<!-- AC:END -->
