---
id: TASK-61
title: Release v0.8.2
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-07 19:40'
updated_date: '2026-09-07 19:45'
labels: []
dependencies: []
ordinal: 65000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Consolidate usable local branch work onto main, clean up obsolete local branches and worktrees, prepare the next patch release, publish it, and verify remote CI and release workflows.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 All useful local branch and worktree changes are represented on main; obsolete local branches and worktrees are removed
- [ ] #2 Local lint, format-check, test, and typecheck quality gates pass
- [ ] #3 Package metadata and changelog identify version 0.8.2
- [ ] #4 origin/main contains the consolidated release commits and its CI passes
- [ ] #5 GitHub release v0.8.2 is published with expected artifacts and passing tagged workflows
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Audit every local unmerged branch/worktree against main and retain only unique, completed work.
2. Merge the TASK-59 review fixes and release task record into main; remove obsolete local refs/worktrees.
3. Update all release metadata and changelog entries to 0.8.2.
4. Run the full local quality gates and commit the release.
5. Push main, wait for green remote CI, tag and push v0.8.2, then verify tagged CI, release publication, and assets.
6. Record objective evidence in TASK-61, finalize it, push the final task update, and clean local branches.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Branch audit: merged task-59-review-fixes into main. Every other task/chore branch is either already merged, patch-equivalent to main, or an obsolete pre-integration chain for a completed task; isolated plus patches are superseded by their completed task integrations. The untracked linked prototype predates and is superseded by completed TASK-18 and includes a stray file, so it is not usable.

Prepared 0.8.2 metadata in the root package, source SDK package/facade, uv.lock, release workflow default, and changelog. Local lint and format-check pass; 276 core and 19 SDK tests pass; both Pyright projects report 0 errors; root and SDK builds pass. Rebuilt dist/release from the four 0.8.2 Python artifacts, generated checksums.txt, and release-validate passes.
<!-- SECTION:NOTES:END -->
