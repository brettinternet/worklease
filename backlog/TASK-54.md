---
id: TASK-54
title: Slim README around examples
status: done
type: docs
priority: medium
rank: a61e8a546f61e89fe
files: [README.md, tests/test_cli.py]
created: 2026-09-07T08:06:00Z
updated: 2026-09-07T14:13:00Z
---

Make the project README easier to scan by replacing exhaustive prose and reference tables with a concise, example-led introduction, lifecycle, installation path, and links to deeper documentation or CLI help.

## Acceptance criteria

- [x] README explains the Worklease problem and same-host boundary with a clear visual
- [x] A reader can install Worklease and follow a complete singleton claim lifecycle from copyable examples
- [x] Detailed command inventory and text grammar no longer dominate the README
- [x] README remains accurate against the current CLI and links to deeper workflow and compatibility documentation

## Plan

Audit the current README and CLI surface, rewrite the README around a visual overview and one runnable lifecycle example, retain concise installation, safety, advanced-use, and development guidance, then run documentation and repository checks.

## Notes

Replaced the 469-line reference-style README with a 197-line visual, example-led guide. Corrected the README/CLI test so short-option stability remains tested without requiring the removed exhaustive option table. README lifecycle smoke test and local-link validation passed.

Verification passed: singleton lifecycle smoke test against the documented key/acquire/status/exec/checkpoint/heartbeat/release commands; 10 local Markdown links resolved; README reduced from 469 lines and 3,137 words to 197 lines and 764 words; mise run lint, format-check, test (223 core and 19 SDK), typecheck, and build all passed.

Post-delivery review (TASK-56) found and fixed defects in this work; see TASK-56 for the specific defect, the fix, and its regression test.

## Summary

Reworked README into a concise visual guide centered on a copyable singleton lifecycle, with compact bundle, agent, automation, safety, and development sections. Removed exhaustive CLI tables in favor of command help and adjusted the test that required the old short-option table. Verified the documented lifecycle, links, full test suite, type checks, formatting, lint, and package build.
