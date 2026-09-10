---
id: TASK-16
title: Pin release toolchain and GitHub Actions
status: Done
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:46'
updated_date: '2026-07-14 02:55'
labels: []
dependencies: []
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make tagged release and CI execution reproducible and reduce workflow supply-chain exposure. Pin release-critical Python build tools in the committed uv lock, pin every third-party GitHub Action to an immutable commit SHA with a readable version comment, and limit release write permission to the publishing job.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Hatchling and PyInstaller resolve from committed exact pins in uv.lock, and CI/release native builds do not use an unpinned --with dependency.
- [x] #2 Every third-party action in CI and release workflows is pinned to a full immutable commit SHA with its intended release documented inline.
- [x] #3 Workflow-level permissions are read-only and only the release publishing job receives contents: write.
- [x] #4 Locked sync, tests, lint, formatting, typecheck, build, workflow validation, and frozen executable smoke checks pass.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Resolve authoritative action tag SHAs through gh and exact compatible build-tool versions through uv. 2. Pin build dependencies in pyproject.toml/uv.lock and consume the locked PyInstaller executable in CI/release. 3. Pin actions and narrow permissions. 4. Add/adjust release validation tests, run all quality gates, independently verify, and commit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Resolved exact Python 3.14-compatible release tools: Hatchling 1.31.0 and PyInstaller 6.21.0. Added a locked release dependency group, made it part of locked sync, changed package builds to no-build-isolation, and removed dynamic PyInstaller --with resolution. Pinned checkout v7.0.0, mise-action v4.2.0, upload-artifact v7.0.1, download-artifact v8.0.1, and action-gh-release v3.0.2 to gh-resolved 40-character commit SHAs. Root workflow permissions are read-only; only publish has contents:write. Added static policy regressions. Current verification: 59 tests, lint, format-check, typecheck, package build, workflow YAML parse, and actual PyInstaller 6.21.0 version/key smoke passed.

Independent verifier PASS with no findings: exact action SHA/tag correspondence, locked build-tool consumption, permissions scope, full gates, YAML parse, and frozen executable behavior all verified.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Pinned Hatchling 1.31.0 and PyInstaller 6.21.0 in the committed uv release group; package/native workflows consume the locked tools. Pinned all CI/release actions to verified immutable SHAs and restricted contents:write to the publish job. Added policy regressions. Verified with locked sync, 59 tests, lint, format-check, typecheck, no-isolation build, YAML parse, actual frozen version/key smoke, staged hooks, and independent review.
<!-- SECTION:FINAL_SUMMARY:END -->
