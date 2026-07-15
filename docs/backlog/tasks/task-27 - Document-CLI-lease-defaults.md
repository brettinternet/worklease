---
id: TASK-27
title: Document CLI lease defaults
status: Done
assignee:
  - '@codex-loop-fresh-20260715-worklease-pass'
created_date: '2026-07-15 15:36'
updated_date: '2026-07-15 15:47'
labels:
  - cli
  - ux
dependencies: []
priority: medium
type: enhancement
ordinal: 28000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Clarify the effective defaults for acquire polling and lease TTL in command help without changing parsing or runtime behavior.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Acquire help states that --poll-interval defaults to 0.25 seconds when --wait-timeout is used and is invalid without --wait-timeout
- [x] #2 Commands exposing --ttl state that the default lease TTL is 900 seconds
- [x] #3 CLI help tests verify both default descriptions and existing behavior remains unchanged
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Centralize the documented lease TTL and polling defaults in the CLI's existing constants. 2. Add explicit help text for --poll-interval and every --ttl option without changing argument semantics. 3. Add subprocess help assertions and run targeted plus repository quality checks.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented shared _add_ttl_argument using models.DEFAULT_TTL (900 seconds) and documented every --ttl-bearing command. Acquire --poll-interval help now states the effective 0.25-second default with --wait-timeout and invalidity without it; parser semantics remain unchanged. Added subprocess help coverage for acquire polling and all TTL-bearing commands. Targeted test: mise exec -- python -m unittest discover -s tests -p test_cli.py (36 passed). Quality gates: mise run lint, mise run format-check, mise run typecheck, mise run test, and mise run hooks all passed.

The retry behavior test now omits --poll-interval and asserts the effective 0.25-second sleep, while existing explicit-interval tests remain intact.

Independent verifier PASS: acquire help and all 11 TTL command help subprocesses show the required defaults; runtime/parser regression checks passed, standalone --poll-interval remains invalid without --wait-timeout, omitted TTL acquires 900.0 seconds.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Documented effective CLI lease defaults without changing behavior. --poll-interval now explains its 0.25-second default with --wait-timeout and invalidity otherwise; all 11 TTL-bearing command helps state the shared 900-second default. Centralized TTL parsing on models.DEFAULT_TTL and added help/default behavior coverage. Verified with the focused CLI suite (36 passed), independent verifier PASS, mise run lint, mise run format-check, mise run typecheck, mise run test, and mise run hooks.
<!-- SECTION:FINAL_SUMMARY:END -->
