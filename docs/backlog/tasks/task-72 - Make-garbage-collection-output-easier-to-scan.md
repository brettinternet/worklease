---
id: TASK-72
title: Make garbage-collection output easier to scan
status: Done
assignee:
  - '@pi-01a09387'
created_date: '2026-09-12 02:11'
updated_date: '2026-09-12 03:14'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
  - tests/test_gc.py
  - docs/cli-reference.md
  - CHANGELOG.md
modified_files:
  - src/worklease/cli.py
  - tests/test_cli.py
  - docs/cli-reference.md
  - CHANGELOG.md
priority: medium
type: enhancement
ordinal: 79000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Human-readable `gc` output exposes storage field names such as `bundleEpochs`, several absolute timestamps, and null placeholders. The result is technically complete but makes it difficult to answer the operator questions: what is eligible, how old is it, what remains protected, and what command is safe to run next.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Dry-run text output clearly identifies that no records were changed, states the retention window and cutoff in concise human terms, and shows the total number of eligible records.
- [x] #2 Eligible record groups use readable labels, counts, and compact oldest/newest ages; zero-count groups and missing ranges do not emit raw `null` values.
- [x] #3 Protected unresolved-operation groups remain visible with enough information to explain why collection cannot remove them.
- [x] #4 The apply hint remains copy-pasteable, preserves the exact cutoff used by the dry run, and is omitted when there is nothing eligible to collect.
- [x] #5 `gc --apply` clearly reports what was collected and distinguishes a successful no-op; JSON output remains schema-compatible and complete.
- [x] #6 CLI contract tests use a fixed clock to cover eligible, empty, protected, dry-run, apply, and JSON cases; human-readable output documentation is updated, and CHANGELOG `Unreleased` records the changed text output.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Replace raw GC text fields with a concise dry-run/apply summary, a fixed readable group order, compact ages derived from capturedAt, and actionable protection/hint lines while leaving JSON untouched.
2. Add deterministic renderer/CLI tests for eligible, empty, protected, apply, no-op, fixed-time ages, hints, and JSON compatibility.
3. Update the CLI text grammar and changelog, then run focused and repository-wide quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented compact GC text summaries with fixed readable group ordering and ages derived from capturedAt. Dry runs explicitly report no mutation, retention, exact cutoff, eligible totals, protected unresolved operations, and an exact-cutoff apply hint; apply distinguishes collected records from a no-op. JSON payload construction is unchanged.

Review found the initial tests exercised only the renderer; added a fixed-clock CLI/store integration test covering eligible and protected records, JSON dry run, apply, and repeated no-op.

Validation after rebasing onto main: mise run lint; mise run format-check; mise run test (323 core + 19 SDK tests); mise run typecheck (0 errors); mise run hooks.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Improved human-readable garbage-collection output with explicit dry-run/apply outcomes, eligible and collected totals, readable nonzero groups, compact ages, protected-operation explanations, and exact-cutoff apply guidance. JSON output is unchanged. Added fixed-clock CLI/store coverage and updated documentation and changelog. Verified with lint, format-check, 342 tests, typecheck, hooks, and post-rebase quality gates; merged to main as 2e5d6d9.
<!-- SECTION:FINAL_SUMMARY:END -->
