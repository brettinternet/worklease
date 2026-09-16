---
id: TASK-111
title: Unify no-claim errors and checkpoint input guidance
status: Done
assignee:
  - '@brett'
created_date: '2026-09-16 04:35'
updated_date: '2026-09-16 04:43'
labels:
  - ergonomics
dependencies: []
priority: medium
type: bug
ordinal: 153000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Bare contextual commands fail inconsistently when no claim exists: `status` and `verify` report 'selected handle is unavailable' while `heartbeat`, `release`, and `checkpoint` report 'handle is missing'. Neither tells a first-time user that the fix is `worklease acquire --path FILE`. Separately, bare `checkpoint` on a live claim reports 'checkpoint must be canonical JSON no larger than 8 KiB' when the real problem is that neither `--data` nor `--data-file` was supplied. Both violate the CLI principle that a command missing a required input names that input and shows an example instead of leaking an internal validation message.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 With no contextual claim, `status`, `verify`, `heartbeat`, `checkpoint`, and `release` return the same reason code and one-line message that names the missing claim and shows `worklease acquire --path FILE` as the next command; `--json` envelopes carry the same reason.
- [x] #2 `checkpoint` without `--data` or `--data-file` fails with an invalid-argument message naming both options and an example; the JSON size/canonical message is reserved for supplied but invalid payloads.
- [x] #3 Tests cover each command's bare no-claim path and the bare `checkpoint` path; `docs/cli-reference.md` error table reflects the unified message.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Locate contextual claim resolution and checkpoint argument validation paths.
2. Unify missing-claim reason/message and add explicit missing checkpoint payload validation.
3. Add command-level regression tests and update the CLI error table.
4. Run focused tests and all repository quality gates, review the diff, then finalize the task.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Unified missing contextual handle failures under claim-selection-missing with acquire guidance, added explicit checkpoint payload-option validation, regression coverage for text/JSON command paths, and documented both errors. Focused internal/cli tests pass.

Validation passed: go test ./internal/cli; mise run lint; mise run format-check; mise run test; mise run typecheck. Diff review found no remaining item-scoped defects.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Unified bare contextual command failures on claim-selection-missing with an acquire example, added a dedicated missing checkpoint payload error while preserving invalid-payload validation, and documented the error contracts. Verified every affected command in text and JSON modes plus the full repository quality suite.
<!-- SECTION:FINAL_SUMMARY:END -->
