---
id: TASK-39
title: Refresh pinned development and CI dependencies
status: Done
assignee:
  - '@brett'
created_date: '2026-08-12 04:57'
updated_date: '2026-08-12 05:11'
labels: []
dependencies: []
modified_files:
  - pyproject.toml
  - uv.lock
  - packages/worklease-source-sdk/pyproject.toml
  - packages/worklease-source-sdk/examples/source-provider-plugin/pyproject.toml
  - tests/fixtures/resource-policy-plugin/pyproject.toml
  - tests/test_release.py
  - .github/workflows/ci.yml
  - .github/workflows/release.yml
type: chore
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Audit and refresh the repository's pinned Python build, development, release, and GitHub Actions dependencies so maintenance and CI use current stable releases without loosening reproducibility.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Direct Python dependency constraints and uv.lock resolve to current compatible stable releases
- [x] #2 GitHub Actions references use current stable releases pinned by commit SHA with matching version comments
- [x] #3 The full repository lint, format-check, test, and typecheck gates pass
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Audit and update Python build, development, and release dependency pins plus uv.lock.
2. Audit GitHub Actions references and update outdated stable versions while retaining SHA pinning and matching comments.
3. Run all repository quality gates and independently verify every acceptance criterion.
4. Record the verified outcome, commit the focused maintenance change, and push main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Refreshed repository-wide Python pins: Hatchling 1.32.0 in all four pyproject build systems, PyInstaller 6.22.0, jsonschema >=4.26.0, Ruff >=0.16.2, and regenerated uv.lock. Refreshed checkout to v7.0.1 and mise-action to v4.2.4 using verified immutable SHAs; other actions were already current. The first full test run exposed stale release-pin assertions; corrected them and reran the complete gate set. Independent verifier passed all criteria and reviewer reported no findings.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Updated all pinned Python build/dev/release dependencies and the lockfile to current stable releases, including the test fixture; updated GitHub Actions checkout and mise-action SHA pins and version comments. Verified with `mise run lint`, `mise run format-check`, `mise run test` (193 core + 19 SDK tests), and `mise run typecheck` (0 errors), then independent criterion verification and maintenance review.
<!-- SECTION:FINAL_SUMMARY:END -->
