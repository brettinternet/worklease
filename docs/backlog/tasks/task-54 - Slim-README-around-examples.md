---
id: TASK-54
title: Slim README around examples
status: Done
assignee: []
created_date: '2026-09-07 08:06'
updated_date: '2026-09-07 14:13'
labels: []
dependencies: []
modified_files:
  - README.md
  - tests/test_cli.py
priority: medium
type: docs
ordinal: 55000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the project README easier to scan by replacing exhaustive prose and reference tables with a concise, example-led introduction, lifecycle, installation path, and links to deeper documentation or CLI help.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 README explains the Worklease problem and same-host boundary with a clear visual
- [x] #2 A reader can install Worklease and follow a complete singleton claim lifecycle from copyable examples
- [x] #3 Detailed command inventory and text grammar no longer dominate the README
- [x] #4 README remains accurate against the current CLI and links to deeper workflow and compatibility documentation
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
Audit the current README and CLI surface, rewrite the README around a visual overview and one runnable lifecycle example, retain concise installation, safety, advanced-use, and development guidance, then run documentation and repository checks.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Replaced the 469-line reference-style README with a 197-line visual, example-led guide. Corrected the README/CLI test so short-option stability remains tested without requiring the removed exhaustive option table. README lifecycle smoke test and local-link validation passed.

Verification passed: singleton lifecycle smoke test against the documented key/acquire/status/exec/checkpoint/heartbeat/release commands; 10 local Markdown links resolved; README reduced from 469 lines and 3,137 words to 197 lines and 764 words; mise run lint, format-check, test (223 core and 19 SDK), typecheck, and build all passed.

Post-delivery review (TASK-56) found and fixed defects in this work; see TASK-56 for the specific defect, the fix, and its regression test.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Reworked README into a concise visual guide centered on a copyable singleton lifecycle, with compact bundle, agent, automation, safety, and development sections. Removed exhaustive CLI tables in favor of command help and adjusted the test that required the old short-option table. Verified the documented lifecycle, links, full test suite, type checks, formatting, lint, and package build.
<!-- SECTION:FINAL_SUMMARY:END -->
