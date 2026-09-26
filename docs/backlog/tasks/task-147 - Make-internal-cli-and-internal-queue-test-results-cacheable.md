---
id: TASK-147
title: Make internal/cli and internal/queue test results cacheable
status: To Do
assignee: []
created_date: '2026-09-26 16:28'
labels:
  - testing
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
- [ ] #1 Running mise run test twice in a row with no source changes reports (cached) for internal/cli and internal/queue on the second run
- [ ] #2 Test isolation guarantees (testkit.Home, IsolateProcessEnvironment) are preserved
<!-- AC:END -->
