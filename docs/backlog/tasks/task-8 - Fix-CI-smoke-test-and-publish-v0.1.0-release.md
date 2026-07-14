---
id: TASK-8
title: Fix CI smoke test and publish v0.1.0 release
status: Done
assignee:
  - '@brett'
created_date: '2026-07-14 02:17'
updated_date: '2026-07-14 02:34'
labels: []
dependencies: []
modified_files:
  - .github/workflows/ci.yml
  - tests/test_execution.py
ordinal: 8000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the repository CI green on every configured runner, then publish the existing 0.1.0 package as a verified tagged GitHub release with all documented assets.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The CI quality smoke test invokes the built wheel and sdist on every configured runner.
- [x] #2 All required CI jobs pass for the release commit.
- [x] #3 A v0.1.0 Git tag and GitHub release contain the documented Python, native, and checksum assets.
- [x] #4 The published assets pass checksum and version smoke validation.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Correct the CI wheel and sdist smoke-test invocation for runners whose user tool bin directory is not on PATH. 2. Run the repository quality gates and build checks locally. 3. Commit and push the fix, then wait for every required CI job to pass on the release commit. 4. Create the v0.1.0 tag to trigger the documented release workflow, then verify the GitHub release assets, checksums, and version smoke tests.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Diagnosed both failing CI runs (29299234847 and 29299457356): macos-15-intel could not find the uv-installed worklease executable because $HOME/.local/bin was not on PATH. Updated .github/workflows/ci.yml to resolve uv tool dir --bin and invoke that exact executable. Local gates passed: mise run sync, test (55 tests), typecheck, lint, format-check, build. Exact wheel and sdist smoke block passed with version 0.1.0.

Fresh CI run 29300826838 passed the package smoke fix on every platform but exposed two macos-15-intel ownership-test startup races. Gated injected LeaseError on each started marker before cleanup; repeated both focused ownership tests 10/10 passed. Local full suite then exposed claim-expired in the long-child heartbeat test under load; widened only that test to ttl=1.0 and a 1.2-second child, preserving heartbeat/revision assertions. Repeated heartbeat test 5/5 passed and full suite 55/55 passed; lint, format-check, typecheck, and build all passed.

Release verification: CI run 29301222282 is successful for head 1c0f6c8c11217c70be016152336a08405411d19a; the release workflow 29301289631 is successful for tag v0.1.0. gh release view confirms the seven documented assets (wheel, sdist, four native binaries, checksums.txt). Downloaded assets passed scripts/release_artifacts.py verify; the downloaded macos-arm64 binary, wheel, and sdist each reported version 0.1.0. Linux and macOS-x86 binaries were not executed on this macOS arm64 host, but each ran its native release-workflow smoke test.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Fixed CI package smoke execution by invoking the exact uv tool binary directory, synchronized ownership-loss tests with child startup, and widened the heartbeat timing fixture for slow runners. Pushed 1c0f6c8, verified all CI matrix jobs green, tagged v0.1.0, and verified the successful release workflow plus checksums/version smoke tests.
<!-- SECTION:FINAL_SUMMARY:END -->
