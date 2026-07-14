---
id: TASK-8
title: Fix CI smoke test and publish v0.1.0 release
status: In Progress
assignee:
  - '@brett'
created_date: '2026-07-14 02:17'
updated_date: '2026-07-14 02:19'
labels: []
dependencies: []
modified_files:
  - .github/workflows/ci.yml
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the repository CI green on every configured runner, then publish the existing 0.1.0 package as a verified tagged GitHub release with all documented assets.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The CI quality smoke test invokes the built wheel and sdist on every configured runner.
- [ ] #2 All required CI jobs pass for the release commit.
- [ ] #3 A v0.1.0 Git tag and GitHub release contain the documented Python, native, and checksum assets.
- [ ] #4 The published assets pass checksum and version smoke validation.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Correct the CI wheel and sdist smoke-test invocation for runners whose user tool bin directory is not on PATH. 2. Run the repository quality gates and build checks locally. 3. Commit and push the fix, then wait for every required CI job to pass on the release commit. 4. Create the v0.1.0 tag to trigger the documented release workflow, then verify the GitHub release assets, checksums, and version smoke tests.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Diagnosed both failing CI runs (29299234847 and 29299457356): macos-15-intel could not find the uv-installed worklease executable because $HOME/.local/bin was not on PATH. Updated .github/workflows/ci.yml to resolve uv tool dir --bin and invoke that exact executable. Local gates passed: mise run sync, test (55 tests), typecheck, lint, format-check, build. Exact wheel and sdist smoke block passed with version 0.1.0.
<!-- SECTION:NOTES:END -->
