---
id: TASK-98
title: Release Go rewrite as v1.0.0
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-13 00:56'
updated_date: '2026-09-13 01:02'
labels: []
dependencies: []
references:
  - .github/workflows/ci.yml
  - .github/workflows/release.yml
modified_files:
  - .github/workflows/release.yml
  - README.md
  - internal/guard/guard.go
  - internal/store/driver.go
priority: high
type: chore
ordinal: 123000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The Go rewrite is complete locally, but the remote CI matrix is failing and the first stable Go release has not been published. The release must preserve the existing mise GitHub backend contract so users can continue selecting latest without changing their tool declaration.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 CI passes on all configured Linux and macOS runners for the release commit
- [ ] #2 Release validation builds and smoke-tests mise-compatible Go archives for linux and macOS on x64 and arm64
- [ ] #3 GitHub release v1.0.0 publishes checksummed Go archives from the release commit
- [ ] #4 A clean mise installation using github:brettinternet/worklease = "latest" installs worklease v1.0.0
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reproduce and correct the cross-platform CI failures without weakening coverage.
2. Add an end-to-end mise installation check to release validation while preserving the established archive naming/layout contract.
3. Run all repository quality gates and release validation locally.
4. Push main, wait for green CI, tag v1.0.0, then verify the release workflow, assets, checksums, version metadata, and a clean latest mise install.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Confirmed the existing release archive names and bin/worklease layout install successfully through mise GitHub backend at v0.10.0. Fixed guarded exec output capture so readers drain owned pipes after the leader exits, and normalized deadline-bounded SQLite contention. Added post-publication latest-mise checks on all four release platforms and documented the unchanged mise declaration.
<!-- SECTION:NOTES:END -->
