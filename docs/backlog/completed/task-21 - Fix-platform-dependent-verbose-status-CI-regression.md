---
id: TASK-21
title: Fix platform-dependent verbose status CI regression
status: Done
assignee:
  - '@codex-main'
created_date: '2026-07-14 22:13'
updated_date: '2026-07-14 22:17'
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
- [x] #1 The verbose status read-only test passes on every configured CI runner without treating baseline SQLite sidecar creation as a status mutation.
- [x] #2 The test continues to verify database row counts and redacted diagnostic output.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reproduce the failed main CI test and identify whether the baseline query or status_verbose creates sidecars. 2. Move the filesystem baseline after the setup query so the test compares the status call against a complete pre-call snapshot. 3. Run the focused test and full quality gates, then push the fix to main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Main CI run 29371073146 at 34e6c98 failed only on macos-15-intel in test_verbose_status_is_redacted_deterministic_and_read_only. The failure showed leases.sqlite3-wal and leases.sqlite3-shm appearing between the test's early tree snapshot and its post-status snapshot; the test's baseline sqlite3 query occurs between those snapshots. Local reproduction with sidecars removed confirms the baseline query creates both files before status_verbose runs.

Validation passed: focused test tests.test_store.StoreTests.test_verbose_status_is_redacted_deterministic_and_read_only; mise run lint; mise run format-check; mise run test (128 tests); mise run typecheck; mise run hooks. Pushed c9bcc32707c8bd808329b891faa46fbfa435c3e7 to main. GitHub Actions run 29372414474 (https://github.com/brettinternet/worklease/actions/runs/29372414474) completed successfully across Native linux-x86_64, Native macos-x86_64, Quality ubuntu-latest, Quality macos-14, Quality macos-15-intel, and Native macos-arm64.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Moved the verbose-status filesystem baseline after the baseline SQLite query, preventing macos-15-intel WAL sidecar creation from appearing as a status mutation while retaining all row-count, redaction, and deterministic projection assertions. Verified with focused test, mise run lint, mise run format-check, mise run test (128 tests), mise run typecheck, mise run hooks, and successful GitHub Actions run 29372414474 at main commit c9bcc32707c8bd808329b891faa46fbfa435c3e7.
<!-- SECTION:FINAL_SUMMARY:END -->
