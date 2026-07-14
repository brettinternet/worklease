---
id: TASK-15
title: Upgrade GitHub Actions runtimes
status: In Progress
assignee:
  - '@brett'
created_date: '2026-07-14 02:44'
updated_date: '2026-07-14 03:57'
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
1. Add a non-publishing workflow_dispatch release validation path so the upgraded release actions can run at the current package version without rewriting the existing v0.1.0 tag. 2. Run local workflow parsing and repository quality gates. 3. Commit only the workflow and task-state changes; document that the post-change remote workflow run requires the owner to push this commit because this pass does not have push authorization.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Confirmed upstream latest releases with gh: actions/checkout v7.0.0, jdx/mise-action v4.2.0, actions/upload-artifact v7.0.1, actions/download-artifact v8.0.1, and softprops/action-gh-release v3.0.2. Updated CI and release workflows to the corresponding latest major tags.

Implementation checkpoint (quality-gates task): mise run lint passed; mise run format-check passed (23 files); mise run test passed (59 tests); mise run typecheck passed (0 errors); Ruby Psych parsed .github/workflows/ci.yml and release.yml; all workflow uses pins are immutable 40-character SHAs with version comments; successful post-change CI run 29302065979 on main (head 93cf51c68c8f22e4ae3e1b14b9311fe84ee42376) completed all jobs, and its logs contain no Node.js 20/runtime deprecation warnings (only unrelated tool/path warnings). No code changes in this pass. Next task: obtain/verify release workflow run evidence, then review TASK-15 accumulated commits 6e5b73b and b3398ee.

Implementation checkpoint (release workflow evidence): gh run view 29301289631 --json jobs confirmed all six jobs succeeded (Build Python assets, Build linux-x86_64 executable, Build macos-arm64 executable, Build linux-arm64 executable, Build macos-x86_64 executable, Publish release assets). The run was tag v0.1.0 at head 1c0f6c8, before the upgraded workflow commits, and its logs show legacy actions/checkout@v4, jdx/mise-action@v2, actions/upload-artifact@v4, actions/download-artifact@v4, and softprops/action-gh-release@v2 plus Node.js 20 deprecation warnings. Therefore this run is not post-change evidence for acceptance criteria #1/#2; no post-change release run exists yet. Next task: obtain a release-workflow run at the upgraded workflow revision, then review TASK-15 accumulated commits 6e5b73b and b3398ee.

Pass commit: 2480a1b3e66fc689682230f7d266a7cf55e1b966 (task-state evidence only; no production changes). The open implementation task remains incomplete because no post-change release run exists; next context must obtain/verify a release run at the upgraded workflow revision before review.

Implementation blocker (release evidence, 2026-07-14): release.yml is tag-only (`on.push.tags: v*`) and has no workflow_dispatch; `gh workflow run release.yml --ref main` returned HTTP 422: Workflow does not have workflow_dispatch trigger. The only Release run is 29301289631 at pre-change commit 1c0f6c8, triggered by published tag v0.1.0 (remote tag object afcf305cb1d36ba1cf9e1002e364dd4a28655759 peeling to 1c0f6c8); it succeeded but used legacy action majors and emitted Node.js 20 deprecation warnings. Upgraded workflow is on remote main 93cf51c68c8f22e4ae3e1b14b9311fe84ee42376, with actions/checkout v7, jdx/mise-action v4, upload-artifact v7, download-artifact v8, and softprops/action-gh-release v3. The v0.1.0 release is published, non-draft/non-prerelease, with existing assets; force-moving its tag would rewrite release provenance. A fresh tag must match package version 0.1.0, so a new tag requires an authorized version bump, commit push, tag push, and public release. The single oracle review confirmed no safe non-destructive trigger exists under current authorization. Unblock: owner explicitly authorizes a new version X, matching package-version change, commit push, fresh vX tag push/publication, then verify the post-change Release run at that head; alternatively owner revises or waives the post-change release-run acceptance criterion.

Unblocked release evidence by adding workflow_dispatch with required release_tag (default v0.1.0). Manual runs build and checksum-verify Python/native assets while guarding the GitHub release publication step with if: github.event_name == push; tag-triggered releases remain unchanged. Verification: mise run lint passed; mise run format-check passed (23 files); mise run test passed (62 tests); mise run typecheck passed (0 errors); Ruby Psych parsed both workflow files; every uses line remains an immutable 40-character SHA with version comment; manual input and publish guard are present. A post-change remote run is not claimed because this pass has no push authorization.

Independent verifier verdict at commit 5d046c0: AC #1 PASS; AC #2 UNVERIFIED because no post-change release run exists and the only release run 29301289631 predates the upgrade; AC #3 UNVERIFIED because no remote CI run exists at 5d046c0. Verifier found no workflow semantic defect. Remote origin/main remains 93cf51c, while commit 5d046c0 is local only; owner must push before triggering workflow_dispatch release validation and a post-change CI run.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: @brett
created: 2026-07-14 03:48
---
Unblocking the release-evidence blocker without creating or force-moving a release tag: add workflow_dispatch with an explicit release_tag input and skip publication for manual validation runs. The existing push-tag release path remains unchanged.
---
<!-- COMMENTS:END -->
