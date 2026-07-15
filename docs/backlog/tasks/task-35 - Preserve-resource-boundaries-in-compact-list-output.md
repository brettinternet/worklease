---
id: TASK-35
title: Preserve resource boundaries in compact list output
status: Done
assignee:
  - '@codex-loop-fresh-20260715-worklease-main'
created_date: '2026-07-15 23:51'
updated_date: '2026-07-15 23:57'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
  - README.md
priority: medium
type: enhancement
ordinal: 36000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Improve the default human-readable list resource rendering when generic shortening hides useful path or item boundaries. Keep resources provider-agnostic and opaque to the formatter while making recognizable separator/path components survive within the existing compact column budget.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Default text list output remains bounded and deterministic while preserving recognizable resource boundary components when shortening long values.
- [x] #2 The formatter does not special-case backlog-md or assume a provider-specific resource grammar; arbitrary opaque resource values remain supported.
- [x] #3 worklease list --full and explicit JSON output remain complete and unchanged.
- [x] #4 CLI contract tests cover the exact backlog-md resource example and compact/full/JSON behavior, and documentation matches the final rendering contract.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Replace midpoint-only resource shortening with a provider-agnostic bounded renderer that preserves recognizable separator and path boundary components, with a deterministic fallback for arbitrary opaque values. 2. Add CLI contract coverage for the exact backlog-md resource example, opaque resources, and unchanged full/JSON output. 3. Update the human-readable grammar to describe the compact resource rendering without promising provider-specific parsing.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented provider-agnostic resource shortening that cuts at separator boundaries while preserving the leading component and useful suffix. Updated the exact backlog-md/worklease example regression, added opaque-value width coverage, and documented the rendering contract. Focused CLI formatter tests pass.

Validation evidence: focused formatter tests passed (3 tests), including the exact backlog-md:/Users/brett/dev/me/worklease/.git:docs/backlog:TASK-18 example, boundary-preserving output, opaque values, and width bounds. Direct CLI smoke produced backlog-md:…/me/worklease/.git:docs/backlog:TASK-18 in compact output and confirmed --full contains the complete resource. mise run lint, mise run format-check, mise run test, mise run typecheck, and mise run hooks all passed.

Post-finalization verification: mise run lint passed, mise run format-check passed (45 files already formatted), and mise run typecheck passed for core and SDK (0 errors, 0 warnings, 0 informations).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Changed default list RESOURCE shortening from midpoint truncation to provider-agnostic separator-boundary shortening. The compact example now preserves `worklease`, `docs/backlog`, and `TASK-18` within the 52-character column; opaque values use bounded deterministic fallback shortening. Full text and JSON remain complete. Added CLI coverage and updated README grammar. Verified with direct compact/full CLI smoke plus mise run lint, format-check, test, typecheck, and hooks.
<!-- SECTION:FINAL_SUMMARY:END -->
