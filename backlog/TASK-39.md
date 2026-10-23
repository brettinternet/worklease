---
id: TASK-39
title: Refresh pinned development and CI dependencies
status: done
type: chore
assignees: ['@brett']
rank: a46f61e8a546f61aa
files: [pyproject.toml, uv.lock, packages/worklease-source-sdk/pyproject.toml, packages/worklease-source-sdk/examples/source-provider-plugin/pyproject.toml, tests/fixtures/resource-policy-plugin/pyproject.toml, tests/test_release.py, .github/workflows/ci.yml, .github/workflows/release.yml]
created: 2026-08-12T04:57:00Z
updated: 2026-08-12T05:11:00Z
---

Audit and refresh the repository's pinned Python build, development, release, and GitHub Actions dependencies so maintenance and CI use current stable releases without loosening reproducibility.

## Acceptance criteria

- [x] Direct Python dependency constraints and uv.lock resolve to current compatible stable releases
- [x] GitHub Actions references use current stable releases pinned by commit SHA with matching version comments
- [x] The full repository lint, format-check, test, and typecheck gates pass

## Plan

1. Audit and update Python build, development, and release dependency pins plus uv.lock.
2. Audit GitHub Actions references and update outdated stable versions while retaining SHA pinning and matching comments.
3. Run all repository quality gates and independently verify every acceptance criterion.
4. Record the verified outcome, commit the focused maintenance change, and push main.

## Notes

Refreshed repository-wide Python pins: Hatchling 1.32.0 in all four pyproject build systems, PyInstaller 6.22.0, jsonschema >=4.26.0, Ruff >=0.16.2, and regenerated uv.lock. Refreshed checkout to v7.0.1 and mise-action to v4.2.4 using verified immutable SHAs; other actions were already current. The first full test run exposed stale release-pin assertions; corrected them and reran the complete gate set. Independent verifier passed all criteria and reviewer reported no findings.

## Summary

Updated all pinned Python build/dev/release dependencies and the lockfile to current stable releases, including the test fixture; updated GitHub Actions checkout and mise-action SHA pins and version comments. Verified with `mise run lint`, `mise run format-check`, `mise run test` (193 core + 19 SDK tests), and `mise run typecheck` (0 errors), then independent criterion verification and maintenance review.
