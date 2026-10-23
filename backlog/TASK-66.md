---
id: TASK-66
title: Refine the manual and release v0.9.1
status: done
type: enhancement
priority: medium
assignees: ['@brett']
rank: a7dc11f7047dc1188
refs: [scripts/release_docs.py, .github/workflows/release.yml, CHANGELOG.md]
files: [.github/workflows/release.yml, CHANGELOG.md, packages/worklease-source-sdk/pyproject.toml, packages/worklease-source-sdk/src/worklease_source_sdk/__init__.py, pyproject.toml, scripts/release_docs.py, tests/test_release.py, uv.lock]
created: 2026-09-11T21:05:00Z
updated: 2026-09-11T21:32:00Z
---

Replace the exhaustive help dump in the generated manual with a concise task-oriented reference, add examples that progress from a basic claim through guarded and bundle workflows, and publish the result as v0.9.1.

## Acceptance criteria

- [x] The generated `worklease(1)` manual is concise, plain, direct, and covers the core lifecycle without duplicating every command help page.
- [x] Examples progress from a simple acquire/status/release flow to lease-file guarded execution and bundle coordination, and every example uses valid current CLI syntax.
- [x] The v0.9.1 changelog and package version are ready, all required checks pass, and GitHub release v0.9.1 is published from the matching tag.

## Plan

1. Replace the complete help dump with a curated roff manual organized around purpose, core commands, safe use, and simple-to-advanced examples while deriving command inventory from the parser. 2. Add tests that render and exercise the documented examples, then update the changelog and package version for v0.9.1. 3. Run all quality checks, commit and push, tag v0.9.1, and verify the release workflow and published assets.

## Notes

Replaced the exhaustive help dump with a task-oriented manual. The generator checks its command inventory against argparse, and tests execute all four examples from basic lease through guarded and bundle workflows. Independent review found unsafe repo-relative advanced handles, a Python dependency, and incomplete JSON guidance; all were corrected with private temporary handles, failure-release traps, /bin/echo, and both JSON selectors. Full lint, formatting, 302 core tests, 19 SDK tests, type checks, release generation, and mandoc lint pass.

Published v0.9.1 from the annotated tag. GitHub Actions release run 34649574899 passed all Python, Linux, macOS, checksum, and publish jobs. The published release is non-draft and non-prerelease with 11 assets; downloaded worklease.1 passes mandoc lint and the published changelog begins with the exact v0.9.1 section.

## Summary

Published v0.9.1 with a concise task-oriented manual, executable examples from simple leases through guarded bundles, safer failure cleanup, matching package and SDK versions, and changelog-derived release notes. Verified all local quality checks, independent review fixes, the successful release workflow, and published assets.
