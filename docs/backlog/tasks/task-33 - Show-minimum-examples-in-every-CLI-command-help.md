---
id: TASK-33
title: Show minimum examples in every CLI command help
status: Done
assignee:
  - '@codex-loop-fresh-20260715-worklease-pass'
created_date: '2026-07-15 23:02'
updated_date: '2026-07-15 23:09'
labels:
  - cli
  - ux
dependencies: []
priority: medium
type: enhancement
ordinal: 34000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make every canonical worklease command help menu include one concise, copyable minimum-arguments example. In particular, make the required --resource argument discoverable for status so users can run the smallest valid status invocation instead of seeing only invalid-arguments.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every canonical top-level and nested command help menu includes a concise minimum-arguments example, including status with --resource.
- [x] #2 Examples preserve existing command parsing and help exit behavior, and aliases inherit the canonical command examples.
- [x] #3 Subprocess tests verify the examples appear and that the minimum status invocation succeeds.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inventory canonical and nested argparse commands and define one minimum-arguments example for each.
2. Attach examples to each command help menu while preserving parser behavior and alias sharing.
3. Add subprocess coverage for all examples plus a successful minimum status invocation.
4. Run focused tests and repository quality gates, then finalize the task with evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Diagnosis: worklease status requires --resource; bare status reproduces ERROR status: invalid-arguments with exit 64, while worklease status --resource local:formatter succeeds with a free-state response.
Implementation: added copyable minimum examples to every canonical top-level and nested command parser, including status and bundle aliases; added subprocess coverage for all examples, aliases, and the minimum status invocation. Focused tests and formatter/lint pass.

Correction: heartbeat and heartbeat-bundle examples now use a shell-safe single-quoted operation ID; targeted help coverage asserts the complete quoted tail.
Verification: independent verifier PASS on all 3 criteria. Six targeted CLI tests passed; verifier exercised all 24 canonical help menus, all 6 aliases, aggregate --help-all, and status --resource local:formatter. Full mise run lint, format-check, test, typecheck, and hooks passed. Smoke-tested status and status --help.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added copyable minimum-arguments examples to every canonical top-level and nested CLI help menu, including status --resource local:formatter and alias inheritance. Preserved parser behavior. Added subprocess coverage for all canonical examples, aliases, and successful minimum status execution. Verified with independent PASS, six targeted tests, full lint/format/test/typecheck/hooks gates, and CLI smoke checks.
<!-- SECTION:FINAL_SUMMARY:END -->
