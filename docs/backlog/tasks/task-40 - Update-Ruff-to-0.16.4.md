---
id: TASK-40
title: Update Ruff to 0.16.4
status: Done
assignee:
  - '@brett'
created_date: '2026-08-23 17:11'
updated_date: '2026-08-23 17:14'
labels: []
dependencies: []
modified_files:
  - pyproject.toml
  - uv.lock
type: chore
ordinal: 41000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Refresh the repository's Ruff development constraint and uv lockfile to the current compatible stable release so local and CI linting use the maintained toolchain.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The Ruff development constraint requires version 0.16.4 or newer
- [x] #2 uv.lock resolves Ruff 0.16.4 consistently for supported platforms
- [x] #3 Repository lint, format-check, test, and typecheck gates pass
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Raise the Ruff development constraint to 0.16.4 and regenerate uv.lock with uv.
2. Run lint, format-check, test, and typecheck quality gates.
3. Independently verify each acceptance criterion, record evidence, finalize the task, commit the focused change, and push main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Raised the Ruff development constraint from >=0.16.2 to >=0.16.4 and regenerated uv.lock with `uv lock --upgrade-package ruff`. Validation passed: `mise run lint`, `mise run format-check` (109 files), `mise run test` (193 core and 19 SDK tests), and `mise run typecheck` (0 errors in both projects). Independent verification confirmed Ruff 0.16.4, a consistent lockfile via `uv lock --check`, and all three acceptance criteria.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Updated the repository Ruff constraint and lockfile from 0.16.2 to 0.16.4. Verified all required quality gates and independent acceptance checks.
<!-- SECTION:FINAL_SUMMARY:END -->
