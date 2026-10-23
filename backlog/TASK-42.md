---
id: TASK-42
title: Refresh locked Python tooling dependencies
status: done
type: chore
assignees: ['@brett']
rank: a4c59d31674c59cee
files: [uv.lock]
created: 2026-09-04T05:10:00Z
updated: 2026-09-04T05:11:00Z
---

Refresh the committed uv lockfile to current compatible patch releases so local and CI tooling use current fixes.

## Acceptance criteria

- [x] uv.lock resolves Ruff 0.16.6 and pyinstaller-hooks-contrib 2026.7
- [x] Project lint, formatting, tests, and type checks pass with the refreshed lockfile

## Plan

1. Refresh the uv lockfile within existing version constraints.
2. Run the full repository quality gates.
3. Record verification, finalize the task, commit, and push.

## Notes

Refreshed uv.lock with uv lock --upgrade. Verification passed: uv tree --locked; mise run lint; mise run format-check; mise run test (212 tests); mise run typecheck.

## Summary

Updated locked Ruff from 0.16.4 to 0.16.6 and pyinstaller-hooks-contrib from 2026.6 to 2026.7. All repository quality gates pass.
