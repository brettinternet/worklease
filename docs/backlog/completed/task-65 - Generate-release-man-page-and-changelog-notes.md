---
id: TASK-65
title: Generate release man page and changelog notes
status: Done
assignee:
  - '@brett'
created_date: '2026-09-11 20:54'
updated_date: '2026-09-11 21:00'
labels: []
dependencies: []
references:
  - CHANGELOG.md
  - .github/workflows/release.yml
  - src/worklease/cli.py
modified_files:
  - .github/workflows/release.yml
  - CHANGELOG.md
  - README.md
  - scripts/release_artifacts.py
  - scripts/release_docs.py
  - tests/test_release.py
priority: medium
type: enhancement
ordinal: 69000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the CLI manual and project changelog first-class release outputs so installed/downloaded documentation matches each tagged Worklease version and GitHub release notes come from the maintained changelog instead of unrelated generated commit notes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A generated `worklease(1)` manual documents the complete current CLI and embeds the tagged release version and date.
- [x] #2 Release automation validates and extracts the matching version section from `CHANGELOG.md`, failing clearly when the tag has no release entry.
- [x] #3 Every tagged release publishes the man page and release changelog alongside existing assets and uses the changelog section for GitHub release notes.
- [x] #4 Automated tests cover generation, validation, packaging, and release-workflow integration.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a dependency-free release documentation generator that derives a roff `worklease(1)` page from argparse help and extracts one exact tagged section from `CHANGELOG.md`. 2. Generate versioned documentation during the release workflow, upload it as a release artifact, and use the extracted notes as the GitHub release body. 3. Extend release tests and documentation, then run focused and repository quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented dependency-free release documentation generation from complete argparse help and exact versioned CHANGELOG sections. Release jobs now validate/generate documentation, include the manual in native archives, publish standalone documentation assets, and use changelog-derived GitHub release notes. Added release integration and packaging tests.

Verification: release documentation unit/integration tests pass; full `mise run lint`, `mise run format-check`, `mise run test` (302 core and 19 SDK tests), and `mise run typecheck` pass. Generated v0.9.0 assets from the repository changelog and `mandoc -T lint` accepted the generated manual. Staged-file `mise run hooks` passed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added version-matched release documentation generation. Tagged releases now fail without an exact changelog entry, publish `worklease.1` and the release changelog, package the manual in native archives, and use the changelog as the GitHub release body. Verified generation with mandoc, release tests, all project quality gates, and pre-commit hooks.
<!-- SECTION:FINAL_SUMMARY:END -->
