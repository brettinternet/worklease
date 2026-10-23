---
id: TASK-58
title: Release v0.8.1
status: done
type: chore
assignees: ['@brett']
rank: a691840ac769183ae
created: 2026-09-07T14:24:00Z
updated: 2026-09-07T14:48:00Z
---

Validate the release workflow against the post-v0.8.0 fixes, prepare version 0.8.1, and publish the tagged release if CI and release validation pass.

## Acceptance criteria

- [x] Current main passes the full CI workflow
- [x] The release workflow validates all Python and native assets on every supported runner before publication
- [x] Package metadata and changelog identify version 0.8.1
- [x] GitHub release v0.8.1 is published with complete checksums and expected artifacts

## Plan

1. Push the completed post-v0.8.0 fixes and validate both CI and a non-publishing release workflow dispatch.
2. Update package versions, lock metadata, release workflow default, and changelog for 0.8.1.
3. Run all local quality gates, commit and push the release preparation, then verify main CI.
4. Tag v0.8.1, push it, and verify both tagged CI and release publication including release assets.

## Notes

Pushed post-v0.8.0 fixes at c376d82. CI run 34132750640 passed all six jobs, including macOS Intel. Non-publishing Release run 34132751025 passed Python assets, all four native platforms, artifact merge, and checksum verification.

Prepared 0.8.1 metadata in the root package, source SDK package/facade, uv.lock, release workflow dispatch default, and changelog. Local mise run lint, format-check, test (253 core + 19 SDK), and typecheck (0 errors) pass.

Release preparation main CI run 34133883463 passed, and the final current-main CI run 34134135231 passed after the contention demo commit. Tagged CI run 34134507424 passed all six jobs.

Tagged Release run 34134507426 passed and published v0.8.1. Downloaded all nine published assets with gh and independently verified checksums using scripts/release_artifacts.py; release is public at https://github.com/brettinternet/worklease/releases/tag/v0.8.1.

## Summary

Published Worklease v0.8.1 from 2a9b799 after green local gates, two green main CI runs, a successful non-publishing release validation, and green tagged CI/Release workflows. Verified all nine published Python, SDK, native, and checksum assets after download.
