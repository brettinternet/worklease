---
id: TASK-15
title: Upgrade GitHub Actions runtimes
status: In Progress
assignee:
  - '@brett'
created_date: '2026-07-14 02:44'
updated_date: '2026-07-14 02:45'
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
- [ ] #1 All action references use the latest major releases confirmed from their upstream repositories.
- [ ] #2 CI and release workflow checks complete without Node.js 20 deprecation warnings from the upgraded actions.
- [ ] #3 The repository quality gates and a post-change CI run pass.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Replace each CI and release workflow action reference with the latest upstream major release confirmed from GitHub. 2. Run repository quality gates and inspect workflow syntax/diffs. 3. Commit and push only the workflow and task-state changes, then verify the resulting CI run and any release workflow checks.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Confirmed upstream latest releases with gh: actions/checkout v7.0.0, jdx/mise-action v4.2.0, actions/upload-artifact v7.0.1, actions/download-artifact v8.0.1, and softprops/action-gh-release v3.0.2. Updated CI and release workflows to the corresponding latest major tags.
<!-- SECTION:NOTES:END -->
