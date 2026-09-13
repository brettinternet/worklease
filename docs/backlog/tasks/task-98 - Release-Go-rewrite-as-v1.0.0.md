---
id: TASK-98
title: Release Go rewrite as v1.0.0
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-13 00:56'
updated_date: '2026-09-13 01:23'
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
- [x] #1 CI passes on all configured Linux and macOS runners for the release commit
- [x] #2 Release validation builds and smoke-tests mise-compatible Go archives for linux and macOS on x64 and arm64
- [x] #3 GitHub release v1.0.0 publishes checksummed Go archives from the release commit
- [x] #4 A clean mise installation using github:brettinternet/worklease = "latest" installs worklease v1.0.0
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

The first post-publication check exposed mise's intentional minimum-release-age filter: a clean latest lookup selected v0.9.1 immediately after publication. The release artifact itself installed successfully when that delay was disabled. Updated release verification to set MISE_MINIMUM_RELEASE_AGE=0 and to run on workflow_dispatch as well as tagged publication, allowing immediate post-release validation without changing users' simple latest declaration.

Verification: local mise run ci passed all format, staticcheck, vet, unit, race, vulnerability, end-to-end, and manual-generation gates. Main CI runs 34729583689 (release commit ca97d51) and 34729975577 (pipeline follow-up d81e4bd) passed all four OS/architecture jobs. Release v1.0.0 publishes four archives plus checksums from ca97d51. Release validation run 34729978282 passed archive build/smoke/manual checks and clean latest mise installation on linux-x64, linux-arm64, macos-x64, and macos-arm64; its first attempt hit a transient macOS artifact-upload DNS failure and the failed attempt passed on rerun. GitHub latest resolves to v1.0.0.

Final bookkeeping CI exposed one remaining macOS-only test race: the signal helper could call os.Exit(0) before its self-sent SIGTERM was delivered. Reopened to make the helper wait for signal delivery and restore a green final main commit.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Released the Go rewrite as v1.0.0 with checksummed cross-platform archives, fixed the CI flakes that blocked the release, preserved and documented the simple mise latest declaration, and added four-platform post-publication mise installation checks. Local gates, both main CI commits, release archive validation, and clean latest mise installs all pass.
<!-- SECTION:FINAL_SUMMARY:END -->
