---
id: TASK-26
title: Restore colored grouped CLI help
status: done
type: bug
priority: medium
assignees: ['@codex-loop-fresh-20260714-worklease-pass']
labels: [cli, ux]
rank: a2f9b658072f9b62e
created: 2026-07-15T04:48:00Z
updated: 2026-07-15T04:49:00Z
---

Restore argparse color styling for command names and group headings rendered in the manually grouped top-level help epilog. The grouped help must retain color when enabled without emitting ANSI when color is disabled.

## Acceptance criteria

- [x] Top-level command group headings and command labels retain argparse color styling when color is enabled.
- [x] The same help output remains ANSI-free when NO_COLOR disables color.
- [x] Subprocess tests cover both color-enabled and color-disabled grouped help.
- [x] All existing CLI behavior and repository quality gates remain passing.

## Plan

1. Reproduce the missing color under a forced-color TTY and trace the manual epilog rendering path. 2. Apply color-aware formatter markers to grouped headings and command labels without affecting nested help. 3. Add subprocess regression coverage for enabled and disabled color, then run all quality gates. 4. Finalize the bug task and commit the fix.

## Notes

Root cause established: grouped command entries are plain epilog text, bypassing argparse color-aware action formatting. Implemented explicit formatter markers resolved through argparse theme colors; no-color mode removes markers.

Validation passed: forced-color subprocess assertions for heading and command-label ANSI sequences; NO_COLOR subprocess assertion; targeted help tests; mise run lint, format-check, typecheck, test, and hooks all pass.

## Summary

Restored argparse theme colors for grouped top-level help by resolving explicit heading and command-label markers in a color-aware RawDescriptionHelpFormatter. Marker resolution emits the normal argparse ANSI theme when enabled and empty strings when color is disabled. Verified with color-enabled and NO_COLOR subprocess tests, three targeted help tests, all 166 core and 19 SDK tests, lint, format-check, typecheck, and hooks.
