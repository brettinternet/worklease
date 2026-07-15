---
id: TASK-31
title: Version agent help links for releases
status: In Progress
assignee:
  - '@brett'
created_date: '2026-07-15 22:16'
updated_date: '2026-07-15 22:20'
labels: []
dependencies: []
ordinal: 32000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Published Worklease CLI artifacts should direct agent workflow help to the matching release documentation, while source and development builds continue to use the main branch documentation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Published Python and native release artifacts embed their validated release version for agent workflow and README help links.
- [ ] #2 Source and development builds continue to use main unless explicit valid published release metadata is supplied.
- [ ] #3 Tests cover source, published-version, and invalid-metadata URL selection without changing existing help behavior.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add build-time release documentation metadata with an unset source default. 2. Have CLI help prefer embedded validated release metadata while preserving explicit environment override and main fallback. 3. Generate metadata in both Python and native release jobs, add focused packaging/help regressions, run repository quality gates, then finalize and push.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented build-time _release_metadata.py default, validated scripts/embed_release_metadata.py generation, CLI fallback/override behavior, and release workflow generation in both Python/native jobs. Focused CLI/release tests passed 52/52; embedded release-help smoke passed.
<!-- SECTION:NOTES:END -->
