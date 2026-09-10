---
id: TASK-28
title: Audit CLI flag defaults
status: Done
assignee:
  - '@codex-loop-fresh-20260715-worklease-pass'
created_date: '2026-07-15 15:58'
updated_date: '2026-07-15 16:11'
labels:
  - cli
  - ux
dependencies: []
priority: medium
type: enhancement
ordinal: 29000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Audit every optional worklease CLI flag for a meaningful runtime default that should be discoverable in command help, then document and test the missing defaults without changing command semantics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every optional flag with a meaningful effective default has that default stated in its command help.
- [x] #2 Help output and parser/runtime tests cover each newly documented default, including conditional defaults.
- [x] #3 Existing command parsing and runtime behavior remain unchanged.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Audit every argparse flag against its effective runtime default and identify missing help documentation. 2. Align parser defaults with documented effective values where safe, preserving conditional semantics. 3. Add focused subprocess and runtime regression coverage. 4. Run repository quality gates, verify acceptance criteria, and commit the implementation.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Audited all argparse options. Documented non-obvious effective defaults for state home, wait timeout, execution directory, list filtering, GC cutoff/dry-run, and GC retention. Made GC's 30-day parser default explicit while preserving --cutoff and explicit-retention conflict semantics with an explicit-value marker. Added help coverage plus GC runtime/conflict tests; focused CLI and GC suite passes (58 tests).

Final verification: independent verifier PASS for all 3 criteria. It enumerated every parser option and found no missed meaningful default; targeted CLI/GC suite passed 58 tests; manual omitted gc, cutoff-only gc, explicit retention/cutoff conflict, held acquire no-wait, and exec/release help scenarios all matched the contract. Final quality gates passed: mise run format-check, lint, typecheck, test, and hooks.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Audited CLI optional flags and documented meaningful defaults in shared help: state-home fallback, acquire no-wait behavior, execution directory, list filtering, and GC retention/cutoff/dry-run semantics. GC now exposes the 30-day parser default while preserving cutoff-only and explicit conflict behavior. Added subprocess/runtime coverage. Independent verifier PASS for all acceptance criteria. Verified with 58 focused CLI/GC tests, full mise format-check, lint, typecheck, test, and hooks.
<!-- SECTION:FINAL_SUMMARY:END -->
