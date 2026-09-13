---
id: TASK-93
title: Omit zero requestNotAfter from public op inspect and history output
status: Done
assignee:
  - '@pi-01a09831'
created_date: '2026-09-12 22:39'
updated_date: '2026-09-13 00:41'
labels:
  - go-rewrite
dependencies: []
references:
  - internal/ledger/ledger.go
modified_files:
  - internal/ledger/ledger.go
  - internal/ledger/ledger_test.go
priority: low
type: chore
ordinal: 118000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
ledger.Operation uses a time.Time for RequestNotAfter with omitempty, which never omits a zero time. Public op inspect and history therefore print "requestNotAfter":"0001-01-01T00:00:00Z" for every operation, while the real deadline is only filled in for authenticated --full inspection. Machine consumers cannot tell "not disclosed" from a real value.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Public op inspect and history omit requestNotAfter entirely instead of emitting the zero time
- [x] #2 Authenticated op inspect --full still returns the recorded deadline and a test covers both projections
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Update the public ledger operation projection so an absent request deadline is omitted while preserving the authenticated full projection.
2. Add regression coverage for public inspect/history omission and authenticated --full disclosure.
3. Run focused tests and repository quality gates, review the diff, then integrate and finalize the task.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Changed ledger.Operation.requestNotAfter to an optional pointer, preserving the stored deadline only for authenticated full inspection. Added regression assertions that public inspect/history JSON omit the field and authenticated full JSON includes the exact deadline. Focused tests pass: go test ./internal/ledger ./internal/cli.

Repository gates pass: mise run lint, format-check, test, typecheck, and staged mise run hooks. Diff review found no item-scoped defects.

Post-merge verification on main passed: go test ./internal/ledger ./internal/cli. Implementation commit f188a85 was fast-forwarded to main.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Made requestNotAfter optional in public ledger operation projections, so public inspect and history omit undisclosed deadlines while authenticated op inspect --full returns the exact recorded deadline. Regression coverage verifies all three projections; focused tests, full lint/format/test/typecheck gates, and pre-commit hooks passed. Integrated as f188a85.
<!-- SECTION:FINAL_SUMMARY:END -->
