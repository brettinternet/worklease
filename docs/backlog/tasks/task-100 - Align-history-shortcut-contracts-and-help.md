---
id: TASK-100
title: Align history shortcut contracts and help
status: Done
assignee:
  - '@brett'
created_date: '2026-09-13 03:36'
updated_date: '2026-09-13 03:46'
labels: []
dependencies: []
references:
  - e3408d5
  - 1ebeaf1
  - 50a3a1f
documentation:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - docs/cli-reference.md
modified_files:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - docs/cli-reference.md
  - internal/cli/commands.go
  - internal/cli/ledger_commands_test.go
  - internal/cli/root_test.go
priority: low
type: docs
ordinal: 125000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Commit e3408d5 made `worklease history` without a resource an alias for the bounded global event feed while preserving `history --resource RESOURCE` as the retained epoch projection. The normative Go product contract still describes a required resource, command help shows only the no-argument form, and the JSON alias behavior should be explicit so future changes do not accidentally fork the event contract.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The normative Go product contract documents both no-argument global events and resource-scoped epoch history, including bounded limit/cursor behavior.
- [x] #2 `worklease history --help` presents both supported forms and clearly distinguishes their projections.
- [x] #3 User documentation states that no-argument `history --json` returns the canonical events envelope with `operation` equal to `events`.
- [x] #4 Automated tests protect the documented help and JSON alias behavior.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Update the normative product contract and CLI reference to define global history alias and resource epoch pagination.
2. Expand history command help to show and distinguish both forms.
3. Add focused tests for help text and canonical JSON alias parity.
4. Run focused and repository quality gates, review the diff, commit, merge to main, finalize the task, and clean up the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Updated the product contract and CLI reference, expanded history help with both forms, and added focused help and canonical JSON alias assertions. Focused internal/cli tests pass.

Validation passed after merge on main: mise run lint; mise run format-check; mise run test; mise run typecheck. Manual help output also showed both forms and distinguished the global event feed from resource epochs. Diff review found no remaining task-scoped defects.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Aligned the history shortcut across the normative contract, CLI reference, and command help. No-resource history remains the canonical events projection (including operation "events" in JSON), while resource history remains a bounded epoch projection. Added rendered-help and JSON alias regression tests; all repository quality gates passed after merge.
<!-- SECTION:FINAL_SUMMARY:END -->
