---
id: TASK-147
title: Make internal/cli and internal/queue test results cacheable
status: Done
assignee:
  - '@pi'
created_date: '2026-09-26 16:28'
updated_date: '2026-09-27 16:40'
labels:
  - testing
  - reviewed
dependencies: []
priority: medium
ordinal: 81000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
go test never reuses cached results for internal/cli (~244s) or internal/queue (~105s), even with no source changes; every other package reports (cached). This makes full `mise run test` runs take ~6 minutes. Go caches a test result only when the files and environment variables the test read are unchanged; these packages likely read per-run paths (temp homes, subprocess state) or changing env. Find what invalidates the cache (e.g. GODEBUG=gocachetest=1 go test ./internal/queue) and remove the per-run inputs without weakening isolation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Running mise run test twice in a row with no source changes reports (cached) for internal/cli and internal/queue on the second run
- [x] #2 Test isolation guarantees (testkit.Home, IsolateProcessEnvironment) are preserved
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reproduce cache misses with the exact mise task and inspect Go cache diagnostics. 2. Preserve existing testkit.Home and TestMain isolation when no code defect remains. 3. Validate two unchanged mise run test invocations and the isolation tests; close the stale task without unnecessary source edits.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Go cache diagnostics showed clean cache hits for internal/cli and internal/queue with GODEBUG=gocachetest=1. The first non-diagnostic mise run test re-executed tests because changing GODEBUG changed the cache input; an immediately repeated plain mise run test returned (cached) for both packages. A direct go test outside mise initially failed because backlog resolved to an invalid mise shim, whereas mise exec -- go test passed; this is not a test-cache regression. Home and process isolation implementations remain unchanged; focused internal/testkit isolation tests passed.

Two consecutive unchanged `mise run test` runs completed; the second printed (cached) for internal/cli and internal/queue. `mise exec -- go test -race -count=3 -run "TestHomeAndEnvironmentArePrivateAndProcessIsolated|TestIsolateProcessEnvironmentRemovesHostileConfiguration" ./internal/testkit` passed. No source changes were needed. Review: the exact acceptance behavior is already present; no scoped defect remains.

Delivery: commit d097212 (task verification record) fast-forward merged into local main. No push requested. Next step: none; both criteria verified and source code unchanged.

Post-completion review found that two CLI setup tests changed the parent test process cwd, creating per-run Go cache inputs. Isolated both using existing testkit.RunIsolatedTest in a worktree (a6143f2), merged to main. Focused race -count=3 and staged hooks passed; two unchanged mise run test invocations now report (cached) for internal/cli and internal/queue. No further scoped follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Both packages reuse cached results on the second unchanged mise run test. A post-completion review found two CLI setup tests invalidating the cache by changing the parent cwd; isolated them with testkit.RunIsolatedTest (a6143f2). Focused race tests, staged hooks, and two full test runs passed.
<!-- SECTION:FINAL_SUMMARY:END -->
