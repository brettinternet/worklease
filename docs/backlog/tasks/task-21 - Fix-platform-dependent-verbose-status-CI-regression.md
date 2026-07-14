---
id: TASK-21
title: Fix platform-dependent verbose status CI regression
status: In Progress
assignee:
  - '@codex-main'
created_date: '2026-07-14 22:13'
labels:
  - ci
  - test
dependencies: []
priority: high
type: bug
ordinal: 22000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Keep the verbose status read-only regression test deterministic across configured SQLite builds. The baseline database inspection may create WAL sidecars after the filesystem snapshot on macos-15-intel, causing main CI to report a false mutation by status_verbose.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The verbose status read-only test passes on every configured CI runner without treating baseline SQLite sidecar creation as a status mutation.
- [ ] #2 The test continues to verify database row counts and redacted diagnostic output.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reproduce the failed main CI test and identify whether the baseline query or status_verbose creates sidecars. 2. Move the filesystem baseline after the setup query so the test compares the status call against a complete pre-call snapshot. 3. Run the focused test and full quality gates, then push the fix to main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Main CI run 29371073146 at 34e6c98 failed only on macos-15-intel in test_verbose_status_is_redacted_deterministic_and_read_only. The failure showed leases.sqlite3-wal and leases.sqlite3-shm appearing between the test's early tree snapshot and its post-status snapshot; the test's baseline sqlite3 query occurs between those snapshots. Local reproduction with sidecars removed confirms the baseline query creates both files before status_verbose runs.
<!-- SECTION:NOTES:END -->
