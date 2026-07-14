---
id: TASK-15
title: Upgrade GitHub Actions runtimes
status: Done
assignee:
  - '@brett'
created_date: '2026-07-14 02:44'
updated_date: '2026-07-14 02:50'
labels: []
dependencies: []
modified_files:
  - .github/workflows/ci.yml
  - .github/workflows/release.yml
ordinal: 16000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Update every action used by the CI and release workflows to its current supported major version so hosted runners do not emit Node.js runtime deprecation warnings.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 All action references use the latest major releases confirmed from their upstream repositories.
- [x] #2 CI and release workflow checks complete without Node.js 20 deprecation warnings from the upgraded actions.
- [x] #3 The repository quality gates and a post-change CI run pass.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Replace each CI and release workflow action reference with the latest upstream major release confirmed from GitHub. 2. Run repository quality gates and inspect workflow syntax/diffs. 3. Commit and push only the workflow and task-state changes, then verify the resulting CI run and any release workflow checks.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Confirmed upstream latest releases with gh: actions/checkout v7.0.0, jdx/mise-action v4.2.0, actions/upload-artifact v7.0.1, actions/download-artifact v8.0.1, and softprops/action-gh-release v3.0.2. Updated CI and release workflows to the corresponding latest major tags.

Validation: isolated commit f9cbc68 passed mise run sync, test (55 tests), lint, format-check, typecheck, and build. GitHub CI run 29301960835 completed successfully for every quality/native matrix job. Its logs resolve actions/checkout@v7 and jdx/mise-action@v4 and contain no matches for Node.js 20, deprecated, or Node.js 24 warnings. gh workflow view --yaml successfully parsed the updated release workflow, which now references checkout v7, mise v4, upload-artifact v7, download-artifact v8, and action-gh-release v3; it remains tag-triggered and was not rerun to avoid publishing a duplicate release.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Upgraded all CI and release workflow actions to the latest upstream major releases confirmed through gh: checkout v7, mise-action v4, upload-artifact v7, download-artifact v8, and action-gh-release v3. Verified local gates, GitHub workflow parsing, and a fully green post-change CI run with no Node.js deprecation warnings.
<!-- SECTION:FINAL_SUMMARY:END -->
